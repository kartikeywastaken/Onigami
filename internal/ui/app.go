package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/stopwatch"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"onigami/internal/agent"
	"onigami/internal/config"
)

// phase is which screen the UI is showing.
type phase int

const (
	phaseLanding   phase = iota // the title + target/mission input box
	phaseDashboard              // the live three-pane engagement dashboard
)

// pane identifies the scroll-focusable panes (the roster is status-only).
type pane int

const (
	paneActivity pane = iota
	paneFindings
)

// landingField identifies which input the landing form has focused.
type landingField int

const (
	fieldTarget landingField = iota
	fieldMission
)

// startParams is what the landing form submits to launch the engagement.
type startParams struct {
	target  string
	mission string
}

type model struct {
	cfg    Config
	styles styles
	keys   keyMap

	help     help.Model
	watch    stopwatch.Model
	sp       spinner.Model
	activity viewport.Model
	findings table.Model
	agents   *roster

	width, height int
	ready         bool
	isDark        bool
	phase         phase
	focus         pane

	// landing form
	target     textinput.Model
	mission    textinput.Model
	field      landingField
	inputError string
	startCh    chan startParams // Run waits on this; nil in tests
	scope      string           // the submitted target, shown in the header

	events    []agent.Event
	maxEvents int
	follow    bool
	tickEvery time.Duration

	findingsCount    int
	findingsSummaryW int

	// computed pane geometry (set in layout)
	rosterW, activityW, topH, findingsH int

	done    bool
	doneErr error
	summary string

	pending *approvalReqMsg
}

func newModel(cfg Config) model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot

	target := textinput.New()
	target.Placeholder = "demo.testfire.net  (host, IP, CIDR, or URL)"
	target.SetValue(cfg.PrefillTarget)
	mission := textinput.New()
	mission.Placeholder = "Find and PoC an exploitable web vulnerability"
	mission.SetValue(cfg.PrefillMission)

	m := model{
		cfg:       cfg,
		styles:    newStyles(true), // rebuilt once we learn the real background
		keys:      newKeyMap(),
		help:      help.New(),
		watch:     stopwatch.New(stopwatch.WithInterval(time.Second)),
		sp:        sp,
		activity:  viewport.New(),
		findings:  table.New(),
		agents:    newRoster(),
		phase:     phaseLanding,
		follow:    true,
		focus:     paneActivity,
		target:    target,
		mission:   mission,
		field:     fieldTarget,
		maxEvents: config.IntEnv("ONIGAMI_UI_SCROLLBACK", 500),
		tickEvery: config.SecondsEnv("ONIGAMI_UI_TICK", time.Second),
	}
	(&m).restyle()
	(&m).focusField(fieldTarget)
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.sp.Tick, textinput.Blink)
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{} })
}

// restyle (re)applies the theme to every styled sub-component.
func (m *model) restyle() {
	m.sp.Style = m.styles.gold
	m.findings.SetStyles(m.styles.tableStyles())
	m.target.SetStyles(m.styles.inputTheme)
	m.mission.SetStyles(m.styles.inputTheme)
	// Help keys/descriptions are foreground-only, matching the single green ground.
	m.help.Styles.ShortKey = lipgloss.NewStyle().Foreground(m.styles.flower)
	m.help.Styles.ShortDesc = lipgloss.NewStyle().Foreground(m.styles.muted)
	m.help.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(m.styles.line)
	m.help.Styles.FullKey = lipgloss.NewStyle().Foreground(m.styles.flower)
	m.help.Styles.FullDesc = lipgloss.NewStyle().Foreground(m.styles.muted)
	m.help.Styles.FullSeparator = lipgloss.NewStyle().Foreground(m.styles.line)
}

