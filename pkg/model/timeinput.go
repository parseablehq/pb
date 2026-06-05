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
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"
	"github.com/parseablehq/pb/pkg/model/datetime"
	"github.com/parseablehq/pb/pkg/ui"
)

var rangeNavigationMap = []string{
	"list", "start", "end", "display",
}

const nowBadgeTolerance = 2 * time.Second

type TimeDisplayMode string

const (
	// TimeDisplayLocal renders result timestamps in the selected local zone.
	TimeDisplayLocal TimeDisplayMode = "local"
	// TimeDisplayUTC renders result timestamps in UTC.
	TimeDisplayUTC TimeDisplayMode = "utc"
)

type TimeInputModel struct {
	start       datetime.Model
	end         datetime.Model
	list        list.Model
	focus       int
	instant     bool
	displayMode TimeDisplayMode
}

// SetInstant switches between range (start+end) and instant (end only) mode.
func (m *TimeInputModel) SetInstant(v bool) {
	m.instant = v

	m.focus = 0
	m.focusSelected()
	if v {
		m.list.Select(1)
	}
}

func (m *TimeInputModel) StartValueUtc() string {
	return m.start.ValueUtc()
}

func (m *TimeInputModel) EndValueUtc() string {
	return m.end.ValueUtc()
}

func (m *TimeInputModel) DisplayMode() TimeDisplayMode {
	if m.displayMode == "" {
		return TimeDisplayLocal
	}
	return m.displayMode
}

func (m *TimeInputModel) SetDisplayMode(mode TimeDisplayMode) {
	switch mode {
	case TimeDisplayUTC:
		m.displayMode = TimeDisplayUTC
	default:
		m.displayMode = TimeDisplayLocal
	}
}

func (m *TimeInputModel) SetStart(t time.Time) {
	m.start.SetTime(t)
}

func (m *TimeInputModel) SetEnd(t time.Time) {
	m.end.SetTime(t)
}

func (m TimeInputModel) endTimeAllowed() bool {
	if m.end.Time().After(time.Now()) {
		return false
	}
	return m.instant || !m.end.Time().Before(m.start.Time())
}

// FocusEnd jumps directly to the end-time field — used by instant mode.
func (m *TimeInputModel) FocusEnd() {
	m.focus = 2 // index of "end" in rangeNavigationMap
	m.focusSelected()
}

func (m *TimeInputModel) SyncPreset() {
	target := m.start.Time().Sub(m.end.Time()).Round(time.Second)
	if m.instant {
		target = time.Until(m.end.Time()).Round(time.Second)
	}
	bestIdx := 0
	bestDiff := durationAbs(target - timeDurations[0].(timeDurationItem).duration)
	for i, item := range timeDurations {
		duration := item.(timeDurationItem).duration
		diff := durationAbs(target - duration)
		if diff < bestDiff {
			bestIdx = i
			bestDiff = diff
		}
	}
	m.list.Select(bestIdx)
}

func durationAbs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func shouldShowNowBadge(t time.Time) bool {
	return durationAbs(time.Since(t)) <= nowBadgeTolerance
}

func (m *TimeInputModel) focusSelected() {
	m.start.Blur()
	m.end.Blur()

	switch m.currentFocus() {
	case "start":
		m.start.Focus()
		m.start.FocusFirstSegment()
	case "end":
		m.end.Focus()
		m.end.FocusFirstSegment()
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
		start:       start,
		end:         end,
		list:        list,
		focus:       0,
		displayMode: TimeDisplayLocal,
	}
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

	default:
		switch m.currentFocus() {
		case "list":
			if key.Type != tea.KeyUp && key.Type != tea.KeyDown {
				return m, nil
			}
			prevDuration := m.list.SelectedItem().(timeDurationItem).duration
			m.list, cmd = m.list.Update(key)
			duration := m.list.SelectedItem().(timeDurationItem).duration
			if m.instant {
				// Presets move evaluation time relative to a stable anchor,
				// so seconds don't jump while navigating the preset list.
				anchor := m.end.Time().Add(-prevDuration)
				m.SetEnd(anchor.Add(duration))
			} else {
				m.SetStart(m.end.Time().Add(duration))
			}
		case "start":
			prev := m.start
			m.start, cmd = m.start.Update(key)
			if m.start.Time().After(m.end.Time()) {
				m.start = prev
			}
		case "end":
			prev := m.end
			m.end, cmd = m.end.Update(key)
			if !m.endTimeAllowed() {
				m.end = prev
			}
		case "display":
			switch key.Type {
			case tea.KeyLeft, tea.KeyRight, tea.KeyUp, tea.KeyDown, tea.KeySpace:
				if m.DisplayMode() == TimeDisplayLocal {
					m.SetDisplayMode(TimeDisplayUTC)
				} else {
					m.SetDisplayMode(TimeDisplayLocal)
				}
			}
		}
	}

	return m, cmd
}

