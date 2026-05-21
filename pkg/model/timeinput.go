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
	"pb/pkg/ui"
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

// View renders the time-range modal — matches mock ViewTime layout:
//
//	┌─ TIME RANGE ──────────┐  START
//	│ ▸ 1 Hour              │  ┌──────────────────────┐
//	│   10 Minutes          │  │ 2026-05-18 11:05:10 │
//	│   5 Hours             │  └──────────────────────┘
//	│   …                   │  END                [now]
//	└───────────────────────┘  ┌──────────────────────┐
//	                            │ 2026-05-18 12:05:10 │
//	                            └──────────────────────┘
//	                            span: 1h · auto step: 1m · ~60 samples
//	                            tab/shift-tab fields · ctrl+{ snap end → now
func (m TimeInputModel) View() string {
	p := ui.Active

	leftFocused := m.currentFocus() == "list"
	startFocus := m.currentFocus() == "start"
	endFocus := m.currentFocus() == "end"

	// Section-title style: lowercase form labels (PRESETS / START /
	// END), Accent bold when that section has focus, Faint otherwise.
	secTitle := func(label string, focused bool) string {
		fg := p.Faint
		if focused {
			fg = p.Accent
		}
		return lipgloss.NewStyle().Foreground(fg).Bold(true).Render(label)
	}

	// ── Left column: PRESETS list ──
	leftColW := 24
	leftHeader := secTitle("presets", leftFocused)
	leftBody := m.list.View()
	leftCol := lipgloss.JoinVertical(lipgloss.Left, leftHeader, "", leftBody)
	leftCol = lipgloss.NewStyle().Width(leftColW).Render(leftCol)

	// ── Right column: START + END fields ──
	startField := renderTimeField("start", m.start.View(), startFocus, false)
	endNow := m.end.Time().Sub(time.Now()).Abs() < 2*time.Second
	endField := renderTimeField("end", m.end.View(), endFocus, endNow)

	var rightCol string
	if m.instant {
		rightCol = endField
	} else {
		rightCol = lipgloss.JoinVertical(lipgloss.Left, startField, "", endField)
	}

	// Gutter between columns.
	gutter := lipgloss.NewStyle().Width(3).Render("")
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, gutter, rightCol)

	// Modal frame — single NormalBorder, "TIME RANGE" in border-style
	// label rendered at top-left of the frame (no centered banner, no
	// letter-spacing).
	header := lipgloss.NewStyle().
		Foreground(p.Accent).
		Bold(true).
		Render("TIME RANGE")
	modalInner := lipgloss.JoinVertical(lipgloss.Left, header, "", body)
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(p.Border).
		Padding(1, 2).
		Render(modalInner)
}

// renderTimeField — flat NormalBorder field. Label is lowercase
// (Faint when idle, Accent bold when focused), value Body, no bg
// fills. now-badge sits to the right of the label.
func renderTimeField(label, val string, focused, nowBadge bool) string {
	p := ui.Active
	borderColor := p.Border
	labelFg := p.Faint
	if focused {
		borderColor = p.BorderHi
		labelFg = p.Accent
	}

	hdr := lipgloss.NewStyle().
		Foreground(labelFg).
		Bold(true).
		Render(label)
	if nowBadge {
		badge := lipgloss.NewStyle().
			Foreground(p.Ok).
			Bold(true).
			Render("· now")
		hdr = lipgloss.JoinHorizontal(lipgloss.Top, hdr, "  ", badge)
	}

	valueRow := lipgloss.NewStyle().
		Foreground(p.Body).
		Bold(true).
		Render(val)
	box := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(34).
		Render(valueRow)
	return lipgloss.JoinVertical(lipgloss.Left, hdr, box)
}

func humanizeDur(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func autoStepFor(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d <= 15*time.Minute:
		return "10s"
	case d <= time.Hour:
		return "1m"
	case d <= 6*time.Hour:
		return "30s"
	case d <= 24*time.Hour:
		return "5m"
	default:
		return "30m"
	}
}

func samplesFor(d time.Duration, step string) int {
	if d < 0 {
		d = -d
	}
	dStep, err := time.ParseDuration(step)
	if err != nil || dStep == 0 {
		return 0
	}
	return int(d / dStep)
}
