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
	name  string
	desc  string
	build func() *agent.Agent // makes a NEW worker each call
	obs   agent.Observer      // passed down so nested work is observable
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
Use your tools to gather evidence. When you have enough, stop and write a concise
report: what you found, the affected component and version, the relevant CVE(s),
and your confidence. Be precise and cite the evidence your tools returned.`

const exploitDevSystem = `You are the EXPLOIT-DEV in an authorized, ethical security engagement.
You receive a specific, already-confirmed finding and produce a clear, well-
structured proof-of-concept that demonstrates the issue for the engagement
report. Explain what the PoC does, the preconditions, and how a defender would
detect or remediate it. Keep it scoped to the single finding you were briefed on.
When done, stop and return the PoC with its explanation.`

// NewAnalyst builds a fresh Analyst wired to the given model and tool belt.
func NewAnalyst(client llm.Client, model string, tools *agent.ToolSet, depth int) *agent.Agent {
	return &agent.Agent{
		Role:     agent.RoleAnalyst,
		System:   analystSystem,
		Tools:    tools,
		LLM:      client,
		Model:    model,
		MaxTurns: 12,
		Depth:    depth,
	}
}

// NewExploitDev builds a fresh Exploit-Dev. It is given its own (smaller) tool
// belt — typically just what it needs to write and sanity-check code.
func NewExploitDev(client llm.Client, model string, tools *agent.ToolSet, depth int) *agent.Agent {
	return &agent.Agent{
		Role:     agent.RoleExploitDev,
		System:   exploitDevSystem,
		Tools:    tools,
		LLM:      client,
		Model:    model,
		MaxTurns: 8,
		Depth:    depth,
	}
}

// NewDispatchTool wires a worker builder up as a tool the orchestrator can call.
func NewDispatchTool(name, desc string, build func() *agent.Agent, obs agent.Observer) *DispatchTool {
	return &DispatchTool{name: name, desc: desc, build: build, obs: obs}
}
