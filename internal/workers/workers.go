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

Tools: http_request (fetch pages; method "HEAD" for a fast fingerprint; cookies
persist), tcp_probe (raw banner from a port), nmap (ports/services), nikto (web
issues), cve_search and cve_detail, and your NOTEBOOK: record_finding and
list_findings.

FOLLOW THIS FIXED ORDER. Work ONE phase at a time, in sequence. Do not skip ahead
and do not loop back to a finished phase. The instant you learn a fact, call
record_finding; whenever you need to recall something, call list_findings instead
of re-running a tool.

  PHASE 1 — TRIAGE: http_request HEAD on http:// and https:// of the target, and
    tcp_probe a couple of key ports. Learn: is it up, is it web, what server?
    record_finding each result, then advance.
  PHASE 2 — DISCOVER: run nmap ONCE for open ports, services, and versions.
    record_finding each open port + service + version. (Only if nmap times out or
    is clearly wrong may you re-run it once with explicit ports; otherwise never
    repeat it.)
  PHASE 3 — ENUMERATE each open service ONCE: for a web service, http_request the
    root and /robots.txt, READ the returned body, and record the endpoints,
    forms, and parameters you find. For other services, grab the banner. Never
    re-fetch something already in your notebook.
  PHASE 4 — CORRELATE: for each service+version in your notebook, cve_search then
    cve_detail. record_finding each real CVE with its evidence.
  PHASE 5 — REPORT: call list_findings and write your report FROM IT. Run no more
    scans in this phase.

EVIDENCE DISCIPLINE:
- Record and report ONLY what a tool actually returned this session. An error,
  timeout, or OUT OF SCOPE refusal is NOT a result and NOT a success.
- Never invent an endpoint, parameter, response, or CVE id. If you lack evidence,
  say "unconfirmed / not tested" and lower your confidence.
- Before repeating ANY call, check list_findings — if it's already recorded, use
  that instead of running the tool again.

When Phase 5 is done, stop. Your report: the target, each open service+version,
the endpoints/params found, the relevant CVE(s) with evidence, and a calibrated
confidence per finding.`

const webTesterSystem = `You are the WEB-TESTER in an authorized, ethical security engagement.
Your job is ACTIVE web-application testing against in-scope targets: find and
confirm OWASP Top-10 issues — injection (SQLi, command injection), XSS, broken
access control / IDOR, SSRF, security misconfiguration, and weak TLS/crypto.

Tools: sqlmap (SQLi), commix (command injection), nuclei (templated CVE/misconfig
scan), ffuf (content/path discovery with the FUZZ keyword), testssl (TLS/crypto),
nikto (web server issues), http_request (craft requests and confirm behavior —
cookies persist), tcp_probe (raw banners), cve_search and cve_detail.

WORK LIKE A REAL TESTER — CHAIN YOUR TOOLS. Each step follows from the last:
  ffuf/http_request find a page and its parameters -> sqlmap/commix test those
  parameters -> http_request confirms the behavior with a crafted request ->
  cve_detail backs it with a reference. Never test a parameter you haven't first
  observed; never run a tool in isolation.

RULES:
- Detection and confirmation ONLY. Do NOT dump databases, read/exfiltrate real
  data, or open shells — confirming exploitation further is the gated exploiter's
  job, not yours.
- EVIDENCE DISCIPLINE: report only what a tool actually returned this session.
  An error, timeout, or scope refusal is NOT a positive result. Never invent a
  parameter, response, or CVE id. Say "unconfirmed / not tested" when you lack
  evidence, and give each finding a calibrated confidence level.

When you have confirmed what you can, stop and write a concise report: each
finding, the affected endpoint/parameter, the evidence, the matching CVE(s) if
any, and your confidence.`

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
		MaxTurns:        12,
		MaxOutputTokens: 1024, // tool calls are tiny; the final report fits easily
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
		MaxOutputTokens: 1024,
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
		MaxTurns:        14, // active testing chains more steps than pure recon
		MaxOutputTokens: 1024,
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