func (m TimeInputModel) View() string {
	const width = 72
	const leftW = 27
	const gapW = 2
	rightW := width - 4 - leftW - gapW
	p := ui.Active
	borderStyle := lipgloss.NewStyle().Foreground(p.BorderHi)
	titleStyle := lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(p.Faint).Bold(true)

	lines := []string{
		paneRule("┌", "┐", "QUERY TIME", width, borderStyle, titleStyle),
	}

	header := timePickerJoin(
		labelStyle.Render("  PRESETS"),
		labelStyle.Render("CUSTOM RANGE"),
		leftW,
		gapW,
		rightW,
	)

	startBox := renderTimePickerInputBox("start", m.start, rightW, m.currentFocus() == "start", false)
	endTitle := "end"
	if m.instant {
		endTitle = "evaluation time"
	}
	endBox := renderTimePickerInputBox(endTitle, m.end, rightW, m.currentFocus() == "end", shouldShowNowBadge(m.end.Time()))
	if m.instant {
		startBox = []string{"", "", ""}
	}

	displayRows := renderTimeDisplayMode(m.DisplayMode(), rightW, m.currentFocus() == "display", m.end.Time())

	rows := []string{header}
	for i := 0; i < 9; i++ {
		left := ""
		if i < len(timeDurations) {
			left = renderTimePickerPreset(i, m.list.Index(), m.currentFocus() == "list")
		}
		right := ""
		switch i {
		case 0, 1, 2:
			right = startBox[i]
		case 3, 4, 5:
			right = endBox[i-3]
		case 6, 7:
			right = displayRows[i-6]
		}
		rows = append(rows, timePickerJoin(left, right, leftW, gapW, rightW))
	}

	lines = append(lines, paneBodyLines(lipgloss.JoinVertical(lipgloss.Left, rows...), width, len(rows), borderStyle)...)
	lines = append(lines, borderStyle.Render("└"+strings.Repeat("─", width-2)+"┘"))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func renderTimePickerPreset(index, selected int, focused bool) string {
	item := timeDurations[index].(timeDurationItem)
	p := ui.Active
	name := lipgloss.NewStyle().Foreground(p.Body).Render(item.repr)
	if index != selected {
		return "  " + name
	}
	if focused {
		rail := lipgloss.NewStyle().Background(p.Active).Render(" ")
		name = lipgloss.NewStyle().Foreground(p.Active).Bold(true).Render(item.repr)
		return rail + " " + name
	}
	name = lipgloss.NewStyle().Foreground(p.Accent).Bold(true).Render(item.repr)
	return "  " + name
}

func renderTimeDisplayMode(mode TimeDisplayMode, width int, focused bool, referenceTime time.Time) []string {
	p := ui.Active
	labelStyle := lipgloss.NewStyle().Foreground(p.Faint).Bold(true)
	if focused {
		labelStyle = labelStyle.Foreground(p.Active)
	}
	active := lipgloss.NewStyle().Foreground(p.InvertText).Background(p.Active).Bold(true)
	inactive := lipgloss.NewStyle().Foreground(p.Body)

	localLabel := " Local " + referenceTime.Format("UTC-07:00") + " "
	local := inactive.Render(localLabel)
	utc := inactive.Render(" UTC ")
	if mode == TimeDisplayUTC {
		utc = active.Render(" UTC ")
	} else {
		local = active.Render(localLabel)
	}

	line := local + "  " + utc
	if lipgloss.Width(line) > width {
		line = truncate.String(line, uint(width))
	}
	return []string{
		labelStyle.Render("DISPLAY RESULTS AS"),
		line,
	}
}

func renderTimePickerInputBox(title string, input datetime.Model, width int, focused, showNow bool) []string {
	p := ui.Active
	borderColor := p.Border
	titleColor := p.Faint
	if focused {
		borderColor = p.Active
		titleColor = p.Active
	}
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)
	titleStyle := lipgloss.NewStyle().Foreground(titleColor).Bold(focused)
	top := paneRule("┌", "┐", title, width, borderStyle, titleStyle)
	innerW := width - 2
	valueW := innerW - 2
	if valueW < 1 {
		valueW = 1
	}
	value := renderSegmentedTime(input.LocalValue(), input.CursorPosition(), focused)
	if showNow {
		now := lipgloss.NewStyle().Foreground(p.Active).Bold(true).Render("[NOW]")
		valuePlainW := valueW - lipgloss.Width(now) - 1
		if valuePlainW < 1 {
			valuePlainW = 1
		}
		value = renderSegmentedTime(input.LocalValue(), input.CursorPosition(), focused)
		if lipgloss.Width(value) > valuePlainW {
			value = truncate.String(value, uint(valuePlainW))
		}
		pad := valueW - lipgloss.Width(value) - lipgloss.Width(now)
		if pad < 1 {
			pad = 1
		}
		value = value + strings.Repeat(" ", pad) + now
	}
	if lipgloss.Width(value) > valueW {
		value = truncate.String(value, uint(valueW))
	}
	pad := valueW - lipgloss.Width(value)
	if pad < 0 {
		pad = 0
	}
	mid := borderStyle.Render("│") + " " + value + strings.Repeat(" ", pad) + " " + borderStyle.Render("│")
	bottom := borderStyle.Render("└" + strings.Repeat("─", width-2) + "┘")
	return []string{top, mid, bottom}
}

