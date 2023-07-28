package model

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle  = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#CDB4DBAA", Dark: "#023047AA"})
	hostStyle   = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#FFAFCCAA", Dark: "#219EBCCC"})
	streamStyle = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#CDB4DBAA", Dark: "#C1121FAA"})
	infoStyle   = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#9381FFAA", Dark: "#051923AA"})
	errorStyle  = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#D8115922", Dark: "#D81159AA"})
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

	right = right_style.AlignHorizontal(lipgloss.Right).Width(right_width).Render(right)

	return lipgloss.JoinHorizontal(lipgloss.Bottom, left, right)
}
