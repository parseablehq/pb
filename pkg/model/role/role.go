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

package role

import (
	"fmt"
	"strings"

	"pb/pkg/model/button"
	"pb/pkg/model/selection"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	privileges             = []string{"none", "admin", "editor", "writer", "reader"}
	navigationMapStreamTag = []string{"role", "stream", "tag", "button"}
	navigationMapStream    = []string{"role", "stream", "button"}
	navigationMap          = []string{"role", "button"}
	navigationMapNone      = []string{"role"}

	FocusPrimary  = lipgloss.AdaptiveColor{Light: "16", Dark: "226"}
	FocusSecondry = lipgloss.AdaptiveColor{Light: "18", Dark: "220"}

	StandardPrimary  = lipgloss.AdaptiveColor{Light: "235", Dark: "255"}
	StandardSecondry = lipgloss.AdaptiveColor{Light: "238", Dark: "254"}

	focusedStyle           = lipgloss.NewStyle().Foreground(FocusSecondry)
	blurredStyle           = lipgloss.NewStyle().Foreground(StandardPrimary)
	selectionFocusStyle    = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true).BorderForeground(StandardSecondry)
	selectionFocusStyleAlt = lipgloss.NewStyle().Border(lipgloss.DoubleBorder(), true).BorderForeground(FocusPrimary)
	selectionBlurStyle     = lipgloss.NewStyle().Height(3).AlignVertical(lipgloss.Center).MarginLeft(1).MarginRight(1)
)

type Model struct {
	focusIndex int
	navMap     *[]string
	Selection  selection.Model
	Stream     textinput.Model
	Tag        textinput.Model
	button     button.Model
	Success    bool
}

func (m *Model) Valid() bool {
	switch m.Selection.Value() {
	case "admin", "editor", "none":
		return true
	case "writer", "reader":
		return !(strings.Contains(m.Stream.Value(), " ") || m.Stream.Value() == "")
	}
	return true
}

func (m *Model) FocusSelected() {
	m.Selection.Blur()
	m.Selection.FocusStyle = selectionFocusStyle
	m.Stream.Blur()
	m.Stream.TextStyle = blurredStyle
	m.Stream.PromptStyle = blurredStyle
	m.Tag.Blur()
	m.Tag.TextStyle = blurredStyle
	m.Tag.PromptStyle = blurredStyle
	m.button.Blur()

	switch (*m.navMap)[m.focusIndex] {
	case "role":
		m.Selection.Focus()
		m.Selection.FocusStyle = selectionFocusStyleAlt
	case "stream":
		m.Stream.TextStyle = focusedStyle
		m.Stream.PromptStyle = focusedStyle
		m.Stream.Focus()
	case "tag":
		m.Tag.TextStyle = focusedStyle
		m.Tag.PromptStyle = focusedStyle
		m.Tag.Focus()
	case "button":
		m.button.Focus()
	}
}

func New() Model {
	selection := selection.New(privileges)
	selection.BlurredStyle = selectionBlurStyle

	button := button.New("Submit")
	button.FocusStyle = focusedStyle
	button.BlurredStyle = blurredStyle

	stream := textinput.New()
	stream.Prompt = "stream: "

	tag := textinput.New()
	tag.Prompt = "tag: "

	m := Model{
		focusIndex: 0,
		navMap:     &navigationMapNone,
		Selection:  selection,
		Stream:     stream,
		Tag:        tag,
		button:     button,
		Success:    false,
	}

	m.FocusSelected()
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case button.Pressed:
		m.Success = true
		return m, tea.Quit
	case tea.KeyMsg:
		// special cases for enter key
		if msg.Type == tea.KeyEnter {
			if m.Selection.Value() == "none" {
				m.Success = true
				return m, tea.Quit
			}
			if m.button.Focused() && !m.button.Invalid {
				m.button, cmd = m.button.Update(msg)
				return m, cmd
			}
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyDown, tea.KeyTab, tea.KeyEnter:
			m.focusIndex++
			if m.focusIndex >= len(*m.navMap) {
				m.focusIndex = 0
			}
			m.FocusSelected()
		case tea.KeyUp, tea.KeyShiftTab:
			m.focusIndex--
			if m.focusIndex < 0 {
				m.focusIndex = len(*m.navMap) - 1
			}
			m.FocusSelected()
		default:
			switch (*m.navMap)[m.focusIndex] {
			case "role":
				m.Selection, cmd = m.Selection.Update(msg)
				switch m.Selection.Value() {
				case "admin", "editor":
					m.navMap = &navigationMap
				case "writer":
					m.navMap = &navigationMapStream
				case "reader":
					m.navMap = &navigationMapStreamTag
				default:
					m.navMap = &navigationMapNone
				}
			case "stream":
				m.Stream, cmd = m.Stream.Update(msg)
			case "tag":
				m.Tag, cmd = m.Tag.Update(msg)
			case "button":
				m.button, cmd = m.button.Update(msg)
			}
			m.button.Invalid = !m.Valid()
		}
	}
	return m, cmd
}

func (m Model) View() string {
	var b strings.Builder

	for _, item := range *m.navMap {
		switch item {
		case "role":
			var buffer string
			if m.Selection.Focused() {
				buffer = lipgloss.JoinHorizontal(lipgloss.Center, "◀ ", m.Selection.View(), " ▶")
			} else {
				buffer = m.Selection.View()
			}
			fmt.Fprintln(&b, buffer)
		case "stream":
			fmt.Fprintln(&b, m.Stream.View())
		case "tag":
			fmt.Fprintln(&b, m.Tag.View())
		case "button":
			fmt.Fprintln(&b)
			fmt.Fprintln(&b, m.button.View())
		}
	}

	if m.Selection.Value() == "none" {
		fmt.Fprintln(&b, blurredStyle.Render("Press enter to create user without a role"))
	}

	return b.String()
}
