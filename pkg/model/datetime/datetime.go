package datetime

import (
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type Model struct {
	time  time.Time
	input textinput.Model
}

func (m *Model) Value() string {
	return m.time.Format(time.RFC3339)
}

func (m *Model) ValueUtc() string {
	return m.time.UTC().Format(time.RFC3339)
}

func (m *Model) SetTime(t time.Time) {
	m.time = t
	m.input.SetValue(m.time.Format(time.DateTime))
}

func (m *Model) Time() time.Time {
	return m.time
}

func New(prompt string) Model {
	input := textinput.New()
	input.Width = 20
	input.Prompt = prompt

	return Model{
		time:  time.Now(),
		input: input,
	}
}

func (m *Model) Focus() tea.Cmd {
	m.input.Focus()
	return nil
}

func (m *Model) Blur() {
	m.input.Blur()
}

func (m *Model) Focused() bool {
	return m.input.Focused()
}

func (m Model) Init() tea.Cmd {
	return nil
}

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
			time, err := time.Parse(time.DateTime, value)
			*time.Location() = *m.time.Location()
			if err == nil {
				m.time = time
				m.input.SetValue(value)
			}
		}
	}

	return m, nil
}

func (m Model) View() string {
	return m.input.View()
}
