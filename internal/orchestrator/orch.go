// Package orchestrator assembles the whole crew: the brain and the two workers
// it can delegate to. This is the one place that knows how the pieces fit
// together, so cmd/main stays tiny.
//
// The assembly reads top-down like the architecture we designed:
//
//	ORCHESTRATOR  (this agent — holds the plan, decides, delegates)
//	  ├── dispatch_analyst      → spawns a fresh Analyst     (recon + CVE tools)
//	  └── dispatch_exploit_dev  → spawns a fresh Exploit-Dev (no stub tools yet)
//
// The orchestrator's "tools" are the two dispatchers. When it calls one, a
// brand-new worker runs to completion and its report comes back as the tool
// result. One mechanism (the agent loop) at both levels.
package orchestrator

import (
	"onigami/internal/agent"
	"onigami/internal/llm"
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
// tools. client is the model everyone thinks with; obs receives live events
// from every level so the UI (or stdout) can watch the whole run.
func New(client llm.Client, model string, obs agent.Observer) *agent.Agent {
	// Each worker type gets its own tool belt. Workers run one level deeper than
	// the orchestrator (depth 1), so the UI can indent their activity.
	const workerDepth = 1

	// Real tools get wired in here next. Empty for now — the stub/fake tools
	// were removed, so the Analyst currently reasons from its own knowledge
	// until nmap/nikto/CVE-lookup tools are added.
	analystTools := agent.NewToolSet()
	exploitTools := agent.NewToolSet()

	// Wrap each worker builder as a dispatch tool. The closures capture the
	// client/tools and build a FRESH worker on every call — nothing is reused
	// between dispatches, which is what keeps workers ephemeral and stateless.
	dispatchAnalyst := workers.NewDispatchTool(
		"dispatch_analyst",
		"Spawn a fresh Analyst to perform recon and vulnerability research. Provide a complete brief; it has no prior context.",
		func() *agent.Agent { return workers.NewAnalyst(client, model, analystTools, workerDepth) },
		obs,
	)

	dispatchExploitDev := workers.NewDispatchTool(
		"dispatch_exploit_dev",
		"Spawn a fresh Exploit-Dev to produce a proof-of-concept for ONE confirmed finding. Provide a complete brief; it has no prior context.",
		func() *agent.Agent { return workers.NewExploitDev(client, model, exploitTools, workerDepth) },
		obs,
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
