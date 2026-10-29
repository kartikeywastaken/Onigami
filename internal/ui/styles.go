package ui

import (
	"image/color"
	"os"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// styles is the whole visual theme. onigami sits on ONE green ground: that green
// is set as the terminal's background (tea.View.BackgroundColor) — a real back
// layer that loads first — and every piece of text is drawn FOREGROUND-ONLY on
// top of it. Nothing paints its own background (except a single selection
// highlight), so no cell ever falls back to a competing dark fill: the green
// shows through uniformly, and streaming output can't leave black gaps.
type styles struct {
	// raw palette
	flower color.Color // amber/yellow — focus, active, brand
	stem   color.Color // green — structure, work, success
	ink    color.Color // body text
	muted  color.Color // labels, metadata
	petal  color.Color // highlights
	oni    color.Color // red — errors
	line   color.Color // hairlines / borders
	bg     color.Color // the green ground (set as the terminal background)
	selBg  color.Color // the one intentional background: the selected table row

	brand   lipgloss.Style
	kanji   lipgloss.Style
	scope   lipgloss.Style
	meta    lipgloss.Style
	dim     lipgloss.Style
	accent  lipgloss.Style
	gold    lipgloss.Style
	body    lipgloss.Style
	errText lipgloss.Style
	success lipgloss.Style

	paneTitle   lipgloss.Style
	pane        lipgloss.Style
	paneFocused lipgloss.Style
	modal       lipgloss.Style

	// landing screen
	logo       lipgloss.Style
	tagline    lipgloss.Style
	inputFrame lipgloss.Style
	fieldLabel lipgloss.Style
	hint       lipgloss.Style
	inputTheme textinput.Styles

	roles map[string]lipgloss.Style
}

func newStyles(isDark bool) styles {
	ld := lipgloss.LightDark(isDark)

	// Green/yellow scheme. flower = amber/yellow (brand, focus, active); stem =
	// green (structure, work, success — the sprout that grows).
	flower := ld(lipgloss.Color("#B45309"), lipgloss.Color("#FACC15"))
	stem := ld(lipgloss.Color("#15803D"), lipgloss.Color("#4ADE80"))
	ink := ld(lipgloss.Color("#1F2937"), lipgloss.Color("#F3F4E6"))
	muted := ld(lipgloss.Color("#6B7280"), lipgloss.Color("#9CA3AF"))
	petal := ld(lipgloss.Color("#166534"), lipgloss.Color("#86EFAC"))
	oni := ld(lipgloss.Color("#C4201B"), lipgloss.Color("#F87171"))
	line := ld(lipgloss.Color("#9CBFA6"), lipgloss.Color("#3F6B4A")) // hairline green
	lime := ld(lipgloss.Color("#4D7C0F"), lipgloss.Color("#A3E635"))
	emerald := ld(lipgloss.Color("#0D9488"), lipgloss.Color("#2DD4BF"))

	// The green ground the whole dashboard sits on — set as the terminal's own
	// background so it is a true back layer. Near-black green in the dark, pale
	// mint in the light. Overridable with ONIGAMI_UI_BG=#rrggbb.
	bg := ld(lipgloss.Color("#EAF3EC"), lipgloss.Color("#0C1410"))
	if hex := strings.TrimSpace(os.Getenv("ONIGAMI_UI_BG")); hex != "" {
		bg = lipgloss.Color(hex)
	}
	// The one intentional fill: the selected findings row, a touch lighter than
	// the ground so the selection reads without a hard black box.
	selBg := ld(lipgloss.Color("#CDE6D4"), lipgloss.Color("#1C3326"))

	// fg builds a foreground-only style: no background, so the terminal's green
	// ground shows through every cell the glyph doesn't fully cover.
	fg := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

	roles := map[string]lipgloss.Style{
		"orchestrator": fg(flower).Bold(true),
		"analyst":      fg(stem),
		"web-tester":   fg(lime),
		"exploit-dev":  fg(emerald),
		"exploiter":    fg(oni).Bold(true),
	}

	// Panes: a rounded border in a fg color, interior transparent (green shows).
	paneBase := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)

	// Text input themed foreground-only; the cursor is the flower.
	it := textinput.DefaultStyles(isDark)
	it.Cursor.Color = flower
	it.Focused.Prompt = fg(flower)
	it.Focused.Text = fg(ink)
	it.Focused.Placeholder = fg(muted)
	it.Blurred.Prompt = fg(muted)
	it.Blurred.Text = fg(ink)
	it.Blurred.Placeholder = fg(muted)

	return styles{
		flower: flower, stem: stem, ink: ink, muted: muted, petal: petal, oni: oni, line: line, bg: bg, selBg: selBg,

		brand:   fg(flower).Bold(true),
		kanji:   fg(stem).Bold(true),
		scope:   fg(ink),
		meta:    fg(muted),
		dim:     fg(line),
		accent:  fg(flower),
		gold:    fg(stem),
		body:    fg(ink),
		errText: fg(oni),
		success: fg(stem),

		paneTitle:   fg(stem).Bold(true),
		pane:        paneBase.BorderForeground(line),
		paneFocused: paneBase.BorderForeground(flower),
		modal:       lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(flower).Padding(1, 3),

		logo:       fg(flower).Bold(true),
		tagline:    fg(muted),
		inputFrame: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(flower).Padding(1, 2),
		fieldLabel: fg(stem).Bold(true),
		hint:       fg(muted),
		inputTheme: it,

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

// tableStyles themes the findings table foreground-only: a green header, amber
// selected row on a faint lighter-green fill (the one deliberate background), and
// rice-paper cells — all transparent otherwise so the ground shows through.
func (s styles) tableStyles() table.Styles {
	st := table.DefaultStyles()
	st.Header = st.Header.Bold(true).Foreground(s.stem).BorderForeground(s.line)
	st.Cell = st.Cell.Foreground(s.ink)
	st.Selected = st.Selected.Bold(true).Foreground(s.flower).Background(s.selBg)
	return st
}
