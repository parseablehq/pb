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
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "move down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("right", "l", "pgdown"),
		key.WithHelp("→/l", "prev page"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("left", "h", "pgup"),
		key.WithHelp("←/h", "next page"),
	),
	ScrollLeft: key.NewBinding(
		key.WithKeys("shift+left", "shift+h"),
		key.WithHelp("shift ←/h", "scroll left"),
	),
	ScrollRight: key.NewBinding(
		key.WithKeys("shift+right", "shift+l"),
		key.WithHelp("shift →/l", "scroll right"),
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
