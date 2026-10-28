// The assembly reads top-down like the architecture we designed: [ assembling all the components together]
//
//	ORCHESTRATOR  (this agent — holds the plan, decides, delegates)
//	  ├── dispatch_analyst      → fresh Analyst     (nmap/nikto/rustscan + CVE tools)
//	  └── dispatch_exploit_dev  → fresh Exploit-Dev (writes a PoC; gated by the user)
package orchestrator

import (
	"onigami/internal/agent"
	"onigami/internal/llm"
	"onigami/internal/scope"
	"onigami/internal/tools"
	"onigami/internal/workers"
)

const orchestratorSystem = `You are the ORCHESTRATOR of onigami, an authorized, ethical red-team assistant.
You never use scanners or write code yourself. You PLAN and DELEGATE.

You command three specialists, each reached through a tool:
  - dispatch_analyst: give it a brief; it performs recon and vulnerability
    research (ports, services, versions, CVEs) and returns findings.
  - dispatch_web_tester: give it a web target and what recon found; it actively
    tests the web app (SQLi, command injection, XSS, access control, TLS,
    misconfig) and returns confirmed findings with evidence.
  - dispatch_exploit_dev: give it a confirmed finding; it returns a
    proof-of-concept with an explanation.
  - dispatch_exploiter: (only when Metasploit is available) give it ONE confirmed
    finding and the in-scope target; it uses Metasploit to verify/demonstrate it.
    Every module run is scope-checked, operator-approved, and audited.

Typical flow: Analyst maps the target, then — for any web surface — the
Web-Tester confirms exploitable issues, then either the Exploit-Dev writes a PoC
or (if available and appropriate) the Exploiter demonstrates it with Metasploit.
Only hand the Exploiter a specific, confirmed finding — never a guess.

Workers are stateless and have no memory of each other or of earlier calls, so
every brief you write must be complete and self-contained. Work in steps: first
have the Analyst investigate, read its report, then — only for confirmed
findings — brief the Exploit-Dev. When the objective is met, stop and write a
final summary of what was found and demonstrated.

Ground your final summary ONLY in what your specialists actually reported. Do
not assert that a step succeeded, a control check passed, or a service was
reachable unless a worker's evidence supports it. If the target could not be
reached or a finding is unconfirmed, say so plainly rather than implying success.`

// Config is everything New needs to assemble the crew. Each agent gets its own
// llm.Client, so you can give them separate API keys to spread rate limits
// across accounts (see main.go). If you only have one key, pass the same client
// for all three — it still works, just under one shared limit.
type Config struct {
	Orchestrator llm.Client // brain
	Analyst      llm.Client // recon worker
	WebTester    llm.Client // active web-testing worker
	ExploitDev   llm.Client // PoC worker
	Exploiter    llm.Client // Metasploit exploitation worker

	// Per-agent model ids. Groq rate limits are per-model-per-org, so running
	// agents on different models spreads them across independent token buckets
	// (see main.go, which resolves each from its own env var). Any left empty
	// falls back to OrchestratorModel.
	OrchestratorModel string
	AnalystModel      string
	WebTesterModel    string
	ExploitDevModel   string
	ExploiterModel    string

	Scope    *scope.Scope
	Observer agent.Observer
	Approver agent.Approver    // human gate on the Exploit-Dev's output and each MSF run
	MSF      *tools.MSFManager // Metasploit daemon manager (nil/unavailable = no exploiter)
	Audit    *tools.AuditLog   // audit log for the exploiter
}

