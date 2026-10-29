package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"onigami/internal/agent"
	"onigami/internal/tools"
)

// step applies one message and returns the updated model, failing if the model
// type ever changes out from under us.
func step(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	next, _ := m.Update(msg)
	nm, ok := next.(model)
	if !ok {
		t.Fatalf("Update returned %T, want ui.model", next)
	}
	return nm
}

func testModel() model {
	return newModel(Config{
		Scope:    "demo.testfire.net",
		Findings: tools.NewFindingsStore(),
		Start:    func(agent.Observer, agent.Approver) (string, error) { return "", nil },
	})
}

// TestFlow drives the dashboard through a realistic sequence and asserts the
// state transitions and that rendering never panics or comes back empty.
func TestFlow(t *testing.T) {
	m := testModel()
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if !m.ready {
		t.Fatal("model not ready after WindowSizeMsg")
	}

	// A spread of event kinds should all render without panicking.
	events := []agent.Event{
		{Agent: agent.RoleOrchestrator, Kind: agent.EventStart, Text: "assess target", Depth: 0},
		{Agent: agent.RoleAnalyst, Kind: agent.EventToolCall, Text: "recon_sweep {target:demo}", Depth: 1},
		{Agent: agent.RoleAnalyst, Kind: agent.EventToolResult, Text: strings.Repeat("port ", 300), Depth: 1},
		{Agent: agent.RoleWebTester, Kind: agent.EventThinking, Text: "testing /login.jsp", Depth: 1},
		{Agent: agent.RoleAnalyst, Kind: agent.EventReport, Text: "done", Depth: 1},
		{Agent: agent.RoleExploitDev, Kind: agent.EventError, Text: "boom", Depth: 1},
	}
	for _, e := range events {
		m = step(t, m, eventMsg{e})
	}
	if len(m.events) != len(events) {
		t.Fatalf("got %d events, want %d", len(m.events), len(events))
	}
	if v := m.View(); strings.TrimSpace(v.Content) == "" {
		t.Fatal("View content is empty after events")
	}

	// Scrollback must be bounded by the cap.
	m.maxEvents = 3
	for i := 0; i < 10; i++ {
		m = step(t, m, eventMsg{agent.Event{Agent: agent.RoleAnalyst, Kind: agent.EventMessage, Text: "x"}})
	}
	if len(m.events) > 3 {
		t.Fatalf("scrollback not capped: %d events, cap 3", len(m.events))
	}
}

// TestApprovalGate checks the proceed/hold bridge: an approval request parks the
// model, and the keypress answers the waiting channel and clears the gate.
func TestApprovalGate(t *testing.T) {
	m := testModel()
	m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	// proceed
	reply := make(chan bool, 1)
	m = step(t, m, approvalReqMsg{role: agent.RoleExploitDev, reply: reply})
	if m.pending == nil {
		t.Fatal("pending approval not set")
	}
	m = step(t, m, tea.KeyPressMsg{Code: 'p', Text: "p"})
	if m.pending != nil {
		t.Fatal("pending approval not cleared after 'p'")
	}
	if got := <-reply; got != true {
		t.Fatalf("approver got %v, want true", got)
	}

	// hold
	reply2 := make(chan bool, 1)
	m = step(t, m, approvalReqMsg{role: agent.RoleExploiter, reply: reply2})
	m = step(t, m, tea.KeyPressMsg{Code: 'h', Text: "h"})
	if got := <-reply2; got != false {
		t.Fatalf("approver got %v, want false", got)
	}
}

// TestDone marks the run finished and freezes the dashboard without panicking.
func TestDone(t *testing.T) {
	m := testModel()
	m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m = step(t, m, doneMsg{summary: "all clear", err: nil})
	if !m.done || m.summary != "all clear" {
		t.Fatalf("done=%v summary=%q", m.done, m.summary)
	}
	if v := m.View(); !strings.Contains(v.Content, "finished") {
		t.Fatal("finished state not shown in footer")
	}
}

// TestTooSmall renders a guard message instead of a broken layout.
func TestTooSmall(t *testing.T) {
	m := testModel()
	m = step(t, m, tea.WindowSizeMsg{Width: 20, Height: 5})
	if v := m.View(); !strings.Contains(v.Content, "too small") {
		t.Fatal("expected too-small guard message")
	}
}
