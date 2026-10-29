package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"onigami/internal/agent"
	"onigami/internal/tools"
)

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
		Findings:       tools.NewFindingsStore(),
		PrefillTarget:  "demo.testfire.net",
		PrefillMission: "assess the target",
		Start: func(string, string, agent.Observer, agent.Approver) (string, error) {
			return "", nil
		},
	})
}

// dashboardModel returns a model advanced past the landing screen into the live
// dashboard (the landing form submitted with the prefilled target + mission), so
// dashboard-focused tests start where they expect.
func dashboardModel(t *testing.T, w, h int) model {
	t.Helper()
	m := testModel()
	m = step(t, m, tea.WindowSizeMsg{Width: w, Height: h})
	m, _ = m.submitLanding()
	if m.phase != phaseDashboard {
		t.Fatalf("expected to reach the dashboard after submit, got phase %v (err %q)", m.phase, m.inputError)
	}
	return m
}

// assertFits fails if the rendered view is taller/wider than the terminal — the
// one thing that breaks an alt-screen TUI (the top scrolls off and it flickers).
func assertFits(t *testing.T, m model, w, h int) {
	t.Helper()
	v := m.View()
	if got := lipgloss.Height(v.Content); got > h {
		t.Fatalf("view height %d exceeds terminal height %d at %dx%d", got, h, w, h)
	}
	if got := lipgloss.Width(v.Content); got > w {
		t.Fatalf("view width %d exceeds terminal width %d at %dx%d", got, w, h, w)
	}
}

// TestFlow drives the dashboard through a realistic sequence and asserts the
// layout fits, events render, and the roster advances with the growth states.
func TestFlow(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {60, 16}, {200, 50}} {
		w, h := sz[0], sz[1]
		m := dashboardModel(t, w, h)
		if !m.ready {
			t.Fatalf("not ready after WindowSizeMsg at %dx%d", w, h)
		}

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
		assertFits(t, m, w, h)

		if m.agents.byRole[agent.RoleAnalyst].status != statusGrown {
			t.Errorf("analyst should be grown (reported) at %dx%d", w, h)
		}
		if m.agents.byRole[agent.RoleExploitDev].status != statusDevoured {
			t.Errorf("exploit-dev should be devoured (errored) at %dx%d", w, h)
		}
		if strings.TrimSpace(m.View().Content) == "" {
			t.Fatalf("empty view at %dx%d", w, h)
		}
	}
}

// TestFindingsTable checks the findings pane fills from the shared store on tick.
func TestFindingsTable(t *testing.T) {
	m := dashboardModel(t, 100, 30)
	m.cfg.Findings.Add("recon", "discover", "port 80/tcp open: Apache-Coyote", "")
	m.cfg.Findings.Add("recon", "enumerate", "/login.jsp", "")
	m = step(t, m, tickMsg{})
	if m.findingsCount != 2 {
		t.Fatalf("findingsCount=%d, want 2", m.findingsCount)
	}
	if n := len(m.findings.Rows()); n != 2 {
		t.Fatalf("findings table has %d rows, want 2", n)
	}
	assertFits(t, m, 100, 30)
}

// TestApprovalModal: a request shows the modal and keys answer the gate.
func TestApprovalModal(t *testing.T) {
	m := dashboardModel(t, 90, 28)

	reply := make(chan bool, 1)
	m = step(t, m, approvalReqMsg{role: agent.RoleExploitDev, reply: reply})
	if m.pending == nil {
		t.Fatal("pending not set")
	}
	if !strings.Contains(m.View().Content, "approval required") {
		t.Fatal("modal not shown")
	}
	m = step(t, m, tea.KeyPressMsg{Code: 'p', Text: "p"})
	if m.pending != nil {
		t.Fatal("pending not cleared after proceed")
	}
	if got := <-reply; !got {
		t.Fatal("approver should have received true")
	}

	reply2 := make(chan bool, 1)
	m = step(t, m, approvalReqMsg{role: agent.RoleExploiter, reply: reply2})
	m = step(t, m, tea.KeyPressMsg{Code: 'h', Text: "h"})
	if got := <-reply2; got {
		t.Fatal("approver should have received false")
	}
}

// TestFocusToggle cycles focus between the activity and findings panes.
func TestFocusToggle(t *testing.T) {
	m := dashboardModel(t, 90, 28)
	if m.focus != paneActivity {
		t.Fatal("should start focused on activity")
	}
	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focus != paneFindings {
		t.Fatal("tab should move focus to findings")
	}
	if !m.findings.Focused() {
		t.Fatal("findings table should be focused")
	}
}

// TestDone marks the run finished and freezes the dashboard without panicking.
func TestDone(t *testing.T) {
	m := dashboardModel(t, 90, 28)
	m = step(t, m, doneMsg{summary: "all clear", err: nil})
	if !m.done || m.summary != "all clear" {
		t.Fatalf("done=%v summary=%q", m.done, m.summary)
	}
	if !strings.Contains(m.View().Content, "finished") {
		t.Fatal("finished state not shown")
	}
	assertFits(t, m, 90, 28)
}

// TestLanding checks the first screen shows the wordmark + input box and that
// submitting a valid form transitions to the dashboard.
func TestLanding(t *testing.T) {
	m := testModel()
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.phase != phaseLanding {
		t.Fatal("should start on the landing screen")
	}
	view := m.View().Content
	if !strings.Contains(view, "Target") || !strings.Contains(view, "Mission") {
		t.Fatal("landing screen should show the Target and Mission fields")
	}
	assertFits(t, m, 100, 30)

	// Enter submits the prefilled form and moves to the dashboard.
	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.phase != phaseDashboard {
		t.Fatalf("enter should move to the dashboard, still on %v", m.phase)
	}
	if m.scope != "demo.testfire.net" {
		t.Errorf("submitted target not captured as scope: %q", m.scope)
	}
}

// TestLandingRejectsEmpty keeps the landing screen until a target is entered.
func TestLandingRejectsEmpty(t *testing.T) {
	m := newModel(Config{
		Findings: tools.NewFindingsStore(),
		Start:    func(string, string, agent.Observer, agent.Approver) (string, error) { return "", nil },
	})
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.phase != phaseLanding {
		t.Fatal("empty form must not submit")
	}
	if m.inputError == "" {
		t.Fatal("expected an inline error for the empty target")
	}
}

// TestTooSmall renders a guard message instead of a broken layout.
func TestTooSmall(t *testing.T) {
	m := testModel()
	m = step(t, m, tea.WindowSizeMsg{Width: 30, Height: 8})
	if !strings.Contains(m.View().Content, "too small") {
		t.Fatal("expected too-small guard")
	}
}
