package ui

import "charm.land/bubbles/v2/key"

// keyMap is the dashboard's keybindings. It implements help.KeyMap so the footer
// help is generated from (and stays in sync with) these bindings.
type keyMap struct {
	Tab      key.Binding
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Help     key.Binding
	Quit     key.Binding

	// gate bindings — only meaningful while the approval modal is open
	Proceed key.Binding
	Hold    key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Tab:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch pane")),
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "b"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "f"), key.WithHelp("pgdn", "page down")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Proceed:  key.NewBinding(key.WithKeys("p", "y"), key.WithHelp("p", "proceed")),
		Hold:     key.NewBinding(key.WithKeys("h", "n", "esc"), key.WithHelp("h", "hold")),
	}
}

// ShortHelp is the single-line footer help.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Tab, k.Up, k.Down, k.Help, k.Quit}
}

// FullHelp is the expanded help shown when '?' is pressed.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Tab, k.Up, k.Down},
		{k.PageUp, k.PageDown},
		{k.Help, k.Quit},
	}
}