// focusField moves focus between the two landing inputs.
func (m *model) focusField(f landingField) {
	m.field = f
	if f == fieldTarget {
		m.target.Focus()
		m.mission.Blur()
	} else {
		m.mission.Focus()
		m.target.Blur()
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.isDark = msg.IsDark()
		m.styles = newStyles(m.isDark)
		(&m).restyle()
		(&m).renderActivity()
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		(&m).layout()
		(&m).renderActivity()
		(&m).refreshFindings()
		if m.follow {
			m.activity.GotoBottom()
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case eventMsg:
		m.events = append(m.events, msg.e)
		if len(m.events) > m.maxEvents {
			m.events = m.events[len(m.events)-m.maxEvents:]
		}
		m.agents.apply(msg.e)
		(&m).renderActivity()
		if m.follow {
			m.activity.GotoBottom()
		}
		return m, nil

	case approvalReqMsg:
		req := msg
		m.pending = &req
		return m, nil

	case doneMsg:
		m.done = true
		m.summary, m.doneErr = msg.summary, msg.err
		(&m).refreshFindings()
		return m, m.watch.Stop()

	case tickMsg:
		(&m).refreshFindings()
		if m.done {
			return m, nil
		}
		return m, tickCmd(m.tickEvery)

	case spinner.TickMsg:
		if m.done {
			return m, nil
		}
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd

	case stopwatch.TickMsg, stopwatch.StartStopMsg, stopwatch.ResetMsg:
		var cmd tea.Cmd
		m.watch, cmd = m.watch.Update(msg)
		return m, cmd
	}

	// Blink and other textinput messages while on the landing screen.
	if m.phase == phaseLanding {
		var cmd tea.Cmd
		if m.field == fieldTarget {
			m.target, cmd = m.target.Update(msg)
		} else {
			m.mission, cmd = m.mission.Update(msg)
		}
		return m, cmd
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// ctrl+c always quits; deny an open gate first so the engagement unblocks.
	if msg.String() == "ctrl+c" {
		if m.pending != nil {
			m.pending.reply <- false
			m.pending = nil
		}
		return m, tea.Quit
	}

	if m.phase == phaseLanding {
		return m.handleLandingKey(msg)
	}

	// While the approval modal is open, keys answer it and nothing else.
	if m.pending != nil {
		switch {
		case key.Matches(msg, m.keys.Proceed):
			m.pending.reply <- true
			m.pending = nil
		case key.Matches(msg, m.keys.Hold):
			m.pending.reply <- false
			m.pending = nil
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		(&m).layout()
		(&m).renderActivity()
		return m, nil
	case key.Matches(msg, m.keys.Tab):
		if m.focus == paneActivity {
			m.focus = paneFindings
			m.findings.Focus()
		} else {
			m.focus = paneActivity
			m.findings.Blur()
		}
		return m, nil
	}

	// Scrolling goes to the focused pane.
	var cmd tea.Cmd
	if m.focus == paneFindings {
		m.findings, cmd = m.findings.Update(msg)
	} else {
		m.activity, cmd = m.activity.Update(msg)
		m.follow = m.activity.AtBottom()
	}
	return m, cmd
}

// handleLandingKey drives the title/input screen: tab switches fields, enter
// submits and transitions to the dashboard, everything else types.
func (m model) handleLandingKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit) && m.field == fieldTarget && m.target.Value() == "" && m.mission.Value() == "":
		// A bare q on an empty form quits (so the landing isn't a trap); once the
		// user is typing, q is just a character.
		return m, tea.Quit
	case msg.String() == "tab" || msg.String() == "down":
		if m.field == fieldTarget {
			(&m).focusField(fieldMission)
		} else {
			(&m).focusField(fieldTarget)
		}
		return m, textinput.Blink
	case msg.String() == "shift+tab" || msg.String() == "up":
		if m.field == fieldMission {
			(&m).focusField(fieldTarget)
		} else {
			(&m).focusField(fieldMission)
		}
		return m, textinput.Blink
	case msg.String() == "enter":
		return m.submitLanding()
	}

	var cmd tea.Cmd
	if m.field == fieldTarget {
		m.target, cmd = m.target.Update(msg)
	} else {
		m.mission, cmd = m.mission.Update(msg)
	}
	return m, cmd
}

