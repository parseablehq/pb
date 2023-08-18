// Copyright (c) 2023 Cloudnatively Services Pvt Ltd
//
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

package credential

import (
	"pb/pkg/model/button"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Default Style for this widget
var (
	FocusPrimary   = lipgloss.AdaptiveColor{Light: "16", Dark: "226"}
	FocusSecondary = lipgloss.AdaptiveColor{Light: "18", Dark: "220"}

	StandardPrimary   = lipgloss.AdaptiveColor{Light: "235", Dark: "255"}
	StandardSecondary = lipgloss.AdaptiveColor{Light: "238", Dark: "254"}

	focusedStyle = lipgloss.NewStyle().Foreground(FocusPrimary)
	blurredStyle = lipgloss.NewStyle().Foreground(StandardSecondary)
	noStyle      = lipgloss.NewStyle()
)

type Model struct {
	focusIndex int
	inputs     []textinput.Model
	button     button.Model
}

func (m *Model) Values() (string, string) {
	if validInputs(&m.inputs) {
		return m.inputs[0].Value(), m.inputs[1].Value()
	}
	return "", ""
}

func validInputs(inputs *[]textinput.Model) bool {
	valid := true
	username := (*inputs)[0].Value()
	password := (*inputs)[1].Value()

	if strings.Contains(username, " ") || username == "" || password == "" {
		valid = false
	}

	return valid
}

func New() Model {
	m := Model{
		inputs: make([]textinput.Model, 2),
	}

	var t textinput.Model
	for i := range m.inputs {
		t = textinput.New()
		t.Cursor.Style = focusedStyle.Copy()
		t.CharLimit = 32

		switch i {
		case 0:
			t.Placeholder = "username"
			t.Focus()
			t.PromptStyle = focusedStyle
			t.Prompt = "user: "
			t.TextStyle = focusedStyle
		case 1:
			t.Placeholder = "password"
			t.Prompt = "pass: "
			t.EchoMode = textinput.EchoPassword
			t.EchoCharacter = '•'
			t.CharLimit = 64
		}
		m.inputs[i] = t
	}

	button := button.New("Submit")
	button.FocusStyle = focusedStyle
	button.BlurredStyle = blurredStyle
	button.Invalid = true

	m.button = button

	return m
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case button.Pressed:
		if validInputs(&m.inputs) {
			return m, tea.Quit
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit

		case "tab", "shift+tab", "enter", "up", "down":
			s := msg.String()

			if s == "enter" && m.focusIndex == 2 && !m.button.Invalid {
				return m, tea.Quit
			}

			if s == "up" || s == "shift+tab" {
				m.focusIndex--
			} else {
				m.focusIndex++
			}

			if m.focusIndex >= 3 {
				m.focusIndex = 0
			} else if m.focusIndex < 0 {
				m.focusIndex = 2
			}

			cmds := make([]tea.Cmd, len(m.inputs))
			for i := 0; i < 2; i++ {
				if i == m.focusIndex {
					// Set focused state
					cmds[i] = m.inputs[i].Focus()
					m.inputs[i].PromptStyle = focusedStyle
					m.inputs[i].TextStyle = focusedStyle
					continue
				}
				// Remove focused state
				m.inputs[i].Blur()
				m.inputs[i].PromptStyle = noStyle
				m.inputs[i].TextStyle = noStyle
			}

			if m.focusIndex == 2 {
				m.button.Focus()
			} else {
				m.button.Blur()
			}

			return m, tea.Batch(cmds...)
		}
	}

	// Handle character input and blinking
	cmd := m.updateInputs(msg)

	if validInputs(&m.inputs) {
		m.button.Invalid = false
	}

	return m, cmd
}

func (m *Model) updateInputs(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs)+1)
	// Only text inputs with Focus() set will respond, so it's safe to simply
	// update all of them here without any further logic.
	for i := range m.inputs {
		m.inputs[i], cmds[i] = m.inputs[i].Update(msg)
	}
	m.button, cmds[2] = m.button.Update(msg)
	return tea.Batch(cmds...)
}

func (m Model) View() string {
	var b strings.Builder

	for i := range m.inputs {
		b.WriteString(m.inputs[i].View())
		b.WriteRune('\n')
	}
	b.WriteString(m.button.View())
	return b.String()
}
