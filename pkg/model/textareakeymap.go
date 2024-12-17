// Copyright (c) 2024 Parseable, Inc
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package model

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
)

type TextAreaHelpKeys struct{}

// ShortHelp returns keybindings to be shown in the mini help view. It's part
// of the key.Map interface.
func (k TextAreaHelpKeys) ShortHelp() []key.Binding {
	t := textAreaKeyMap
	return []key.Binding{t.WordForward, t.WordBackward, t.DeleteWordBackward, t.DeleteWordForward}
}

// FullHelp returns keybindings for the expanded help view. It's part of the
// key.Map interface.
func (k TextAreaHelpKeys) FullHelp() [][]key.Binding {
	t := textAreaKeyMap
	return [][]key.Binding{
		{t.CharacterForward, t.CharacterBackward, t.WordForward, t.WordBackward}, // first column
		{t.DeleteWordForward, t.DeleteWordBackward, t.DeleteCharacterForward, t.DeleteCharacterBackward},
		{t.LineStart, t.LineEnd, t.InputBegin, t.InputEnd}, // second column
	}
}

var textAreaKeyMap = textarea.KeyMap{
	CharacterForward: key.NewBinding(
		key.WithKeys("right", "ctrl+f"),
		key.WithHelp("→", "right"),
	),
	CharacterBackward: key.NewBinding(
		key.WithKeys("left", "ctrl+b"),
		key.WithHelp("←", "right"),
	),
	WordForward: key.NewBinding(
		key.WithKeys("ctrl+right", "alt+f"),
		key.WithHelp("ctrl →", "word forward")),
	WordBackward: key.NewBinding(
		key.WithKeys("ctrl+left", "alt+b"),
		key.WithHelp("ctrl ←", "word backward")),
	LineNext: key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "down")),
	LinePrevious: key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "up")),
	DeleteWordBackward: key.NewBinding(
		key.WithKeys("ctrl+backspace", "ctrl+w"),
		key.WithHelp("ctrl bkspc", "delete word behind")),
	DeleteWordForward: key.NewBinding(
		key.WithKeys("ctrl+delete", "alt+d"),
		key.WithHelp("ctrl del", "delete word forward")),
	DeleteAfterCursor: key.NewBinding(
		key.WithKeys("ctrl+k"),
	),
	DeleteBeforeCursor: key.NewBinding(
		key.WithKeys("ctrl+u"),
	),
	InsertNewline: key.NewBinding(
		key.WithKeys("enter", "ctrl+m"),
	),
	DeleteCharacterBackward: key.NewBinding(
		key.WithKeys("backspace", "ctrl+h"),
		key.WithHelp("bkspc", "delete backward"),
	),
	DeleteCharacterForward: key.NewBinding(
		key.WithKeys("delete", "ctrl+d"),
		key.WithHelp("del", "delete"),
	),
	LineStart: key.NewBinding(
		key.WithKeys("home", "ctrl+a"),
		key.WithHelp("home", "line start")),
	LineEnd: key.NewBinding(
		key.WithKeys("end", "ctrl+e"),
		key.WithHelp("end", "line end")),
	Paste: key.NewBinding(
		key.WithKeys("ctrl+v"),
		key.WithHelp("ctrl v", "paste")),
	InputBegin: key.NewBinding(
		key.WithKeys("ctrl+home"),
		key.WithHelp("ctrl home", "home")),
	InputEnd: key.NewBinding(
		key.WithKeys("ctrl+end"),
		key.WithHelp("ctrl end", "end")),

	CapitalizeWordForward: key.NewBinding(key.WithKeys("alt+c")),
	LowercaseWordForward:  key.NewBinding(key.WithKeys("alt+l")),
	UppercaseWordForward:  key.NewBinding(key.WithKeys("alt+u")),

	TransposeCharacterBackward: key.NewBinding(key.WithKeys("ctrl+t")),
}
