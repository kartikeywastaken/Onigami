// Package agent defines what an "agent" is in onigami and, most importantly,
// the loop that drives one. Read this file first  everything else exists to
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
	"strings"

	"onigami/internal/llm"
)

// Role names what an agent is for. Used in prompts, logs, and the UI.
type Role string

const (
	RoleOrchestrator Role = "orchestrator" // the brain: plans and delegates
	RoleAnalyst      Role = "analyst"      // finds & researches vulnerabilities
	RoleWebTester    Role = "web-tester"   // actively tests web apps (OWASP Top 10)
	RoleExploitDev   Role = "exploit-dev"  // writes proof-of-concept code
	RoleExploiter    Role = "exploiter"    // runs gated, real exploitation (Metasploit)
)

type Task struct {
	Objective string
	Brief     string
}

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
	EventThinking   EventKind = "thinking"    // the model's chain-of-thought (reasoning)
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

	// MaxOutputTokens caps how many tokens the model may generate per turn.
	// Keep it modest: providers like Groq count this reservation against the
	// per-minute token budget, so an oversized value silently eats the whole
	// budget before any conversation is even sent. 0 means "use the default".
	MaxOutputTokens int

	// MaxIdenticalCalls is how many times the agent may issue the SAME tool
	// call (same name + same arguments) before the loop guard refuses to run it
	// again. 0 means "use the default". This stops a confused model from, say,
	// re-running the identical nmap scan a dozen times.
	MaxIdenticalCalls int

	// RequireToolUse turns on the REFLECTOR guard (borrowed from PentAGI's
	// Reflector agent). When true, an agent that tries to finish with a report
	// WITHOUT ever having run a tool is bounced back with a pointed nudge:
	// findings must come from a tool, not from prose. This is the project's
	// defense against a weak model NARRATING fake tool output ("I ran sqlmap and
	// confirmed SQLi…") instead of actually calling the tool. Set it on the
	// recon/active-testing roles, whose job is meaningless without real tool
	// output; leave it off for roles that legitimately only write (e.g. the
	// exploit-dev, which reasons a PoC in prose).
	RequireToolUse bool
}

