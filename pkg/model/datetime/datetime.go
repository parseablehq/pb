package datetime

import (
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
	m.input.SetValue(m.time.Format(time.DateTime))
}

// Time returns the current time of the datetime component
func (m *Model) Time() time.Time {
	return m.time
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
		// Allow navigation keys to pass through
		if key.Matches(msg, m.input.KeyMap.CharacterForward,
			m.input.KeyMap.CharacterBackward,
			m.input.KeyMap.WordForward,
			m.input.KeyMap.WordBackward,
			m.input.KeyMap.LineStart,
			m.input.KeyMap.LineEnd) {
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		// do replace on current cursor
		if len(msg.Runes) == 1 && unicode.IsDigit(msg.Runes[0]) {
			pos := m.input.Position()
			oldValue := m.input.Value()
			newValue := []rune(oldValue)
			newValue[pos] = msg.Runes[0]
			value := string(newValue)
			local, _ := time.LoadLocation("Local")
			newTime, err := time.ParseInLocation(time.DateTime, value, local)
			if err == nil {
				m.time = newTime
				m.SetTime(newTime)
			}
		}
	}

	return m, nil
}

// View returns the view of the datetime component
func (m Model) View() string {
	return m.input.View()
}
