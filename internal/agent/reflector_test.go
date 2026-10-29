package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"onigami/internal/llm"
)

// scriptedLLM returns a pre-set sequence of responses, one per Complete call,
// and records how many calls it saw. The last response repeats once exhausted.
type scriptedLLM struct {
	responses []llm.Response
	calls     int
}

func (s *scriptedLLM) Complete(_ context.Context, _ llm.Request) (llm.Response, error) {
	i := s.calls
	s.calls++
	if i >= len(s.responses) {
		i = len(s.responses) - 1
	}
	return s.responses[i], nil
}

func textReport(s string) llm.Response {
	return llm.Response{
		Message:    llm.Message{Role: llm.RoleAssistant, Content: []llm.Content{{Type: llm.ContentText, Text: s}}},
		StopReason: llm.StopEndTurn,
	}
}

func toolCall(name, args string) llm.Response {
	return llm.Response{
		Message: llm.Message{Role: llm.RoleAssistant, Content: []llm.Content{
			{Type: llm.ContentToolUse, ToolID: "c1", ToolName: name, ToolInput: json.RawMessage(args)},
		}},
		StopReason: llm.StopToolUse,
	}
}

// noopTool is a minimal in-scope tool the scripted model can "call".
type noopTool struct{}

func (noopTool) Name() string            { return "probe" }
func (noopTool) Description() string     { return "a test tool" }
func (noopTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (noopTool) Run(context.Context, json.RawMessage) (string, error) {
	return "tool ran: observed a real result", nil
}

// TestReflectorBouncesNoToolReport is the guard's core contract: a RequireToolUse
// agent that tries to finish WITHOUT ever running a tool is nudged back, and the
// model is given another chance (so the narrated report is not accepted as-is).
func TestReflectorBouncesNoToolReport(t *testing.T) {
	// Turn 1: narrate a fake result (no tool call). After the nudge, turn 2 calls
	// the real tool, then turn 3 writes the grounded report.
	llmStub := &scriptedLLM{responses: []llm.Response{
		textReport("I ran the probe and confirmed the issue."), // narration — must be bounced
		toolCall("probe", `{}`),
		textReport("Grounded report: the probe returned a real result."),
	}}
	a := &Agent{
		Role: RoleWebTester, System: "test", Tools: NewToolSet(noopTool{}),
		LLM: llmStub, Model: "stub", MaxTurns: 10, RequireToolUse: true,
	}
	rep, err := a.Run(context.Background(), Task{Objective: "t", Brief: "b"}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if llmStub.calls < 3 {
		t.Fatalf("expected the no-tool report to be bounced (>=3 model calls), got %d", llmStub.calls)
	}
	if !strings.Contains(rep.Summary, "Grounded report") {
		t.Errorf("expected the grounded report after the tool ran, got: %q", rep.Summary)
	}
}

// TestReflectorEventuallyGivesUp checks the guard is bounded: a model that NEVER
// runs a tool is nudged at most twice, then its report is accepted rather than
// looping forever.
func TestReflectorEventuallyGivesUp(t *testing.T) {
	llmStub := &scriptedLLM{responses: []llm.Response{textReport("still just narrating, no tool")}}
	a := &Agent{
		Role: RoleAnalyst, System: "test", Tools: NewToolSet(noopTool{}),
		LLM: llmStub, Model: "stub", MaxTurns: 10, RequireToolUse: true,
	}
	rep, err := a.Run(context.Background(), Task{Objective: "t", Brief: "b"}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// 1 initial + 2 nudges = 3 calls, then accepted.
	if llmStub.calls != 3 {
		t.Errorf("expected exactly 3 calls (initial + 2 bounded nudges), got %d", llmStub.calls)
	}
	if rep.Summary == "" {
		t.Error("expected the report to be accepted after the nudge budget, got empty")
	}
}

// TestReflectorOffWhenNotRequired confirms a prose-only role (RequireToolUse
// false, e.g. the exploit-dev) is NOT bounced for finishing without a tool.
func TestReflectorOffWhenNotRequired(t *testing.T) {
	llmStub := &scriptedLLM{responses: []llm.Response{textReport("here is the PoC, reasoned in prose")}}
	a := &Agent{
		Role: RoleExploitDev, System: "test", Tools: NewToolSet(noopTool{}),
		LLM: llmStub, Model: "stub", MaxTurns: 10, RequireToolUse: false,
	}
	if _, err := a.Run(context.Background(), Task{Objective: "t", Brief: "b"}, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if llmStub.calls != 1 {
		t.Errorf("prose-only role should not be nudged; expected 1 call, got %d", llmStub.calls)
	}
}
