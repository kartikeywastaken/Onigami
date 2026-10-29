package ui

import "charm.land/lipgloss/v2"

// styles is the whole visual theme, rebuilt whenever we learn the terminal's
// background (light vs dark) so colors stay readable either way.
type styles struct {
	brand   lipgloss.Style
	scope   lipgloss.Style
	meta    lipgloss.Style
	footer  lipgloss.Style
	key     lipgloss.Style
	dim     lipgloss.Style
	accent  lipgloss.Style
	errText lipgloss.Style
	success lipgloss.Style
	warn    lipgloss.Style
	rule    lipgloss.Style

	// roles maps an agent role to its label color, so each specialist is
	// recognizable at a glance in the activity stream.
	roles map[string]lipgloss.Style
}

func newStyles(isDark bool) styles {
	ld := lipgloss.LightDark(isDark)

	accent := ld(lipgloss.Color("#4F46E5"), lipgloss.Color("#818CF8"))
	muted := ld(lipgloss.Color("#6B7280"), lipgloss.Color("#9CA3AF"))
	subtle := ld(lipgloss.Color("#9CA3AF"), lipgloss.Color("#6B7280"))
	errc := ld(lipgloss.Color("#B91C1C"), lipgloss.Color("#F87171"))
	okc := ld(lipgloss.Color("#047857"), lipgloss.Color("#34D399"))
	warnc := ld(lipgloss.Color("#B45309"), lipgloss.Color("#FBBF24"))
	fg := ld(lipgloss.Color("#111827"), lipgloss.Color("#E5E7EB"))

	roles := map[string]lipgloss.Style{
		"orchestrator": lipgloss.NewStyle().Bold(true).Foreground(accent),
		"analyst":      lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#0E7490"), lipgloss.Color("#22D3EE"))),
		"web-tester":   lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#9333EA"), lipgloss.Color("#C084FC"))),
		"exploit-dev":  lipgloss.NewStyle().Foreground(ld(lipgloss.Color("#C2410C"), lipgloss.Color("#FB923C"))),
		"exploiter":    lipgloss.NewStyle().Bold(true).Foreground(errc),
	}

	return styles{
		brand:   lipgloss.NewStyle().Bold(true).Foreground(accent),
		scope:   lipgloss.NewStyle().Foreground(fg),
		meta:    lipgloss.NewStyle().Foreground(muted),
		footer:  lipgloss.NewStyle().Foreground(muted),
		key:     lipgloss.NewStyle().Bold(true).Foreground(accent),
		dim:     lipgloss.NewStyle().Foreground(subtle),
		accent:  lipgloss.NewStyle().Foreground(accent),
		errText: lipgloss.NewStyle().Foreground(errc),
		success: lipgloss.NewStyle().Foreground(okc),
		warn:    lipgloss.NewStyle().Bold(true).Foreground(warnc),
		rule:    lipgloss.NewStyle().Foreground(subtle),
		roles:   roles,
	}
}

// role returns the label style for an agent, falling back to muted for any
// role we don't have a dedicated color for.
func (s styles) role(r string) lipgloss.Style {
	if st, ok := s.roles[r]; ok {
		return st
	}
	return s.meta
}

// keyHint renders a "key action" pair for the footer help line.
func (s styles) keyHint(k, action string) string {
	return s.key.Render(k) + " " + s.footer.Render(action)
}
