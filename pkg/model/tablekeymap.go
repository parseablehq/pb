package model

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/evertras/bubble-table/table"
)

type TableKeyMap struct {
	RowUp       key.Binding
	RowDown     key.Binding
	PageUp      key.Binding
	PageDown    key.Binding
	PageFirst   key.Binding
	PageLast    key.Binding
	ScrollRight key.Binding
	ScrollLeft  key.Binding
	Filter      key.Binding
	FilterClear key.Binding
	FilterBlur  key.Binding
}

// ShortHelp returns keybindings to be shown in the mini help view. It's part
// of the key.Map interface.
func (k TableKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.ScrollRight, k.ScrollRight, k.Filter, k.FilterClear}
}

// FullHelp returns keybindings for the expanded help view. It's part of the
// key.Map interface.
func (k TableKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.RowUp, k.RowDown, k.PageUp, k.PageDown}, // first column
		{k.ScrollLeft, k.ScrollRight, k.PageFirst, k.PageLast},
		{k.FilterClear, k.Filter, k.FilterBlur}, // second column
	}
}

var tableHelpBinds = TableKeyMap{
	RowUp: key.NewBinding(
		key.WithKeys("up", "w"),
		key.WithHelp("↑/w", "scroll up"),
	),
	RowDown: key.NewBinding(
		key.WithKeys("down", "s"),
		key.WithHelp("↓/s", "scroll down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("shift+up", "W", "pgup"),
		key.WithHelp("shift ↑/w", "prev page"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("shift+down", "S", "pgdown"),
		key.WithHelp("shift ↓/s", "next page"),
	),
	PageFirst: key.NewBinding(
		key.WithKeys("home", "ctrl+y"),
		key.WithHelp("home/ctrl y", "first page"),
	),
	PageLast: key.NewBinding(
		key.WithKeys("end", "ctrl+v"),
		key.WithHelp("end/ctrl v", "last page"),
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
		key.WithHelp("/", "Filter"),
	),
	FilterClear: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "remove filter"),
	),
	FilterBlur: key.NewBinding(
		key.WithKeys("esc", "enter"),
		key.WithHelp("enter/esc", "blur filter"),
	),
}

var tableKeyBinds = table.KeyMap{
	RowUp:       tableHelpBinds.RowUp,
	RowDown:     tableHelpBinds.RowDown,
	PageUp:      tableHelpBinds.PageUp,
	PageDown:    tableHelpBinds.PageDown,
	PageFirst:   tableHelpBinds.PageFirst,
	PageLast:    tableHelpBinds.PageLast,
	ScrollLeft:  tableHelpBinds.ScrollLeft,
	ScrollRight: tableHelpBinds.ScrollRight,
	Filter:      tableHelpBinds.Filter,
	FilterClear: tableHelpBinds.FilterClear,
	FilterBlur:  tableHelpBinds.FilterBlur,
}