func renderSegmentedTime(value string, cursor int, focused bool) string {
	p := ui.Active
	normal := lipgloss.NewStyle().Foreground(p.Body)
	if !focused || value == "" {
		return normal.Render(value)
	}
	runes := []rune(value)
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(runes) {
		cursor = len(runes) - 1
	}
	start, end := timeSegmentBounds(runes, cursor)
	active := lipgloss.NewStyle().Background(p.Active).Foreground(p.InvertText).Bold(true)
	return normal.Render(string(runes[:start])) + active.Render(string(runes[start:end])) + normal.Render(string(runes[end:]))
}

func timeSegmentBounds(runes []rune, cursor int) (int, int) {
	if len(runes) == 0 {
		return 0, 0
	}
	if cursor >= len(runes) {
		cursor = len(runes) - 1
	}
	if !isTimeSegmentChar(runes[cursor]) {
		if cursor+1 < len(runes) && isTimeSegmentChar(runes[cursor+1]) {
			cursor++
		} else if cursor > 0 && isTimeSegmentChar(runes[cursor-1]) {
			cursor--
		}
	}
	start := cursor
	for start > 0 && sameTimeSegmentKind(runes[start-1], runes[cursor]) {
		start--
	}
	end := cursor + 1
	for end < len(runes) && sameTimeSegmentKind(runes[end], runes[cursor]) {
		end++
	}
	return start, end
}

func isTimeSegmentChar(r rune) bool {
	return isTimeDigit(r) || isTimeLetter(r)
}

func sameTimeSegmentKind(a, b rune) bool {
	return (isTimeDigit(a) && isTimeDigit(b)) || (isTimeLetter(a) && isTimeLetter(b))
}

func isTimeDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func isTimeLetter(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

func timePickerJoin(left, right string, leftW, gapW, rightW int) string {
	if lipgloss.Width(left) > leftW {
		left = truncate.String(left, uint(leftW))
	}
	if lipgloss.Width(right) > rightW {
		right = truncate.String(right, uint(rightW))
	}
	leftPad := leftW - lipgloss.Width(left)
	if leftPad < 0 {
		leftPad = 0
	}
	rightPad := rightW - lipgloss.Width(right)
	if rightPad < 0 {
		rightPad = 0
	}
	return left + strings.Repeat(" ", leftPad+gapW) + right + strings.Repeat(" ", rightPad)
}
