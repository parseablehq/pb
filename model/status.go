package model

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	commonStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#000000"})

	titleStyle = commonStyle.Copy().
			Background(lipgloss.AdaptiveColor{Light: "#134074", Dark: "#FFADAD"}).
			Padding(0, 1)

	hostStyle = commonStyle.Copy().
			Background(lipgloss.AdaptiveColor{Light: "#13315C", Dark: "#FFD6A5"}).
			Padding(0, 1)

	streamStyle = commonStyle.Copy().
			Background(lipgloss.AdaptiveColor{Light: "#0B2545", Dark: "#FDFFB6"}).
			Padding(0, 1)

	infoStyle = commonStyle.Copy().
			Background(lipgloss.AdaptiveColor{Light: "#212529", Dark: "#CAFFBF"}).
			AlignHorizontal(lipgloss.Right)

	errorStyle = commonStyle.Copy().
			Background(lipgloss.AdaptiveColor{Light: "#5A2A27", Dark: "#D4A373"}).
			AlignHorizontal(lipgloss.Right)
)

type StatusBar struct {
	title  string
	host   string
	stream string
	Info   string
	Error  string
	width  int
}

func NewStatusBar(host string, stream string, width int) StatusBar {
	return StatusBar{
		title:  "Parseable",
		host:   host,
		stream: stream,
		Info:   "",
		Error:  "",
		width:  width,
	}
}

func (m StatusBar) Init() tea.Cmd {
	return nil
}

func (m StatusBar) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m StatusBar) View() string {
	var right string
	var right_style lipgloss.Style

	if m.Error != "" {
		right = m.Error
		right_style = errorStyle
	} else {
		right = m.Info
		right_style = infoStyle
	}

	left := lipgloss.JoinHorizontal(lipgloss.Bottom, titleStyle.Render(m.title), hostStyle.Render(m.host), streamStyle.Render(m.stream))

	left_width := lipgloss.Width(left)
	right_width := m.width - left_width

	right = right_style.Width(right_width).Render(right)

	return lipgloss.JoinHorizontal(lipgloss.Bottom, left, right)
}
