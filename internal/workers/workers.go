// Package workers defines the specialist agents (the Analyst and the
// Exploit-Dev) and the trick that lets the orchestrator spawn them: wrapping a
// worker as a Tool.
//
// Recall the design: workers are EPHEMERAL and STATELESS. Each time the
// orchestrator delegates, it gets a brand-new worker with an empty mind, hands
// it a tailored brief, reads the single report it returns, and lets it die.
// DispatchTool encodes exactly that — its Run builds a fresh Agent every call.
package workers

import (
	"context"
	"encoding/json"
	"fmt"

	"onigami/internal/agent"
	"onigami/internal/llm"
)

// briefInput is the argument schema for a dispatch tool: the orchestrator
// writes a brief describing precisely what it wants this worker to do.
type briefInput struct {
	Brief string `json:"brief"`
}

// briefSchema is the JSON Schema we advertise for that single argument.
var briefSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "brief": {
      "type": "string",
      "description": "A complete, self-contained instruction for the worker. It has no memory and no context beyond this text, so include every relevant detail (target, versions, findings so far, and exactly what you want back)."
    }
  },
  "required": ["brief"]
}`)

// DispatchTool exposes "spawn a worker and get its report" as a Tool the
// orchestrator can call. The build function is the important part: it is called
// fresh on every dispatch, so every worker starts clean — that's the
// "ephemeral, stateless" rule made literal.
type DispatchTool struct {
	name    string
	desc    string
	build   func() *agent.Agent // makes a NEW worker each call
	obs     agent.Observer      // passed down so nested work is observable
	approve agent.Approver      // optional human gate on the worker's output (nil = none)
}

// Name, Description, and Schema satisfy the agent.Tool interface.
func (d *DispatchTool) Name() string            { return d.name }
func (d *DispatchTool) Description() string     { return d.desc }
func (d *DispatchTool) Schema() json.RawMessage { return briefSchema }

// Run spawns a fresh worker, runs it on the brief, and returns its report
// summary as the tool's output. From the orchestrator's side this looks like
// any other tool call; under the hood a whole sub-agent loop just ran.
func (d *DispatchTool) Run(ctx context.Context, input json.RawMessage) (string, error) {
	var in briefInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("bad dispatch input: %w", err)
	}
	if in.Brief == "" {
		return "", fmt.Errorf("brief must not be empty")
	}

	worker := d.build() // <-- fresh, empty-minded worker, every time
	report, err := worker.Run(ctx, agent.Task{
		Objective: string(worker.Role) + " task",
		Brief:     in.Brief,
	}, d.obs)
	if err != nil {
		return "", fmt.Errorf("%s worker failed: %w", worker.Role, err)
	}

	// Human-in-the-loop gate: if an approver is set, the user reviews this
	// worker's output (which was just streamed to them) and chooses to proceed
	// or hold. On hold, we tell the orchestrator to stop rather than build on it.
	if !d.approve.Ask(ctx, worker.Role, report.Summary) {
		return "The user reviewed this output and chose to HOLD. Do not proceed with or act on it. Stop here and report that the engagement was paused at the user's request.", nil
	}
	return report.Summary, nil
}

// --- Worker builders ------------------------------------------------------
//
// Each builder returns a configured (but not yet run) Agent. A "worker type" in
// onigami is nothing more than a role + a system prompt + a tool belt — these
// functions just assemble those three things.

const analystSystem = `You are the ANALYST in an authorized, ethical security engagement.
Your job is reconnaissance and vulnerability research: enumerate the target,
correlate findings with known issues (CVEs), and report what is exploitable and
why. You do NOT write exploit code — you hand findings to the orchestrator.

Tools: recon_sweep (automated one-shot discovery+enumeration — your main tool),
cve_search and cve_detail (research), http_request / tcp_probe / nmap / nikto (for
targeted follow-up only), and the shared NOTEBOOK: record_finding and list_findings.