// submitLanding validates the form and, if it's good, transitions to the
// dashboard and signals the engagement to start. On the first empty field it
// focuses it and shows an inline error instead.
func (m model) submitLanding() (model, tea.Cmd) {
	target := strings.TrimSpace(m.target.Value())
	mission := strings.TrimSpace(m.mission.Value())
	if target == "" {
		m.inputError = "a target is required"
		(&m).focusField(fieldTarget)
		return m, nil
	}
	if mission == "" {
		m.inputError = "a mission is required"
		(&m).focusField(fieldMission)
		return m, nil
	}
	if m.cfg.Validate != nil {
		if err := m.cfg.Validate(target); err != nil {
			m.inputError = err.Error()
			(&m).focusField(fieldTarget)
			return m, nil
		}
	}

	m.inputError = ""
	m.scope = target
	m.phase = phaseDashboard
	(&m).layout()
	(&m).renderActivity()
	(&m).refreshFindings()

	cmds := []tea.Cmd{m.sp.Tick, m.watch.Start(), tickCmd(m.tickEvery)}
	if m.startCh != nil {
		ch := m.startCh
		p := startParams{target: target, mission: mission}
		cmds = append(cmds, func() tea.Msg { ch <- p; return nil })
	}
	return m, tea.Batch(cmds...)
}

// --- layout ------------------------------------------------------------------

func (m *model) layout() {
	if !m.ready {
		return
	}
	m.help.SetWidth(m.width)

	// Landing inputs span most of the frame box.
	iw := m.width/2 - 8
	if iw < 24 {
		iw = 24
	}
	if iw > 80 {
		iw = 80
	}
	m.target.SetWidth(iw)
	m.mission.SetWidth(iw)

	headerH := lipgloss.Height(m.headerView())
	footerH := lipgloss.Height(m.footerView())
	bodyH := m.height - headerH - footerH
	if bodyH < 8 {
		bodyH = 8
	}

	findingsH := bodyH / 3
	if findingsH < 6 {
		findingsH = 6
	}
	if findingsH > 14 {
		findingsH = 14
	}
	topH := bodyH - findingsH
	if topH < 5 {
		topH = 5
	}

	rosterW := m.width / 4
	if rosterW < 24 {
		rosterW = 24
	}
	if rosterW > 36 {
		rosterW = 36
	}
	if rosterW > m.width-24 {
		rosterW = m.width - 24
	}
	if rosterW < 1 {
		rosterW = 1
	}
	activityW := m.width - rosterW

	m.rosterW, m.activityW, m.topH, m.findingsH = rosterW, activityW, topH, findingsH

	// Pane frame costs 4 cols (border 2 + padding 2) and 2 rows (border), and the
	// title takes 1 line — so inner content is (outer-4) × (outer-2) minus title.
	m.activity.SetWidth(activityW - 4)
	m.activity.SetHeight(topH - 3)

	m.findings.SetWidth(m.width - 4)
	m.findings.SetHeight(findingsH - 4) // table draws a header row above its rows
	m.setFindingsColumns(m.width - 4)
}

func (m *model) setFindingsColumns(w int) {
	timeW, agentW, phaseW := 8, 11, 10
	// Each of the 4 columns carries 2 cols of cell padding (DefaultStyles).
	sumW := w - timeW - agentW - phaseW - 8
	if sumW < 10 {
		sumW = 10
	}
	m.findingsSummaryW = sumW
	m.findings.SetColumns([]table.Column{
		{Title: "time", Width: timeW},
		{Title: "agent", Width: agentW},
		{Title: "phase", Width: phaseW},
		{Title: "summary", Width: sumW},
	})
}

func (m *model) refreshFindings() {
	items := m.cfg.Findings.List()
	m.findingsCount = len(items)
	rows := make([]table.Row, 0, len(items))
	for _, f := range items {
		t := f.Time
		if len(t) >= 19 {
			t = t[11:19]
		}
		phase := f.Phase
		if phase == "" {
			phase = "-"
		}
		rows = append(rows, table.Row{t, f.Agent, phase, clip(oneLine(f.Summary), m.findingsSummaryW)})
	}
	m.findings.SetRows(rows)
	if m.focus != paneFindings {
		m.findings.GotoBottom()
	}
}

func (m *model) renderActivity() {
	if !m.ready {
		return
	}
	width := m.activity.Width()
	lines := make([]string, 0, len(m.events))
	for _, e := range m.events {
		lines = append(lines, m.formatEvent(e, width))
	}
	m.activity.SetContent(strings.Join(lines, "\n"))
}

// --- view --------------------------------------------------------------------

