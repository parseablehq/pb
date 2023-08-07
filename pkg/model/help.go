package model

import (
	"github.com/charmbracelet/bubbles/key"
)

type TableKeyMap struct {
	Up          key.Binding
	Down        key.Binding
	PageUp      key.Binding
	PageDown    key.Binding
	ScrollRight key.Binding
	ScrollLeft  key.Binding
	Filter      key.Binding
	ClearFilter key.Binding
}

// ShortHelp returns keybindings to be shown in the mini help view. It's part
// of the key.Map interface.
func (k TableKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.ScrollRight, k.ScrollRight, k.Filter, k.ClearFilter}
}

// FullHelp returns keybindings for the expanded help view. It's part of the
// key.Map interface.
func (k TableKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown}, // first column
		{k.ScrollLeft, k.ScrollRight},
		{k.ClearFilter, k.Filter}, // second column
	}
}

var tableKeys = TableKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "w"),
		key.WithHelp("↑/w", "scroll up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "s"),
		key.WithHelp("↓/s", "scroll down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("shift+up", "shift+w", "pgup"),
		key.WithHelp("shift ↑/w", "prev page"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("shift+down", "shift+s", "pgdown"),
		key.WithHelp("shift ↓/s", "next page"),
	),
	ScrollLeft: key.NewBinding(
		key.WithKeys("left", "a"),
		key.WithHelp("←/a", "scroll left"),
	),
	ScrollRight: key.NewBinding(
		key.WithKeys("right", "d"),
		key.WithHelp("→/d", "scroll right"),
	),
	Filter: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/ .. <enter>", "Filter"),
	),
	ClearFilter: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "remove filter"),
	),
}
