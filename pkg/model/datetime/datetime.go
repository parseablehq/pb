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

package datetime

import (
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const localLayout = "2006-Jan-02 15:04:05"

var (
	segmentStarts = []int{0, 5, 9, 12, 15, 18}
	segmentEnds   = []int{4, 8, 11, 14, 17, 20}
)

// Model is the model for the datetime component
type Model struct {
	time  time.Time
	input textinput.Model
}

// Value returns the current value of the datetime component
func (m *Model) Value() string {
	return m.time.Format(time.RFC3339)
}

// ValueUtc returns the current value of the datetime component in UTC
func (m *Model) ValueUtc() string {
	return m.time.UTC().Format(time.RFC3339)
}

// SetTime sets the value of the datetime component
func (m *Model) SetTime(t time.Time) {
	m.time = t
	m.input.SetValue(m.time.Format(localLayout))
}

// Time returns the current time of the datetime component
func (m *Model) Time() time.Time {
	return m.time
}

// LocalValue returns the editable local timestamp string.
func (m *Model) LocalValue() string {
	return m.input.Value()
}

// CursorPosition returns the cursor position in the editable timestamp.
func (m *Model) CursorPosition() int {
	return m.input.Position()
}

// FocusFirstSegment moves editing to the first date-time segment.
func (m *Model) FocusFirstSegment() {
	m.input.SetCursor(segmentStarts[0])
}

// New creates a new datetime component
func New(prompt string) Model {
	input := textinput.New()
	input.Width = 20
	input.Prompt = prompt

	return Model{
		time:  time.Now(),
		input: input,
	}
}

// Focus focuses the datetime component
func (m *Model) Focus() tea.Cmd {
	m.input.Focus()
	return nil
}

// Blur blurs the datetime component
func (m *Model) Blur() {
	m.input.Blur()
}

// Focused returns true if the datetime component is focused
func (m *Model) Focused() bool {
	return m.input.Focused()
}

// Init initializes the datetime component
func (m Model) Init() tea.Cmd {
	return nil
}

// Update updates the datetime component
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	if !m.Focused() {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyRight:
			m.moveSegment(1)
			return m, nil
		case tea.KeyLeft:
			m.moveSegment(-1)
			return m, nil
		case tea.KeyUp:
			m.AdjustSegment(1)
			return m, nil
		case tea.KeyDown:
			m.AdjustSegment(-1)
			return m, nil
		}

		// Allow navigation keys to pass through
		if key.Matches(msg, m.input.KeyMap.CharacterForward,
			m.input.KeyMap.CharacterBackward,
			m.input.KeyMap.WordForward,
			m.input.KeyMap.WordBackward,
			m.input.KeyMap.LineStart,
			m.input.KeyMap.LineEnd) {
			switch {
			case key.Matches(msg, m.input.KeyMap.CharacterForward):
				m.moveSegment(1)
				return m, nil
			case key.Matches(msg, m.input.KeyMap.CharacterBackward):
				m.moveSegment(-1)
				return m, nil
			default:
				m.input, cmd = m.input.Update(msg)
			}
			return m, cmd
		}
		// do replace on current cursor
		if len(msg.Runes) == 1 && unicode.IsDigit(msg.Runes[0]) {
			pos := m.input.Position()
			oldValue := m.input.Value()
			newValue := []rune(oldValue)
			if pos < 0 || pos >= len(newValue) || !unicode.IsDigit(newValue[pos]) {
				return m, nil
			}
			newValue[pos] = msg.Runes[0]
			value := string(newValue)
			local, _ := time.LoadLocation("Local")
			newTime, err := time.ParseInLocation(localLayout, value, local)
			if err == nil {
				m.time = newTime
				m.SetTime(newTime)
				m.input.SetCursor(pos)
			}
		}
	}

	return m, nil
}

// AdjustSegment increments or decrements the currently focused date-time segment.
func (m *Model) AdjustSegment(delta int) {
	if delta == 0 {
		return
	}
	pos := m.input.Position()
	seg := segmentIndex(pos)
	next := m.time
	switch seg {
	case 0:
		next = next.AddDate(delta, 0, 0)
	case 1:
		next = addMonthsClamped(next, delta)
	case 2:
		next = next.AddDate(0, 0, delta)
	case 3:
		next = next.Add(time.Duration(delta) * time.Hour)
	case 4:
		next = next.Add(time.Duration(delta) * time.Minute)
	case 5:
		next = next.Add(time.Duration(delta) * time.Second)
	}
	m.SetTime(next)
	m.input.SetCursor(segmentStarts[seg])
}

func (m *Model) moveSegment(direction int) {
	seg := segmentIndex(m.input.Position())
	seg += direction
	if seg < 0 || seg >= len(segmentStarts) {
		return
	}
	m.input.SetCursor(segmentStarts[seg])
}

func segmentIndex(pos int) int {
	for i := range segmentStarts {
		if pos >= segmentStarts[i] && pos < segmentEnds[i] {
			return i
		}
	}
	if pos < segmentStarts[0] {
		return 0
	}
	for i := range segmentStarts {
		if pos < segmentStarts[i] {
			return i
		}
	}
	return len(segmentStarts) - 1
}

func addMonthsClamped(t time.Time, months int) time.Time {
	year, month, day := t.Date()
	hour, minute, sec := t.Clock()
	loc := t.Location()
	targetMonth := int(month) + months
	targetYear := year + (targetMonth-1)/12
	targetMonth = (targetMonth-1)%12 + 1
	if targetMonth <= 0 {
		targetMonth += 12
		targetYear--
	}
	lastDay := time.Date(targetYear, time.Month(targetMonth)+1, 0, hour, minute, sec, t.Nanosecond(), loc).Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(targetYear, time.Month(targetMonth), day, hour, minute, sec, t.Nanosecond(), loc)
}

// View returns the view of the datetime component
func (m Model) View() string {
	return m.input.View()
}
