// Package ui is onigami's optional terminal dashboard (opt-in via --tui).
//
// It is a thin consumer of the engagement machinery, not a rewrite of it: the
// orchestrator and workers already emit everything worth showing through an
// agent.Observer and gate risky actions through an agent.Approver. This package
// turns that event stream into a live three-pane Bubble Tea (v2) dashboard — an
// agent roster, a scrolling activity log, and a findings table — and turns the
// approval gate into a centered modal.
//
// The engagement runs on its own goroutine; the two callbacks bridge back into
// the Elm loop the correct way — never mutating the model from the goroutine,
// only sending it typed messages via Program.Send.
package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"onigami/internal/agent"
	"onigami/internal/tools"
)

// Config is what main hands the dashboard to run one engagement.
type Config struct {
	Scope    string               // the in-scope target(s), shown in the header
	Findings *tools.FindingsStore // polled to fill the findings table
	// Start runs the blocking engagement. The UI supplies the observer (feeds the
	// activity log + roster) and the approver (the approval modal), and Start
	// returns the final report summary and any error.
	Start func(obs agent.Observer, approver agent.Approver) (string, error)
}

// --- messages that cross from the engagement goroutine into the Elm loop -----

type eventMsg struct{ e agent.Event }

// approvalReqMsg carries a decision channel: the approver blocks on it until the
// user answers the modal, which lets the TUI service a synchronous gate safely.
type approvalReqMsg struct {
	role  agent.Role
	reply chan bool
}

type doneMsg struct {
	summary string
	err     error
}

type tickMsg struct{}

// Run shows the dashboard for one engagement and blocks until the user quits.
// It returns the final report summary and the engagement's error (if any).
func Run(cfg Config) (string, error) {
	m := newModel(cfg)
	prog := tea.NewProgram(m)

	// Observer bridge: every agent event becomes one message. Send is safe from
	// another goroutine; the model is only ever mutated inside Update.
	obs := func(e agent.Event) { prog.Send(eventMsg{e}) }

	// Approver bridge: ask the UI, then block this (engagement) goroutine on the
	// reply channel until the user decides, or the context is cancelled.
	approver := func(ctx context.Context, role agent.Role, _ string) bool {
		reply := make(chan bool, 1)
		prog.Send(approvalReqMsg{role: role, reply: reply})
		select {
		case ok := <-reply:
			return ok
		case <-ctx.Done():
			return false
		}
	}

	go func() {
		summary, err := cfg.Start(obs, approver)
		prog.Send(doneMsg{summary: summary, err: err})
	}()

	final, err := prog.Run()
	if err != nil {
		return "", err
	}
	fm, _ := final.(model)
	return fm.summary, fm.doneErr
}
