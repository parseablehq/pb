package button

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Pressed bool

type Model struct {
	text          string
	focusStyle    lipgloss.Style
	blurredStyle  lipgloss.Style
	focus         bool
	invalid_state bool
}

func New(text string) Model {
	return Model{
		text:         text,
		focusStyle:   lipgloss.NewStyle(),
		blurredStyle: lipgloss.NewStyle(),
	}
}

func (m *Model) Focus() tea.Cmd {
	m.focus = true
	return nil
}

func (m *Model) Blur() {
	m.focus = false
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if !m.focus {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			return m, func() tea.Msg { return Pressed(true) }
		default:
			return m, nil
		}
	}

	return m, nil
}

func (m Model) View() string {
	var b strings.Builder
	var text string
	if m.invalid_state {
		text = "X"
	} else {
		text = m.text
	}

	b.WriteString("[ ")
	if m.focus {
		text = m.focusStyle.Render(text)
	} else {
		text = m.blurredStyle.Render(text)
	}
	b.WriteString(text)
	b.WriteString(" ]")

	return b.String()
}
