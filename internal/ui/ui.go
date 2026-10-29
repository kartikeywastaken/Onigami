// Package ui is onigami's optional terminal dashboard (opt-in via --tui).
//
// It is a thin consumer of the engagement machinery, not a rewrite of it: the
// orchestrator and workers already emit everything worth showing through an
// agent.Observer, and they gate risky actions through an agent.Approver. This
// package turns that event stream into a live, scrolling Bubble Tea (v2) view
// and turns the approval gate into a one-line footer prompt.
//
// The engagement runs on its own goroutine; the two callbacks bridge back into
// the Elm loop the correct way — never mutating the model from the goroutine,
// only sending it typed messages via Program.Send.
package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"onigami/internal/agent"
	"onigami/internal/config"
	"onigami/internal/tools"
)

// Config is what main hands the dashboard to run one engagement.
type Config struct {
	Scope    string               // the in-scope target(s), shown in the header
	Findings *tools.FindingsStore // polled for the live findings count
	// Start runs the blocking engagement. The UI supplies the observer (to feed
	// the activity stream) and the approver (the footer proceed/hold gate), and
	// Start returns the final report summary and any error.
	Start func(obs agent.Observer, approver agent.Approver) (string, error)
}

// --- messages that cross from the engagement goroutine into the Elm loop -----

type eventMsg struct{ e agent.Event }

// approvalReqMsg carries a decision channel: the approver blocks on it until the
// user presses a key, which lets the TUI answer a synchronous gate safely.
type approvalReqMsg struct {
	role  agent.Role
	reply chan bool
}

type doneMsg struct {
	summary string
	err     error
}

type tickMsg time.Time

// Run shows the dashboard for one engagement and blocks until the user quits.
// It returns the final report summary and the engagement's error (if any).
func Run(cfg Config) (string, error) {
	m := newModel(cfg)
	prog := tea.NewProgram(m)

	// Observer bridge: every agent event becomes one message. Send is safe to
	// call from another goroutine; the model is only ever mutated in Update.
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

// --- model -------------------------------------------------------------------

type model struct {
	cfg    Config
	styles styles
	sp     spinner.Model
	vp     viewport.Model

	ready  bool
	width  int
	height int
	isDark bool

	start     time.Time
	tickEvery time.Duration
	maxEvents int

	events   []agent.Event
	findings int
	follow   bool // auto-scroll to the newest line unless the user scrolled up

	done    bool
	doneErr error
	summary string

	pending *approvalReqMsg // non-nil while a proceed/hold decision is awaited
}

func newModel(cfg Config) model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	return model{
		cfg:       cfg,
		styles:    newStyles(true), // rebuilt once we learn the real background
		sp:        sp,
		start:     time.Now(),
		follow:    true,
		tickEvery: config.SecondsEnv("ONIGAMI_UI_TICK", time.Second),
		maxEvents: config.IntEnv("ONIGAMI_UI_SCROLLBACK", 500),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.sp.Tick, tickCmd(m.tickEvery))
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.isDark = msg.IsDark()
		m.styles = newStyles(m.isDark)
		m.sp.Style = m.styles.accent
		(&m).renderContent()
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if !m.ready {
			m.vp = viewport.New()
			m.ready = true
		}
		(&m).layout()
		(&m).renderContent()
		if m.follow {
			m.vp.GotoBottom()
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case eventMsg:
		m.events = append(m.events, msg.e)
		if len(m.events) > m.maxEvents {
			m.events = m.events[len(m.events)-m.maxEvents:]
		}
		(&m).renderContent()
		if m.follow {
			m.vp.GotoBottom()
		}
		return m, nil

	case approvalReqMsg:
		req := msg // take a stable address for the pointer below
		m.pending = &req
		return m, nil

	case doneMsg:
		m.done = true
		m.summary = msg.summary
		m.doneErr = msg.err
		m.findings = m.cfg.Findings.Count()
		return m, nil

	case tickMsg:
		m.findings = m.cfg.Findings.Count()
		if m.done {
			return m, nil // stop the heartbeat once the engagement is over
		}
		return m, tickCmd(m.tickEvery)

	case spinner.TickMsg:
		if m.done {
			return m, nil
		}
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// ctrl+c always quits; if a gate is open, deny it so the engagement unblocks.
	if key == "ctrl+c" {
		if m.pending != nil {
			m.pending.reply <- false
			m.pending = nil
		}
		return m, tea.Quit
	}

	// While a gate is open, keys answer it and nothing else.
	if m.pending != nil {
		switch key {
		case "p", "y":
			m.pending.reply <- true
			m.pending = nil
		case "h", "n", "esc":
			m.pending.reply <- false
			m.pending = nil
		}
		return m, nil
	}

	if key == "q" {
		return m, tea.Quit
	}

	// Everything else is scrolling — hand it to the viewport and remember whether
	// we're still pinned to the bottom (so new events keep auto-scrolling).
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	m.follow = m.vp.AtBottom()
	return m, cmd
}

// layout sizes the viewport to whatever is left after the header and footer.
func (m *model) layout() {
	if !m.ready {
		return
	}
	bodyH := m.height - lipgloss.Height(m.headerView()) - lipgloss.Height(m.footerView())
	if bodyH < 1 {
		bodyH = 1
	}
	m.vp.SetWidth(m.width)
	m.vp.SetHeight(bodyH)
}

// renderContent rebuilds the activity stream from the stored events. We re-render
// from the source events (rather than appending pre-rendered text) so a resize or
// a theme change re-wraps and re-colors everything correctly.
func (m *model) renderContent() {
	if !m.ready {
		return
	}
	width := m.vp.Width()
	lines := make([]string, 0, len(m.events))
	for _, e := range m.events {
		lines = append(lines, m.formatEvent(e, width))
	}
	m.vp.SetContent(strings.Join(lines, "\n"))
}

// formatEvent renders one event as a wrapped, colored line (or block).
func (m *model) formatEvent(e agent.Event, width int) string {
	indent := strings.Repeat("  ", e.Depth)
	label := m.styles.role(string(e.Agent)).Render(string(e.Agent))

	var icon, text string
	switch e.Kind {
	case agent.EventStart:
		icon, text = m.styles.accent.Render("▶"), "started: "+oneLine(e.Text)
	case agent.EventThinking:
		icon, text = m.styles.dim.Render("·"), m.styles.dim.Render("thinking: "+clip(oneLine(e.Text), 400))
	case agent.EventToolCall:
		icon, text = m.styles.accent.Render("→"), m.styles.accent.Render(oneLine(e.Text))
	case agent.EventToolResult:
		icon, text = m.styles.meta.Render("←"), clip(oneLine(e.Text), 400)
	case agent.EventMessage:
		icon, text = m.styles.meta.Render("✎"), clip(oneLine(e.Text), 600)
	case agent.EventReport:
		icon, text = m.styles.success.Render("✔"), m.styles.success.Render("reported")
	case agent.EventError:
		icon, text = m.styles.errText.Render("✘"), m.styles.errText.Render(oneLine(e.Text))
	default:
		icon, text = " ", oneLine(e.Text)
	}

	line := fmt.Sprintf("%s%s %s  %s", indent, icon, label, text)
	if width > 0 {
		return lipgloss.NewStyle().Width(width).Render(line)
	}
	return line
}

func (m model) View() tea.View {
	if !m.ready {
		return tea.NewView("")
	}
	if m.width < 40 || m.height < 8 {
		msg := m.styles.warn.Render("terminal too small — need at least 40×8")
		v := tea.NewView(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg))
		v.AltScreen = true
		return v
	}

	content := lipgloss.JoinVertical(lipgloss.Left, m.headerView(), m.vp.View(), m.footerView())
	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "onigami"
	return v
}

