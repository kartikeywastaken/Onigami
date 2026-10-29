package ui

import (
	"strings"

	"onigami/internal/agent"
)

// agentStatus follows the garden's growth states: a seed in the soil, a sprout,
// a grown flower — and the aka-oni that eats a failed command.
type agentStatus int

const (
	statusSeed     agentStatus = iota // idle — a seed (種)
	statusSprout                      // working — a sprout (芽)
	statusGrown                       // reported — a grown flower (育)
	statusDevoured                    // errored — eaten by the oni (鬼)
)

type agentState struct {
	role    agent.Role
	status  agentStatus
	current string // the tool it's running, or a short note
}

// roster tracks every agent's live status, derived purely from the event stream.
type roster struct {
	order  []agent.Role
	byRole map[agent.Role]*agentState
}

func newRoster() *roster {
	order := []agent.Role{
		agent.RoleOrchestrator, agent.RoleAnalyst, agent.RoleWebTester,
		agent.RoleExploitDev, agent.RoleExploiter,
	}
	byRole := make(map[agent.Role]*agentState, len(order))
	for _, r := range order {
		byRole[r] = &agentState{role: r, status: statusSeed}
	}
	return &roster{order: order, byRole: byRole}
}

// apply advances an agent's status from one event.
func (rs *roster) apply(e agent.Event) {
	s := rs.byRole[e.Agent]
	if s == nil {
		return // events from non-roster sources (e.g. a final notice) are ignored
	}
	switch e.Kind {
	case agent.EventStart:
		s.status, s.current = statusSprout, "starting…"
	case agent.EventToolCall:
		s.status, s.current = statusSprout, clip(oneLine(e.Text), 48)
	case agent.EventThinking:
		s.status, s.current = statusSprout, "thinking…"
	case agent.EventToolResult, agent.EventMessage:
		s.status = statusSprout
	case agent.EventReport:
		s.status, s.current = statusGrown, "reported"
	case agent.EventError:
		s.status, s.current = statusDevoured, clip(oneLine(e.Text), 48)
	}
}

// view renders the roster into a fixed-height block (width×height content area).
// spinnerFrame is the current spinner glyph for sprouting (working) agents.
func (rs *roster) view(s styles, spinnerFrame string, width, height int) string {
	lines := make([]string, 0, height)
	for _, r := range rs.order {
		st := rs.byRole[r]

		var mark string
		switch st.status {
		case statusSprout:
			mark = s.gold.Render(spinnerFrame) // a sprout on a gold stem
		case statusGrown:
			mark = s.success.Render("✿") // the grown vermilion flower
		case statusDevoured:
			mark = s.errText.Render("鬼") // eaten by the aka-oni
		default:
			mark = s.dim.Render("·") // a seed in the soil
		}

		label := s.role(string(r)).Render(string(r))
		// Pad each line to the full width on the green ground (s.body carries the
		// background) so no space between/around styled segments shows black.
		lines = append(lines, s.body.Width(width).Render(mark+s.body.Render(" ")+label))

		// A sub-line with the current tool/note, for the agent that's doing work.
		if st.current != "" && (st.status == statusSprout || st.status == statusDevoured) {
			note := s.dim.Render(clip(st.current, width-4))
			lines = append(lines, s.body.Width(width).Render(s.body.Render("   ")+note))
		}
	}

	// Pad or truncate to exactly `height` lines so the pane never overflows.
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}