func (m model) View() tea.View {
	if !m.ready {
		// Set the green ground from the very first frame, before any content.
		v := tea.NewView("")
		v.BackgroundColor = m.styles.bg
		return v
	}

	var content string
	switch {
	case m.width < 60 || m.height < 16:
		box := m.styles.errText.Render("terminal too small — need at least 60×16")
		content = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	case m.pending != nil:
		content = m.modalView()
	case m.phase == phaseLanding:
		content = m.landingView()
	default:
		content = m.dashboardView()
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "onigami"
	// The green ground is the TERMINAL's own background — one back layer, set
	// once. All content is drawn foreground-only on top, so nothing overwrites the
	// tint with a competing fill and no cell falls back to black.
	v.BackgroundColor = m.styles.bg
	if m.phase == phaseLanding {
		v.Cursor = m.activeInput().Cursor()
	}
	return v
}

func (m model) activeInput() textinput.Model {
	if m.field == fieldMission {
		return m.mission
	}
	return m.target
}

// landingView is the first screen: the onigami wordmark and a framed box that
// takes the target link and the mission, centered on the green ground.
func (m model) landingView() string {
	logo := m.styles.logo.Render(asciiLogo)
	tagline := m.styles.tagline.Render("agentic red-team crew · authorized targets only")

	label := func(active bool, s string) string {
		mark := m.styles.hint.Render("  ")
		if active {
			mark = m.styles.accent.Render("❯ ")
		}
		return mark + m.styles.fieldLabel.Render(s)
	}
	body := lipgloss.JoinVertical(lipgloss.Left,
		label(m.field == fieldTarget, "Target"),
		m.target.View(),
		"",
		label(m.field == fieldMission, "Mission"),
		m.mission.View(),
	)
	box := m.styles.inputFrame.Render(body)

	foot := m.styles.hint.Render("tab switch field · enter start · ctrl+c quit")
	if m.inputError != "" {
		foot = m.styles.errText.Render("⚠ " + m.inputError)
	}

	stacked := lipgloss.JoinVertical(lipgloss.Center, logo, tagline, "", box, "", foot)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, stacked)
}

func (m model) dashboardView() string {
	rosterBody := m.agents.view(m.styles, m.sp.View(), m.rosterW-4, m.topH-3)
	rosterPane := m.renderPane("AGENTS", rosterBody, m.rosterW, m.topH, false)
	activityPane := m.renderPane("ACTIVITY", m.activity.View(), m.activityW, m.topH, m.focus == paneActivity)
	top := lipgloss.JoinHorizontal(lipgloss.Top, rosterPane, activityPane)

	findPane := m.renderPane(fmt.Sprintf("FINDINGS (%d)", m.findingsCount), m.findings.View(), m.width, m.findingsH, m.focus == paneFindings)

	return lipgloss.JoinVertical(lipgloss.Left, m.headerView(), top, findPane, m.footerView())
}

func (m model) renderPane(title, body string, outerW, outerH int, focused bool) string {
	st := m.styles.pane
	if focused {
		st = m.styles.paneFocused
	}
	// In lipgloss v2, Width and Height are the TOTAL size (border + padding
	// included). The usable text area is outer minus the frame; we hard-clip the
	// body to it so no child line can wrap or overflow and break the alt-screen
	// layout.
	hFrame := st.GetHorizontalFrameSize()
	vFrame := st.GetVerticalFrameSize()
	textW := outerW - hFrame
	body = clampBlock(body, textW, (outerH-vFrame)-1) // content lines minus title
	inner := clipLine(m.styles.paneTitle.Render(title), textW) + "\n" + body
	return st.Width(outerW).Height(outerH).Render(inner)
}

// clampBlock forces a block to exactly h lines, each at most w display columns
// (ANSI-aware), padding short blocks and truncating long ones.
func clampBlock(s string, w, h int) string {
	if h < 1 {
		h = 1
	}
	lines := strings.Split(s, "\n")
	out := make([]string, h)
	for i := 0; i < h; i++ {
		if i < len(lines) {
			out[i] = clipLine(lines[i], w)
		}
	}
	return strings.Join(out, "\n")
}

