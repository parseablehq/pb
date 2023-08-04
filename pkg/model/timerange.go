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
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	TenMinute    = -10 * time.Minute
	TwentyMinute = -20 * time.Minute
	ThirtyMinute = -30 * time.Minute
	OneHour      = -1 * time.Hour
	ThreeHour    = -3 * time.Hour
	OneDay       = -24 * time.Hour
	ThreeDay     = -72 * time.Hour
	OneWeek      = -168 * time.Hour
)

var (
	timeDurations = []list.Item{
		timeDurationItem{duration: TenMinute, repr: "10 Minutes"},
		timeDurationItem{duration: TwentyMinute, repr: "20 Minutes"},
		timeDurationItem{duration: ThirtyMinute, repr: "30 Minutes"},
		timeDurationItem{duration: OneHour, repr: "1 Hour"},
		timeDurationItem{duration: ThreeHour, repr: "3 Hours"},
		timeDurationItem{duration: OneDay, repr: "1 Day"},
		timeDurationItem{duration: ThreeDay, repr: "3 Days"},
		timeDurationItem{duration: OneWeek, repr: "1 Week"},
	}

	listItemRender         = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#3f9999", Dark: "#edf2fb"})
	listSelectedItemRender = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#00171f", Dark: "#FFFFFF"})
)

var rangeNavigationMap = []string{
	"list", "start", "end",
}

type timeDurationItem struct {
	duration time.Duration
	repr     string
}

func (i timeDurationItem) FilterValue() string { return i.repr }

type timeDurationItemDelegate struct{}

func (d timeDurationItemDelegate) Height() int                             { return 1 }
func (d timeDurationItemDelegate) Spacing() int                            { return 0 }
func (d timeDurationItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d timeDurationItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(timeDurationItem)
	if !ok {
		return
	}

	fn := listItemRender.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return listSelectedItemRender.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(i.repr))
}

type timeRangeModel struct {
	start       time.Time
	end         time.Time
	list_model  list.Model
	start_model textinput.Model
	end_model   textinput.Model
	focus       int
}

func (m *timeRangeModel) StartValue() string {
	return m.start.Format(time.RFC3339)
}

func (m *timeRangeModel) EndValue() string {
	return m.end.Format(time.RFC3339)
}

func (m *timeRangeModel) StartValueUtc() string {
	return m.start.UTC().Format(time.RFC3339)
}

func (m *timeRangeModel) EndValueUtc() string {
	return m.end.UTC().Format(time.RFC3339)
}

func (m *timeRangeModel) SetStart(t time.Time) {
	m.start = t
	m.start_model.SetValue(m.StartValue())
}

func (m *timeRangeModel) SetEnd(t time.Time) {
	m.end = t
	m.end_model.SetValue(m.EndValue())
}

func (m *timeRangeModel) focusSelected() {
	switch m.currentFocus() {
	case "list":
		return
	case "start":
		m.start_model.Focus()
	case "end":
		m.end_model.Focus()
	}
}

func (m *timeRangeModel) blurSelected() {
	switch m.currentFocus() {
	case "list":
		return
	case "start":
		m.start_model.Blur()
	case "end":
		m.end_model.Blur()
	}
}

func (m *timeRangeModel) Navigate(key tea.KeyMsg) {
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

func (m *timeRangeModel) currentFocus() string {
	return rangeNavigationMap[m.focus]
}

func NewTimeRangeModel(duration uint) timeRangeModel {
	end_time := time.Now()
	start_time := end_time.Add(TenMinute)

	if duration != 0 {
		start_time = end_time.Add(-(time.Duration(duration) * time.Minute))
	}

	list := list.New(timeDurations, timeDurationItemDelegate{}, 20, 10)
	list.SetShowPagination(false)
	list.SetShowHelp(false)
	list.SetShowFilter(false)
	list.SetShowTitle(true)
	list.Styles.TitleBar = baseStyle.Copy()
	list.Styles.Title = baseStyle.Copy().MarginBottom(1)
	list.Styles.TitleBar.Align(lipgloss.Left)
	list.Title = "Select Time Range"
	list.SetShowStatusBar(false)

	input_style := lipgloss.NewStyle().Bold(true).Faint(true).Width(6).Align(lipgloss.Center)

	start := textinput.New()
	start.Width = datetime_width
	start.Prompt = input_style.Render("start")
	start.SetValue(start_time.Format(time.RFC3339))

	end := textinput.New()
	end.Width = datetime_width
	end.Prompt = input_style.Render("end")
	end.SetValue(end_time.Format(time.RFC3339))

	return timeRangeModel{
		start:       start_time,
		end:         end_time,
		list_model:  list,
		start_model: start,
		end_model:   end,
		focus:       0,
	}
}

func (m timeRangeModel) Init() tea.Cmd {
	return nil
}

func (m timeRangeModel) Update(msg tea.Msg) (timeRangeModel, tea.Cmd) {
	var cmd tea.Cmd

	switch key := msg.(type) {
	case tea.KeyMsg:
		if key.Type == tea.KeyShiftTab || key.Type == tea.KeyTab {
			m.blurSelected()
			m.Navigate(key)
			m.focusSelected()
		} else {
			switch m.currentFocus() {
			case "list":
				m.list_model, cmd = m.list_model.Update(key)
				duration := m.list_model.SelectedItem().(timeDurationItem).duration
				m.SetEnd(time.Now())
				m.SetStart(m.end.Add(duration))
			case "start":
				m.start_model, cmd = m.start_model.Update(key)
			case "end":
				m.end_model, cmd = m.end_model.Update(key)
			}
		}
	}

	return m, cmd
}

func (m timeRangeModel) View() string {
	var input_style = lipgloss.NewStyle().
		Inherit(baseStyle).
		Margin(0)

	var list_style = input_style.Copy().
		Border(lipgloss.RoundedBorder(), true).
		Padding(2)

	var start_style = input_style.Copy().Border(lipgloss.NormalBorder(), false, false, true, false)
	var end_style = start_style.Copy()

	focused := m.currentFocus()

	switch focused {
	case "list":
		list_style.BorderStyle(lipgloss.ThickBorder())
	case "start":
		start_style.Border(lipgloss.NormalBorder(), true)
	case "end":
		end_style.Border(lipgloss.NormalBorder(), true)
	}

	right := lipgloss.JoinVertical(lipgloss.Left, start_style.MarginBottom(3).Render(m.start_model.View()), end_style.Render(m.end_model.View()))
	page := lipgloss.JoinHorizontal(lipgloss.Center, list_style.MarginRight(2).Render(m.list_model.View()), right)

	return page
}
