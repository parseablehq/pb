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

package button

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Pressed is a flag that is enabled when the button is pressed.
type Pressed bool

// Model is the model for a button.
type Model struct {
	text         string
	FocusStyle   lipgloss.Style
	BlurredStyle lipgloss.Style
	focus        bool
	Invalid      bool
}

// New returns a new button model.
func New(text string) Model {
	return Model{
		text:         text,
		FocusStyle:   lipgloss.NewStyle(),
		BlurredStyle: lipgloss.NewStyle(),
	}
}

// Focus sets the focus flag to true.
func (m *Model) Focus() tea.Cmd {
	m.focus = true
	return nil
}

// Blur sets the focus flag to false.
func (m *Model) Blur() {
	m.focus = false
}

// Focused returns true if the button is focused.
func (m *Model) Focused() bool {
	return m.focus
}

// Init initializes the button.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update updates the button.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.focus {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			if m.Invalid {
				return m, nil
			}
			return m, func() tea.Msg { return Pressed(true) }
		default:
			return m, nil
		}
	}

	return m, nil
}

// View renders the button.
func (m Model) View() string {
	var b strings.Builder
	var text string
	if m.Invalid {
		text = "X"
	} else {
		text = m.text
	}

	b.WriteString("[ ")
	if m.focus {
		text = m.FocusStyle.Render(text)
	} else {
		text = m.BlurredStyle.Render(text)
	}
	b.WriteString(text)
	b.WriteString(" ]")

	return b.String()
}