func (m model) headerView() string {
	left := m.styles.kanji.Render("鬼神") + " " + m.styles.brand.Render("onigami") +
		m.styles.meta.Render("  scope ") + m.styles.scope.Render(m.scope)
	right := m.statusText() +
		m.styles.meta.Render("  ⏱ ") + m.styles.body.Render(m.elapsed()) +
		m.styles.meta.Render("  findings ") + m.styles.accent.Render(fmt.Sprint(m.findingsCount))

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	// Clip to a single line so a narrow terminal truncates rather than wraps. The
	// gap is plain spaces: with no background, they show the green ground.
	return clipLine(left+strings.Repeat(" ", gap)+right, m.width)
}

func (m model) statusText() string {
	if m.done {
		if m.doneErr != nil {
			return m.styles.errText.Render("鬼 ended early")
		}
		return m.styles.success.Render("神 finished")
	}
	return m.styles.gold.Render(m.sp.View()) + m.styles.meta.Render(" running")
}

func (m model) elapsed() string {
	d := m.watch.Elapsed().Truncate(time.Second)
	return fmt.Sprintf("%02d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

func (m model) footerView() string {
	focus := m.styles.meta.Render("pane ") + m.styles.accent.Render(m.focusName())
	helpView := m.help.View(m.keys)
	if m.help.ShowAll {
		return lipgloss.JoinVertical(lipgloss.Left, clipLine(focus, m.width), helpView)
	}
	return clipLine(focus+m.styles.meta.Render("    ")+helpView, m.width)
}

func (m model) focusName() string {
	if m.focus == paneFindings {
		return "findings"
	}
	return "activity"
}

func (m model) modalView() string {
	role := string(m.pending.role)
	lines := []string{
		m.styles.brand.Render("⚠ approval required"),
		"",
		m.styles.body.Render("The ") + m.styles.role(role).Render(role) + m.styles.body.Render(" is awaiting your decision."),
		m.styles.meta.Render("Review its output in the activity log, then choose."),
		"",
		m.styles.accent.Render("p") + m.styles.meta.Render(" proceed      ") +
			m.styles.accent.Render("h") + m.styles.meta.Render(" hold"),
	}
	box := m.styles.modal.Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// formatEvent renders one event as a colored line. Everything is foreground-only
// so the green ground shows through; no separator paints its own background.
func (m model) formatEvent(e agent.Event, width int) string {
	indent := strings.Repeat("  ", e.Depth)
	label := m.styles.role(string(e.Agent)).Render(string(e.Agent))

	var icon, text string
	switch e.Kind {
	case agent.EventStart:
		icon, text = m.styles.accent.Render("▶"), m.styles.body.Render("started: "+oneLine(e.Text))
	case agent.EventThinking:
		icon, text = m.styles.dim.Render("·"), m.styles.dim.Render("thinking: "+clip(oneLine(e.Text), 400))
	case agent.EventToolCall:
		icon, text = m.styles.gold.Render("→"), m.styles.gold.Render(oneLine(e.Text))
	case agent.EventToolResult:
		icon, text = m.styles.meta.Render("←"), m.styles.body.Render(clip(oneLine(e.Text), 400))
	case agent.EventMessage:
		icon, text = m.styles.meta.Render("✎"), m.styles.body.Render(clip(oneLine(e.Text), 600))
	case agent.EventReport:
		icon, text = m.styles.success.Render("✿"), m.styles.success.Render("reported")
	case agent.EventError:
		icon, text = m.styles.errText.Render("鬼"), m.styles.errText.Render(oneLine(e.Text))
	default:
		icon, text = " ", m.styles.body.Render(oneLine(e.Text))
	}

	line := indent + icon + " " + label + "  " + text
	if width > 0 {
		return clipLine(line, width)
	}
	return line
}

// --- small helpers -----------------------------------------------------------

// asciiLogo is the onigami wordmark on the landing screen.
const asciiLogo = `   ____  _   _ _____ _____    _    __  __ ___
  / __ \| \ | |_   _/ ____|  / \  |  \/  |_ _|
 | |  | |  \| | | || |  _   / _ \ | |\/| || |
 | |__| | |\  | | || |_| | / ___ \| |  | || |
  \____/|_| \_| |_| \____|/_/   \_\_|  |_|___|`

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// clipLine truncates a (possibly ANSI-styled) line to a display width.
func clipLine(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}
