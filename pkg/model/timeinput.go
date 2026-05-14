// Copyright (c) 2024 Parseable, Inc
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

package model

import (
	"fmt"
	"pb/pkg/model/datetime"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var rangeNavigationMap = []string{
	"list", "start", "end",
}

type endTimeKeyBind struct {
	ResetTime key.Binding
	Ok        key.Binding
}

func (k endTimeKeyBind) ShortHelp() []key.Binding {
	return []key.Binding{k.ResetTime, k.Ok}
}

func (k endTimeKeyBind) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.ResetTime},
		{k.Ok},
	}
}

var endHelpBinds = endTimeKeyBind{
	ResetTime: key.NewBinding(
		key.WithKeys("ctrl+{"),
		key.WithHelp("ctrl+{", "change end time to current time"),
	),
	Ok: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "save and go back"),
	),
}

type TimeInputModel struct {
	start   datetime.Model
	end     datetime.Model
	list    list.Model
	focus   int
	instant bool // when true: hides start, presets move end backwards from now
}

// SetInstant switches between range (start+end) and instant (end only) mode.
func (m *TimeInputModel) SetInstant(v bool) {
	m.instant = v
	// stay on list so arrow keys immediately work on presets
	m.focus = 0
	m.focusSelected()
	if v {
		// pre-select "1 Hour" in the list to match the default end=now-1h
		m.list.Select(1)
	}
}

func (m *TimeInputModel) StartValueUtc() string {
	return m.start.ValueUtc()
}

func (m *TimeInputModel) EndValueUtc() string {
	return m.end.ValueUtc()
}

func (m *TimeInputModel) SetStart(t time.Time) {
	m.start.SetTime(t)
}

func (m *TimeInputModel) SetEnd(t time.Time) {
	m.end.SetTime(t)
}

// FocusEnd jumps directly to the end-time field — used by instant mode.
func (m *TimeInputModel) FocusEnd() {
	m.focus = 2 // index of "end" in rangeNavigationMap
	m.focusSelected()
}

func (m *TimeInputModel) focusSelected() {
	m.start.Blur()
	m.end.Blur()

	switch m.currentFocus() {
	case "start":
		m.start.Focus()
	case "end":
		m.end.Focus()
	}
}

func (m *TimeInputModel) Navigate(key tea.KeyMsg) {
	n := len(rangeNavigationMap)
	switch key.String() {
	case "shift+tab":
		if m.focus == 0 {
			m.focus = n
		}
		m.focus--
		// instant mode: skip "start"
		if m.instant && m.currentFocus() == "start" {
			if m.focus == 0 {
				m.focus = n
			}
			m.focus--
		}
	case "tab":
		if m.focus == n-1 {
			m.focus = -1
		}
		m.focus++
		// instant mode: skip "start"
		if m.instant && m.currentFocus() == "start" {
			if m.focus == n-1 {
				m.focus = -1
			}
			m.focus++
		}
	default:
		return
	}
}

func (m *TimeInputModel) currentFocus() string {
	return rangeNavigationMap[m.focus]
}

// NewTimeInputModel creates a new model
func NewTimeInputModel(startTime, endTime time.Time) TimeInputModel {
	list := NewTimeRangeModel()
	inputStyle := lipgloss.NewStyle().Inherit(baseStyle).Bold(true).Width(6).Align(lipgloss.Center)

	start := datetime.New(inputStyle.Render("start"))
	start.SetTime(startTime)
	start.Focus()
	end := datetime.New(inputStyle.Render("end"))
	end.SetTime(endTime)

	return TimeInputModel{
		start: start,
		end:   end,
		list:  list,
		focus: 0,
	}
}

func (m TimeInputModel) FullHelp() [][]key.Binding {
	return endHelpBinds.FullHelp()
}

func (m TimeInputModel) Init() tea.Cmd {
	return nil
}

func (m TimeInputModel) Update(msg tea.Msg) (TimeInputModel, tea.Cmd) {
	var cmd tea.Cmd
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch key.Type {
	case tea.KeyShiftTab, tea.KeyTab:
		m.Navigate(key)
		m.focusSelected()

	case tea.KeyCtrlOpenBracket:
		m.end.SetTime(time.Now())
	default:
		switch m.currentFocus() {
		case "list":
			m.list, cmd = m.list.Update(key)
			duration := m.list.SelectedItem().(timeDurationItem).duration
			if m.instant {
				// preset moves end backwards from now
				m.SetEnd(time.Now().Add(duration))
			} else {
				m.SetStart(m.end.Time().Add(duration))
			}
		case "start":
			m.start, cmd = m.start.Update(key)
		case "end":
			m.end, cmd = m.end.Update(key)
		}
	}

	return m, cmd
}

func (m TimeInputModel) View() string {
	listStyle := &borderedStyle
	startStyle := &borderedStyle
	endStyle := &borderedStyle

	switch m.currentFocus() {
	case "list":
		listStyle = &borderedFocusStyle
	case "start":
		startStyle = &borderedFocusStyle
	case "end":
		endStyle = &borderedFocusStyle
	}

	list := lipgloss.NewStyle().PaddingLeft(1).Render(m.list.View())
	left := listStyle.Render(lipgloss.PlaceHorizontal(27, lipgloss.Left, list))
	center := baseStyle.Render("│\n│\n│\n│")
	center = lipgloss.PlaceHorizontal(5, lipgloss.Center, center)

	var right string
	if m.instant {
		// instant mode: only show end time, no start
		label := lipgloss.NewStyle().Inherit(baseStyle).Bold(true).
			Foreground(FocusSecondary).Render(" evaluation time ")
		right = fmt.Sprintf("%s\n%s", label, endStyle.Render(m.end.View()))
	} else {
		right = fmt.Sprintf("%s\n\n%s",
			startStyle.Render(m.start.View()),
			endStyle.Render(m.end.View()),
		)
	}

	return lipgloss.JoinHorizontal(lipgloss.Center, left, center, right)
}
