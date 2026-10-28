// Package agent defines what an "agent" is in onigami and, most importantly,
// the loop that drives one. Read this file first — everything else exists to
// serve this loop.
//
// An agent is simple: a role, a system prompt that defines its job, a set of
// tools it may use, and a model to think with. You give it a Task; it thinks,
// calls tools, looks at the results, thinks again, and eventually returns a
// written Report. That think→act→observe cycle is THE pattern of agentic AI,
// and it's the same whether the agent is the orchestrator or a worker.
package agent

import (
	"context"
	"fmt"
	"onigami/internal/llm"
)

// Role names what an agent is for. Used in prompts, logs, and the UI.
type Role string

const (
	RoleOrchestrator Role = "orchestrator" // the brain: plans and delegates
	RoleAnalyst      Role = "analyst"      // finds & researches vulnerabilities
	RoleExploitDev   Role = "exploit-dev"  // writes proof-of-concept code
)

// Task is the unit of work handed to an agent.
//
// Objective is the standing goal (useful for logging/UI). Brief is the actual,
// freshly-written context the agent starts from — for a worker this is the
// tailored instruction the orchestrator composes ("host X runs Apache 2.4.49,
// research known issues"), not a generic prompt.
type Task struct {
	Objective string
	Brief     string
}

// Report is what an agent returns when it's done: its role and the final
// written summary. (We'll grow this into structured findings later; a plain
// summary is enough to get the architecture working and reviewable.)
type Report struct {
	Role    Role
	Summary string
}

// --- Observability -------------------------------------------------------
//
// An agent must not know anything about the terminal UI. But we DO want to
// watch it work — every thought, tool call, and result — so the TUI (coming
// later) can render live activity. We bridge that gap with an Observer: a
// callback the agent calls as things happen. The engine stays UI-agnostic;
// the UI just supplies a function. For now main.go supplies one that prints.

// EventKind classifies something that happened during a run.
type EventKind string

const (
	EventStart      EventKind = "start"       // an agent began a task
	EventToolCall   EventKind = "tool_call"   // agent is invoking a tool
	EventToolResult EventKind = "tool_result" // a tool returned
	EventMessage    EventKind = "message"     // the agent produced text
	EventReport     EventKind = "report"      // the agent finished with a report
	EventError      EventKind = "error"       // something went wrong
)

// Event is one thing worth surfacing. Depth lets the UI indent nested work
// (the orchestrator is depth 0, a worker it spawns is depth 1, and so on).
type Event struct {
	Agent Role
	Kind  EventKind
	Text  string
	Depth int
}

// Observer receives events as an agent works. A nil Observer is fine — the
// agent checks before calling, so you can pass nil to run silently.
type Observer func(Event)

func (o Observer) emit(e Event) {
	if o != nil {
		o(e)
	}
}

// --- The agent -----------------------------------------------------------

// Agent is a configured, runnable agent. The same struct is used for the
// orchestrator and for every worker — they differ only in their Role, System
// prompt, and Tools.
type Agent struct {
	Role     Role
	System   string     // system prompt: defines the job and the rules
	Tools    *ToolSet   // what this agent may call
	LLM      llm.Client // how it thinks
	Model    string     // which model id to use
	MaxTurns int        // safety cap so a confused agent can't loop forever
	Depth    int        // nesting level, for indented UI/logs
}

// Run drives the agent to completion on a task and returns its Report.
//
// This is the loop. In plain English:
//
//  1. Put the task brief into the conversation as the first user message.
//  2. Ask the model what to do, given its tools.
//  3. If the model just wrote an answer (no tool calls) → that's the report, done.
//  4. If it asked to use tools → run each one, append the results, go to step 2.
//  5. If we exceed MaxTurns, bail out with an error (something's stuck).
//
// Notice there is NOTHING here specific to "orchestrator" or "worker". Give it
// worker-dispatch tools and it behaves as the brain; give it scan tools and it
// behaves as a worker. That's the recursion that keeps the system small.
func (a *Agent) Run(ctx context.Context, task Task, obs Observer) (Report, error) {
	obs.emit(Event{Agent: a.Role, Kind: EventStart, Text: task.Objective, Depth: a.Depth})

	// The running conversation. It starts with the brief and grows one turn at
	// a time as the model and our tools take turns adding to it.
	msgs := []llm.Message{llm.UserText(task.Brief)}

	for turn := 0; turn < a.MaxTurns; turn++ {
		// Respect cancellation (e.g. the user quits the TUI mid-run).
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}

		resp, err := a.LLM.Complete(ctx, llm.Request{
			System:    a.System,
			Messages:  msgs,
			Tools:     a.Tools.Definitions(),
			Model:     a.Model,
			MaxTokens: 4096,
		})
		if err != nil {
			obs.emit(Event{Agent: a.Role, Kind: EventError, Text: err.Error(), Depth: a.Depth})
			return Report{}, fmt.Errorf("%s: model call failed: %w", a.Role, err)
		}

		// Record the model's turn in the conversation.
		msgs = append(msgs, resp.Message)

		if text := resp.Message.Text(); text != "" {
			obs.emit(Event{Agent: a.Role, Kind: EventMessage, Text: text, Depth: a.Depth})
		}

		// If the model didn't ask for any tools, it's finished thinking: its
		// text is the final report.
		if resp.StopReason != llm.StopToolUse {
			report := Report{Role: a.Role, Summary: resp.Message.Text()}
			obs.emit(Event{Agent: a.Role, Kind: EventReport, Text: report.Summary, Depth: a.Depth})
			return report, nil
		}

		// Otherwise, run every tool the model asked for and collect the results
		// into a single user turn.
		var results []llm.Content
		for _, call := range resp.Message.ToolCalls() {
			obs.emit(Event{Agent: a.Role, Kind: EventToolCall,
				Text: call.ToolName, Depth: a.Depth})

			result := a.Tools.Run(ctx, call)

			obs.emit(Event{Agent: a.Role, Kind: EventToolResult,
				Text:  fmt.Sprintf("%s → %s", call.ToolName, truncate(result.ToolResult, 120)),
				Depth: a.Depth})

			results = append(results, result)
		}
		msgs = append(msgs, llm.UserResults(results...))
	}

	err := fmt.Errorf("%s: gave up after %d turns without finishing", a.Role, a.MaxTurns)
	obs.emit(Event{Agent: a.Role, Kind: EventError, Text: err.Error(), Depth: a.Depth})
	return Report{}, err
}

// truncate shortens a string for display, adding an ellipsis if it was cut.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
