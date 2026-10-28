
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

You command two specialists, each reached through a tool:
  - dispatch_analyst: give it a brief; it performs recon and vulnerability
    research and returns findings.
  - dispatch_exploit_dev: give it a confirmed finding; it returns a
    proof-of-concept with an explanation.

Workers are stateless and have no memory of each other or of earlier calls, so
every brief you write must be complete and self-contained. Work in steps: first
have the Analyst investigate, read its report, then — only for confirmed
findings — brief the Exploit-Dev. When the objective is met, stop and write a
final summary of what was found and demonstrated.`

// New builds the orchestrator agent, fully wired to its workers and their
// tools.
//
//   - client: the model everyone thinks with
//   - sc:     the authorized scope; scan tools enforce it on every call
//   - obs:    receives live events from every level (for stdout / the TUI)
//   - approve: the human gate applied to the Exploit-Dev's output (proceed/hold)
func New(client llm.Client, model string, sc *scope.Scope, obs agent.Observer, approve agent.Approver) *agent.Agent {
	// Each worker type gets its own tool belt. Workers run one level deeper than
	// the orchestrator (depth 1), so the UI can indent their activity.
	const workerDepth = 1

	// The Analyst gets the real recon + CVE tools (only the scanners that are
	// actually installed), all bound to the scope. The Exploit-Dev has no tools:
	// it reasons and writes a PoC, and the user gates whether to proceed with it.
	analystTools := agent.NewToolSet(tools.AnalystTools(sc)...)
	exploitTools := agent.NewToolSet()

	// Wrap each worker builder as a dispatch tool. The closures capture the
	// client/tools and build a FRESH worker on every call — nothing is reused
	// between dispatches, which is what keeps workers ephemeral and stateless.
	// Only the Exploit-Dev dispatch carries the human approval gate.
	dispatchAnalyst := workers.NewDispatchTool(
		"dispatch_analyst",
		"Spawn a fresh Analyst to perform recon and vulnerability research. Provide a complete brief; it has no prior context.",
		func() *agent.Agent { return workers.NewAnalyst(client, model, analystTools, workerDepth) },
		obs,
		nil, // no gate on research
	)

	dispatchExploitDev := workers.NewDispatchTool(
		"dispatch_exploit_dev",
		"Spawn a fresh Exploit-Dev to produce a proof-of-concept for ONE confirmed finding. Provide a complete brief; it has no prior context.",
		func() *agent.Agent { return workers.NewExploitDev(client, model, exploitTools, workerDepth) },
		obs,
		approve, // user reviews the PoC and chooses proceed/hold
	)

	return &agent.Agent{
		Role:     agent.RoleOrchestrator,
		System:   orchestratorSystem,
		Tools:    agent.NewToolSet(dispatchAnalyst, dispatchExploitDev),
		LLM:      client,
		Model:    model,
		MaxTurns: 20,
		Depth:    0,
	}
}
