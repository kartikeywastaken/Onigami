package ui

import (
	"image/color"
	"os"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
)

// styles is the whole visual theme. It follows the onigami 鬼神 "朱塗 Shrine"
// design system (see the reference design doc): torii vermilion for the flower
// (focus, the active agent, a grown finding), gold leaf for stems (structure,
// panes, work in progress), rice-paper ink for text, on lacquer black. Colors
// are chosen per light/dark background so they stay readable either way; the
// vermilion/gold/ink reads as the shrine on any dark terminal.
type styles struct {
	// raw palette (exposed for components that take colors directly)
	flower color.Color // vermilion — focus, active, grown
	stem   color.Color // gold — structure, in-progress
	ink    color.Color // rice paper — body text
	muted  color.Color // labels, metadata
	petal  color.Color // highlights
	oni    color.Color // aka-oni red — errors
	line   color.Color // hairlines
	bg     color.Color // the green-tinted ground behind everything

	app     lipgloss.Style // full-screen background wrapper (the green tint)
	brand   lipgloss.Style
	kanji   lipgloss.Style
	scope   lipgloss.Style
	meta    lipgloss.Style
	dim     lipgloss.Style
	accent  lipgloss.Style // vermilion text
	gold    lipgloss.Style // gold text
	body    lipgloss.Style
	errText lipgloss.Style
	success lipgloss.Style // grown flower (vermilion)

	paneTitle   lipgloss.Style
	pane        lipgloss.Style // gold stem border
	paneFocused lipgloss.Style // vermilion flower border
	modal       lipgloss.Style

	roles map[string]lipgloss.Style
}

func newStyles(isDark bool) styles {
	ld := lipgloss.LightDark(isDark)

	// Green/yellow scheme, with darker twins for light terminals.
	// flower = yellow/amber (brand, focus, the active thing); stem = green
	// (structure, work in progress, success — the sprout that grows).
	flower := ld(lipgloss.Color("#B45309"), lipgloss.Color("#FACC15")) // amber/yellow
	stem := ld(lipgloss.Color("#15803D"), lipgloss.Color("#4ADE80"))   // green
	ink := ld(lipgloss.Color("#1F2937"), lipgloss.Color("#F3F4E6"))    // soft warm white
	muted := ld(lipgloss.Color("#6B7280"), lipgloss.Color("#9CA3AF"))
	petal := ld(lipgloss.Color("#166534"), lipgloss.Color("#86EFAC")) // light green
	oni := ld(lipgloss.Color("#C4201B"), lipgloss.Color("#F87171"))   // red — errors/danger
	line := ld(lipgloss.Color("#D1D5DB"), lipgloss.Color("#44403C"))
	lime := ld(lipgloss.Color("#4D7C0F"), lipgloss.Color("#A3E635")) // yellow-green
	emerald := ld(lipgloss.Color("#0D9488"), lipgloss.Color("#2DD4BF"))

	// The green tint the whole dashboard sits on: a near-black green in the dark,
	// a pale mint in the light. Every style below carries it as its background so
	// the tint is uniform — no glyph cell falls back to the terminal default and
	// leaves the panes looking patchy. Overridable with ONIGAMI_UI_BG=#rrggbb.
	bg := ld(lipgloss.Color("#EAF3EC"), lipgloss.Color("#0C1410"))
	if hex := strings.TrimSpace(os.Getenv("ONIGAMI_UI_BG")); hex != "" {
		bg = lipgloss.Color(hex)
	}
	// Panes are faintly lighter/darker than the ground so their edges read.
	paneBg := ld(lipgloss.Color("#DFEEE3"), lipgloss.Color("#0F1A14"))

	// txt builds a foreground style already carrying the ground background, so
	// text cells match the tint. tile does the same for the pane backgrounds.
	txt := func(fg color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(fg).Background(bg) }
	tile := func() lipgloss.Style {
		return lipgloss.NewStyle().Background(paneBg).Border(lipgloss.RoundedBorder()).Padding(0, 1)
	}

	// Agents spread across the green/yellow family; the brain is yellow, the
	// dangerous exploiter stays red.
	roles := map[string]lipgloss.Style{
		"orchestrator": txt(flower).Bold(true),
		"analyst":      txt(stem),
		"web-tester":   txt(lime),
		"exploit-dev":  txt(emerald),
		"exploiter":    txt(oni).Bold(true),
	}

	return styles{
		flower: flower, stem: stem, ink: ink, muted: muted, petal: petal, oni: oni, line: line, bg: bg,

		app:     lipgloss.NewStyle().Background(bg),
		brand:   txt(flower).Bold(true),
		kanji:   txt(stem).Bold(true),
		scope:   txt(ink),
		meta:    txt(muted),
		dim:     txt(line),
		accent:  txt(flower),
		gold:    txt(stem),
		body:    txt(ink),
		errText: txt(oni),
		success: txt(stem),

		paneTitle:   txt(stem).Bold(true),
		pane:        tile().BorderForeground(line).BorderBackground(bg),
		paneFocused: tile().BorderForeground(flower).BorderBackground(bg),
		modal:       lipgloss.NewStyle().Background(paneBg).Border(lipgloss.RoundedBorder()).BorderForeground(flower).BorderBackground(bg).Padding(1, 3),

		roles: roles,
	}
}

// role returns the label style for an agent, defaulting to muted.
func (s styles) role(r string) lipgloss.Style {
	if st, ok := s.roles[r]; ok {
		return st
	}
	return s.meta
}

// tableStyles themes the findings table: gold header (stems), vermilion selected
// row (the grown flower), rice-paper cells. It builds on DefaultStyles so the
// standard cell padding and header rule are kept — only the colors change.
func (s styles) tableStyles() table.Styles {
	st := table.DefaultStyles()
	st.Header = st.Header.Bold(true).Foreground(s.stem).Background(s.bg).BorderForeground(s.line).BorderBackground(s.bg)
	st.Cell = st.Cell.Foreground(s.ink).Background(s.bg)
	st.Selected = st.Selected.Bold(true).Foreground(s.flower).Background(s.bg)
	return st
}
