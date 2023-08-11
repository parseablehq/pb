// Copyright (c) 2023 Cloudnatively Services Pvt Ltd
//
// This file is part of MinIO Object Storage stack
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

type EndTimeKeyBind struct {
	ResetTime key.Binding
	Ok        key.Binding
}

func (k EndTimeKeyBind) ShortHelp() []key.Binding {
	return []key.Binding{k.ResetTime, k.Ok}
}

func (k EndTimeKeyBind) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.ResetTime},
		{k.Ok},
	}
}

var EndHelpBinds = EndTimeKeyBind{
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
	start datetime.Model
	end   datetime.Model
	list  list.Model
	focus int
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
	switch key.String() {
	case "shift+tab":
		if m.focus == 0 {
			m.focus = len(rangeNavigationMap)
		}
		m.focus -= 1
	case "tab":
		if m.focus == len(rangeNavigationMap)-1 {
			m.focus = -1
		}
		m.focus += 1
	default:
		return
	}
}

func (m *TimeInputModel) currentFocus() string {
	return rangeNavigationMap[m.focus]
}

func NewTimeInputModel(duration uint) TimeInputModel {
	endTime := time.Now()
	startTime := endTime.Add(TenMinute)

	if duration != 0 {
		startTime = endTime.Add(-(time.Duration(duration) * time.Minute))
	}

	list := NewTimeRangeModel()
	input_style := lipgloss.NewStyle().Inherit(baseStyle).Bold(true).Width(6).Align(lipgloss.Center)

	start := datetime.New(input_style.Render("start"))
	start.SetTime(startTime)
	start.Focus()
	end := datetime.New(input_style.Render("end"))
	end.SetTime(endTime)

	return TimeInputModel{
		start: start,
		end:   end,
		list:  list,
		focus: 0,
	}
}

func (m TimeInputModel) FullHelp() [][]key.Binding {
	return EndHelpBinds.FullHelp()
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
			m.SetStart(m.end.Time().Add(duration))
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
	right := fmt.Sprintf("%s\n\n%s",
		startStyle.Render(m.start.View()),
		endStyle.Render(m.end.View()),
	)
	center := baseStyle.Render("│\n│\n│\n│")
	center = lipgloss.PlaceHorizontal(5, lipgloss.Center, center)

	page := lipgloss.JoinHorizontal(lipgloss.Center, left, center, right)

	return page
}