FOLLOW THIS FIXED ORDER — it is short on purpose:

  PHASE 1 — SWEEP: call recon_sweep ONCE on the target. It runs the full
    mechanical recon (nmap port/service/version discovery, HTTP fingerprinting,
    endpoint/parameter enumeration) in one step and records everything to the
    shared notebook. Do NOT hand-run nmap/http_request to redo this — read the
    sweep's result instead. Only use the individual tools afterward to dig into a
    SPECIFIC gap the sweep left.
  PHASE 2 — CORRELATE: for each distinct service+version the sweep found, call
    cve_search then cve_detail, and record_finding each real CVE with its evidence.
  PHASE 3 — REPORT: call list_findings once and write your report FROM IT.

EVIDENCE DISCIPLINE (the system enforces this in code — work with it):
- Record and report ONLY what a tool actually returned this session. An error,
  timeout, or OUT OF SCOPE refusal is NOT a result and NOT a success.
- Never invent an endpoint, parameter, response, or CVE id. If you lack evidence,
  say "unconfirmed / not tested" and lower your confidence.
- record_finding now REQUIRES an evidence string (paste the supporting tool
  output). What you record is marked UNVERIFIED (an agent claim) and is NOT
  acted on automatically — only tool-produced facts are. recon_sweep and
  cve_detail already auto-record their results as verified, so don't re-record
  those; your record_finding is only for a correlation you want noted.
- Never re-run a tool just to recall its output — it's in the notebook.

When Phase 3 is done, stop. Your report: the target, each open service+version,
the endpoints/params found, the relevant CVE(s) with evidence, and a calibrated
confidence per finding.`

const webTesterSystem = `You are the WEB-TESTER in an authorized, ethical security engagement.
Your job is ACTIVE web-application testing against in-scope targets: find and
confirm OWASP Top-10 issues — injection (SQLi, command injection), XSS, broken
access control / IDOR, SSRF, security misconfiguration, and weak TLS/crypto.

Tools: sqlmap (SQLi), commix (command injection), nuclei (templated CVE/misconfig
scan), ffuf (content/path discovery with the FUZZ keyword), testssl (TLS/crypto),
nikto (web server issues), http_request (craft requests and confirm behavior —
cookies persist; HEAD for a fast fingerprint), tcp_probe (raw banners), cve_search
and cve_detail, and the shared NOTEBOOK: record_finding and list_findings.

FOLLOW THIS FIXED ORDER. Work ONE phase at a time, in sequence; do not loop back
to a finished phase, and do not run a tool in isolation.

  PHASE 0 — ORIENT: call list_findings FIRST. The analyst has already recorded
    the target's services, versions, and endpoints in the shared notebook — build
    on that; do NOT re-discover what's already recorded.
  PHASE 1 — MAP: fill gaps only. http_request the key pages and READ the bodies to
    enumerate forms and parameters; use ffuf (URL with FUZZ) for hidden paths.
    record_finding each endpoint + its parameters. Do not re-fetch a page already
    covered by the notebook.
  PHASE 2 — TEST each injectable surface ONCE, choosing the right tool per class:
    - SQL injection: sqlmap on the parameterized URL (or --data for POST).
    - Command injection: commix on the parameterized URL.
    - Reflected XSS: http_request a probe payload, then check the response body
      for the reflection yourself.
    - TLS/crypto: testssl on the HTTPS host. Misconfig/known-CVE: nuclei on the base URL.
    record_finding each confirmed issue with its evidence. Test a parameter only
    after you've observed it; never test the same surface twice.
  PHASE 3 — REPORT: call list_findings and write your report FROM IT.

RULES:
- Detection and confirmation ONLY. Do NOT dump databases, read/exfiltrate real
  data, or open shells — confirming exploitation further is the gated exploiter's
  job, not yours.
- EVIDENCE DISCIPLINE (enforced in code): report only what a tool actually
  returned this session. An error, timeout, or scope refusal is NOT a positive
  result. Never invent a parameter, response, or CVE id; say "unconfirmed / not
  tested" when you lack evidence. record_finding REQUIRES an evidence string and
  what you record is marked UNVERIFIED (not acted on automatically) — only
  tool-produced facts are. Give each finding a calibrated confidence level.
- Record findings as you go and never re-run a tool just to recall its output —
  it's in the notebook. Reserve list_findings for Phase 0 and Phase 3.