func (m model) headerView() string {
	left := m.styles.brand.Render("onigami") +
		m.styles.meta.Render("  scope ") + m.styles.scope.Render(m.cfg.Scope)

	status := m.sp.View() + m.styles.meta.Render(" running")
	if m.done {
		if m.doneErr != nil {
			status = m.styles.errText.Render("● ended early")
		} else {
			status = m.styles.success.Render("● finished")
		}
	}
	right := fmt.Sprintf("%s   %s %s   %s %s",
		status,
		m.styles.meta.Render("elapsed"), m.elapsed(),
		m.styles.meta.Render("findings"), m.styles.accent.Render(fmt.Sprint(m.findings)))

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	bar := lipgloss.NewStyle().Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
	return bar + "\n" + m.styles.rule.Render(strings.Repeat("─", m.width))
}

func (m model) footerView() string {
	divider := m.styles.rule.Render(strings.Repeat("─", m.width))

	if m.pending != nil {
		prompt := m.styles.warn.Render(fmt.Sprintf("⚠ %s awaiting approval — ", m.pending.role)) +
			m.styles.keyHint("p", "proceed") + "   " + m.styles.keyHint("h", "hold")
		return divider + "\n" + prompt
	}

	if m.done {
		var head string
		if m.doneErr != nil {
			head = m.styles.errText.Render("✘ ended early: " + clip(oneLine(m.doneErr.Error()), 80))
		} else {
			head = m.styles.success.Render("✔ engagement finished")
		}
		help := m.styles.keyHint("↑/↓", "scroll") + "   " + m.styles.keyHint("q", "quit")
		return divider + "\n" + head + "   " + help
	}

	help := m.styles.keyHint("↑/↓", "scroll") + "   " +
		m.styles.keyHint("q", "quit") + "   " + m.styles.keyHint("ctrl+c", "abort")
	return divider + "\n" + help
}

// elapsed is the run time, frozen once the engagement is done.
func (m model) elapsed() string {
	d := time.Since(m.start).Truncate(time.Second)
	return fmt.Sprintf("%02d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

// --- small helpers -----------------------------------------------------------

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