// Budget knobs for staying under a tight tokens-per-minute tier (Groq free tier
// is 8000 TPM, and the reservation below counts against it).
const (
	// defaultMaxOutputTokens is used when an Agent doesn't set its own cap.
	defaultMaxOutputTokens = 1024

	// defaultMaxIdenticalCalls is the loop-guard threshold: how many times the
	// same (tool, args) pair may run before we stop executing it and nudge the
	// model instead. A couple is fine — a transient failure warrants a retry —
	// but beyond that an identical call is a loop that won't make progress.
	defaultMaxIdenticalCalls = 2

	// keepFullResults is how many of the NEWEST tool results we send verbatim.
	// Older ones are elided to a stub so a long run's history can't grow past
	// the budget — the model is almost always reasoning over the recent few.
	keepFullResults = 4

	// elidedNote replaces the body of an older tool result once it ages out of
	// the "keep full" window. The tool_call pairing is preserved; only the
	// (large) output text is dropped.
	elidedNote = "[earlier tool result elided to conserve the token budget]"
)

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

	maxOut := a.MaxOutputTokens
	if maxOut <= 0 {
		maxOut = defaultMaxOutputTokens
	}

	maxIdentical := a.MaxIdenticalCalls
	if maxIdentical <= 0 {
		maxIdentical = defaultMaxIdenticalCalls
	}
	// callCounts tracks how many times each (tool, args) signature has been
	// issued this run, so the loop guard can refuse endless identical calls.
	callCounts := map[string]int{}

	// reportNudges counts how many times we've had to prod the model to actually
	// WRITE its report. Verbose reasoning models sometimes spend a whole turn in
	// chain-of-thought (or get cut off by the length limit) and emit no content
	// text at all — which would otherwise yield an empty report.
	reportNudges := 0

	// toolCallsMade counts how many tool calls this run has actually executed,
	// and reflectNudges bounds how many times the REFLECTOR guard may bounce a
	// no-tool report back (see RequireToolUse).
	toolCallsMade := 0
	reflectNudges := 0

	for turn := 0; turn < a.MaxTurns; turn++ {
		// Respect cancellation (e.g. the user quits the TUI mid-run).
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}

		resp, err := a.LLM.Complete(ctx, llm.Request{
			System: a.System,
			// Send a trimmed view: the brief and recent turns in full, older
			// tool outputs elided, so one request stays under the token budget.
			Messages:  trimToolResults(msgs, keepFullResults),
			Tools:     a.Tools.Definitions(),
			Model:     a.Model,
			MaxTokens: maxOut,
		})
		if err != nil {
			obs.emit(Event{Agent: a.Role, Kind: EventError, Text: err.Error(), Depth: a.Depth})
			return Report{}, fmt.Errorf("%s: model call failed: %w", a.Role, err)
		}

		// Record the model's turn in the conversation. (We intentionally do NOT
		// store reasoning back into msgs — it's for display only, and resending it
		// would waste tokens.)
		msgs = append(msgs, resp.Message)

		// Surface the model's reasoning so you can see WHY it's about to act.
		if resp.Reasoning != "" {
			obs.emit(Event{Agent: a.Role, Kind: EventThinking, Text: resp.Reasoning, Depth: a.Depth})
		}

		if text := resp.Message.Text(); text != "" {
			obs.emit(Event{Agent: a.Role, Kind: EventMessage, Text: text, Depth: a.Depth})
		}

		// If the model didn't ask for any tools, it's finished thinking: its
		// text is the final report.
		if resp.StopReason != llm.StopToolUse {
			text := resp.Message.Text()

			// REFLECTOR guard (PentAGI's Reflector, ported): a tool-grounded role
			// must not finish having run NO tool at all — that is the signature of a
			// model narrating fake tool output instead of calling the tool. Bounce it
			// back a bounded number of times, pointing it at the real tools. We only
			// do this when the agent HAS tools and is flagged RequireToolUse, so
			// prose-only roles (exploit-dev) are never penalised.
			if a.RequireToolUse && toolCallsMade == 0 && len(a.Tools.Definitions()) > 0 && reflectNudges < 2 {
				reflectNudges++
				obs.emit(Event{Agent: a.Role, Kind: EventError,
					Text:  "finished without running any tool — findings must come from a tool, not prose; nudging it to actually run one",
					Depth: a.Depth})
				msgs = append(msgs, llm.UserText("You ended without running a single tool. In this engagement a finding only counts if a TOOL produced it — you may not assert results you did not obtain from a tool. Call the appropriate tool now against the in-scope target. If you have genuinely established there is nothing to run, say so explicitly and state that no tool confirmed a finding."))
				continue
			}

			// Empty final answer. This happens when the model poured the whole
			// turn into its reasoning channel, or hit the length limit mid-thought
			// (StopMaxTokens) before writing any content. Prod it to actually
			// write the report — a bounded number of times — instead of returning
			// nothing.
			if strings.TrimSpace(text) == "" && reportNudges < 2 {
				reportNudges++
				obs.emit(Event{Agent: a.Role, Kind: EventError,
					Text:  "produced no written report (all reasoning or cut off by the length limit); nudging it to write one",
					Depth: a.Depth})
				msgs = append(msgs, llm.UserText("Your last turn produced no written report — it was either all internal reasoning or cut off by the length limit. Reply now with ONLY your final report: concise, no long reasoning."))
				continue
			}

			// Still empty after nudging: fall back to the reasoning text so the
			// caller gets *something* grounded rather than a blank summary.
			if strings.TrimSpace(text) == "" {
				if r := strings.TrimSpace(resp.Reasoning); r != "" {
					text = r
				} else {
					text = "(the worker produced no written report)"
				}
			}

			report := Report{Role: a.Role, Summary: text}
			obs.emit(Event{Agent: a.Role, Kind: EventReport, Text: report.Summary, Depth: a.Depth})
			return report, nil
		}

		// Otherwise, run every tool the model asked for and collect the results
		// into a single user turn.
		var results []llm.Content
		for _, call := range resp.Message.ToolCalls() {
			// Include the arguments so you can see exactly what it's doing
			// (e.g. nmap {"target":"scanme.nmap.org"}), not just the tool name.
			obs.emit(Event{Agent: a.Role, Kind: EventToolCall,
				Text:  fmt.Sprintf("%s %s", call.ToolName, truncate(string(call.ToolInput), 160)),
				Depth: a.Depth})

			// Loop guard: if this exact call has already run enough times, don't
			// run it again — the result won't change. Hand the model a pointed
			// note instead, so it changes approach or finishes.
			sig := call.ToolName + "\x00" + string(call.ToolInput)
			callCounts[sig]++
			if callCounts[sig] > maxIdentical {
				note := fmt.Sprintf("[loop guard] You have already issued this exact %s call %d times; the result will not change. Do not repeat it — change your approach or write your final report now.",
					call.ToolName, callCounts[sig]-1)
				obs.emit(Event{Agent: a.Role, Kind: EventToolResult,
					Text:  fmt.Sprintf("%s → %s", call.ToolName, note),
					Depth: a.Depth})
				results = append(results, llm.ToolResultBlock(call.ToolID, note, true))
				continue
			}

			result := a.Tools.Run(ctx, call)
			toolCallsMade++

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

// trimToolResults returns a copy of msgs in which all but the newest keepFull
// tool results have their (potentially large) output text replaced by a short
// stub. This is what keeps a long agent run inside a tight tokens-per-minute
// budget: scan and HTTP outputs from early turns are rarely needed once the
// model has moved on, but resending them every turn is what blows the limit.
//
// The structure is preserved exactly — same messages, same tool_use/tool_result
// pairing (the API requires it) — only the elided results' text changes.
func trimToolResults(msgs []llm.Message, keepFull int) []llm.Message {
	// Count tool-result blocks so we know how many of the oldest to elide.
	total := 0
	for _, m := range msgs {
		for _, c := range m.Content {
			if c.Type == llm.ContentToolResult {
				total++
			}
		}
	}
	if total <= keepFull {
		return msgs // nothing old enough to drop yet
	}

	toElide := total - keepFull
	seen := 0
	out := make([]llm.Message, len(msgs))
	for i, m := range msgs {
		content := make([]llm.Content, len(m.Content))
		copy(content, m.Content)
		for j, c := range content {
			if c.Type != llm.ContentToolResult {
				continue
			}
			if seen < toElide {
				content[j].ToolResult = elidedNote
			}
			seen++
		}
		out[i] = llm.Message{Role: m.Role, Content: content}
	}
	return out
}

// truncate shortens a string for display, adding an ellipsis if it was cut.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