When Phase 3 is done, stop. Your report: each finding, the affected
endpoint/parameter, the evidence, the matching CVE(s) if any, and your confidence.`

const exploitDevSystem = `You are the EXPLOIT-DEV in an authorized, ethical security engagement.
You receive a specific, already-confirmed finding and produce a clear, well-
structured proof-of-concept that demonstrates the issue for the engagement
report. Explain what the PoC does, the preconditions, and how a defender would
detect or remediate it. Keep it scoped to the single finding you were briefed on.
When done, stop and return the PoC with its explanation.`

// NewAnalyst builds a fresh Analyst wired to the given model and tool belt.
func NewAnalyst(client llm.Client, model string, tools *agent.ToolSet, depth int) *agent.Agent {
	return &agent.Agent{
		Role:            agent.RoleAnalyst,
		System:          analystSystem,
		Tools:           tools,
		LLM:             client,
		Model:           model,
		MaxTurns:        24,   // 5 phases + notebook bookkeeping need headroom
		MaxOutputTokens: 1536, // tool calls are tiny; the final report fits easily
		Depth:           depth,
	}
}

const exploiterSystem = `You are the EXPLOITER in an authorized, ethical security engagement.
You operate Metasploit against IN-SCOPE targets to confirm and demonstrate a
specific, already-identified vulnerability. This is real exploitation, so act
with precision and restraint.

Tools: msf_search (find modules), msf_module_info (a module's options), msf_run
(run a module — mode "check" verifies exploitability, mode "exploit" runs it for
real), msf_sessions (see if a session opened), tcp_probe (confirm an effect).

HOW TO WORK:
- Start from the confirmed finding in your brief. Search for the matching module,
  read its options, set them (RHOSTS must be the in-scope target).
- Prefer mode "check" first to verify the target is vulnerable; only use mode
  "exploit" when a demonstration is actually required.
- Set the minimum options needed. Do not touch hosts outside your brief's scope.

SAFETY YOU CANNOT BYPASS (enforced in code, not by you): every msf_run is scope-
checked on RHOSTS, requires the operator's approval, and is written to an audit
log. If the operator holds a run, stop and report that exploitation was paused.

EVIDENCE DISCIPLINE: report only what msf_run/msf_sessions actually returned. A
"check" that is not vulnerable, an error, or a held run is NOT a success. Do not
claim a shell/session unless msf_sessions shows it. When done, report what you
confirmed, the module and options used, and the real evidence.`

// NewExploiter builds a fresh Exploiter wired to the Metasploit tool belt.
func NewExploiter(client llm.Client, model string, tools *agent.ToolSet, depth int) *agent.Agent {
	return &agent.Agent{
		Role:            agent.RoleExploiter,
		System:          exploiterSystem,
		Tools:           tools,
		LLM:             client,
		Model:           model,
		MaxTurns:        12,
		MaxOutputTokens: 1536,
		Depth:           depth,
	}
}

// NewWebTester builds a fresh Web-Tester wired to the given model and tool belt
// (the active OWASP-testing tools plus the manual primitives).
func NewWebTester(client llm.Client, model string, tools *agent.ToolSet, depth int) *agent.Agent {
	return &agent.Agent{
		Role:            agent.RoleWebTester,
		System:          webTesterSystem,
		Tools:           tools,
		LLM:             client,
		Model:           model,
		MaxTurns:        20, // active testing chains more steps than pure recon
		MaxOutputTokens: 1536,
		Depth:           depth,
	}
}

// NewExploitDev builds a fresh Exploit-Dev. It is given its own (smaller) tool
// belt — typically just what it needs to write and sanity-check code.
func NewExploitDev(client llm.Client, model string, tools *agent.ToolSet, depth int) *agent.Agent {
	return &agent.Agent{
		Role:            agent.RoleExploitDev,
		System:          exploitDevSystem,
		Tools:           tools,
		LLM:             client,
		Model:           model,
		MaxTurns:        8,
		MaxOutputTokens: 1536, // a little more room: it writes a PoC with explanation
		Depth:           depth,
	}
}

// NewDispatchTool wires a worker builder up as a tool the orchestrator can
// call. approve may be nil (no human gate); pass one to require the user to
// review the worker's output before the orchestrator can build on it.
func NewDispatchTool(name, desc string, build func() *agent.Agent, obs agent.Observer, approve agent.Approver) *DispatchTool {
	return &DispatchTool{name: name, desc: desc, build: build, obs: obs, approve: approve}
}