// New builds the orchestrator agent, fully wired to its workers and their tools.
func New(cfg Config) *agent.Agent {
	// Each worker type gets its own tool belt. Workers run one level deeper than
	// the orchestrator (depth 1), so the UI can indent their activity.
	const workerDepth = 1

	// Resolve per-agent models; anything unset falls back to the orchestrator's.
	orchModel := cfg.OrchestratorModel
	pick := func(m string) string {
		if m == "" {
			return orchModel
		}
		return m
	}
	analystModel := pick(cfg.AnalystModel)
	webTesterModel := pick(cfg.WebTesterModel)
	exploitDevModel := pick(cfg.ExploitDevModel)
	exploiterModel := pick(cfg.ExploiterModel)

	// Wrap each worker builder as a dispatch tool. The closures build a FRESH
	// worker — AND a fresh tool set — on every call. Building the tools per
	// spawn matters: it gives each Analyst its own HTTP cookie jar, so sessions
	// never leak between workers. That's what keeps workers truly stateless.
	// Only the Exploit-Dev dispatch carries the human approval gate.
	dispatchAnalyst := workers.NewDispatchTool(
		"dispatch_analyst",
		"Spawn a fresh Analyst to perform recon and vulnerability research. Provide a complete brief; it has no prior context.",
		func() *agent.Agent {
			// The Analyst gets the real recon + CVE + http tools (only scanners
			// that are installed), all bound to the scope.
			tb := agent.NewToolSet(tools.AnalystTools(cfg.Scope)...)
			return workers.NewAnalyst(cfg.Analyst, analystModel, tb, workerDepth)
		},
		cfg.Observer,
		nil, // no gate on research
	)

	// The Web-Tester gets the active OWASP-testing belt (fresh per spawn, so each
	// one has its own HTTP cookie jar). Active testing of an authorized, in-scope
	// target is not gated — the scope check bounds it and the tools run in
	// detection mode; the human gate sits on the Exploit-Dev instead.
	dispatchWebTester := workers.NewDispatchTool(
		"dispatch_web_tester",
		"Spawn a fresh Web-Tester to actively test an in-scope web app for OWASP Top-10 issues (SQLi, command injection, XSS, access control, TLS, misconfig). Provide a complete brief including what recon already found; it has no prior context.",
		func() *agent.Agent {
			tb := agent.NewToolSet(tools.WebTesterTools(cfg.Scope)...)
			return workers.NewWebTester(cfg.WebTester, webTesterModel, tb, workerDepth)
		},
		cfg.Observer,
		nil, // active in-scope testing is not gated; see note above
	)

	dispatchExploitDev := workers.NewDispatchTool(
		"dispatch_exploit_dev",
		"Spawn a fresh Exploit-Dev to produce a proof-of-concept for ONE confirmed finding. Provide a complete brief; it has no prior context.",
		func() *agent.Agent {
			// No tools: it reasons and writes a PoC; the user gates whether to proceed.
			return workers.NewExploitDev(cfg.ExploitDev, exploitDevModel, agent.NewToolSet(), workerDepth)
		},
		cfg.Observer,
		cfg.Approver, // user reviews the PoC and chooses proceed/hold
	)

	// Assemble the crew. The Exploiter is only offered when Metasploit is
	// actually available on this machine; otherwise the orchestrator falls back
	// to the Exploit-Dev (which writes a PoC without live exploitation).
	crew := []agent.Tool{dispatchAnalyst, dispatchWebTester, dispatchExploitDev}
	if cfg.MSF != nil && cfg.MSF.Available() {
		dispatchExploiter := workers.NewDispatchTool(
			"dispatch_exploiter",
			"Spawn a fresh Exploiter to confirm/demonstrate ONE vulnerability with Metasploit against an in-scope target. Provide a complete brief with the finding and target; it has no prior context. Each module run is scope-checked, operator-gated, and audited.",
			func() *agent.Agent {
				tb := agent.NewToolSet(tools.ExploiterTools(cfg.MSF, cfg.Scope, cfg.Approver, cfg.Audit)...)
				return workers.NewExploiter(cfg.Exploiter, exploiterModel, tb, workerDepth)
			},
			cfg.Observer,
			nil, // gating happens per-module inside msf_run, not at dispatch
		)
		crew = append(crew, dispatchExploiter)
	}

	return &agent.Agent{
		Role:            agent.RoleOrchestrator,
		System:          orchestratorSystem,
		Tools:           agent.NewToolSet(crew...),
		LLM:             cfg.Orchestrator,
		Model:           orchModel,
		MaxTurns:        24,   // room to dispatch workers, re-dispatch on failure, and still report
		MaxOutputTokens: 1536, // it writes briefs and a final summary, nothing large
		Depth:           0,
	}
}
