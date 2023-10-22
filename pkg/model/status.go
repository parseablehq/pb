// Copyright (c) 2023 Cloudnatively Services Pvt Ltd
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

	infoStyle = commonStyle.Copy().
			Background(lipgloss.AdaptiveColor{Light: "#212529", Dark: "#CAFFBF"}).
			AlignHorizontal(lipgloss.Right)

	errorStyle = commonStyle.Copy().
			Background(lipgloss.AdaptiveColor{Light: "#5A2A27", Dark: "#D4A373"}).
			AlignHorizontal(lipgloss.Right)
)

type StatusBar struct {
	title string
	host  string
	Info  string
	Error string
	width int
}

func NewStatusBar(host string, width int) StatusBar {
	return StatusBar{
		title: "Parseable",
		host:  host,
		Info:  "",
		Error: "",
		width: width,
	}
}

func (m StatusBar) Init() tea.Cmd {
	return nil
}

func (m StatusBar) Update(_ tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m StatusBar) View() string {
	var right string
	var rightStyle lipgloss.Style

	if m.Error != "" {
		right = m.Error
		rightStyle = errorStyle
	} else {
		right = m.Info
		rightStyle = infoStyle
	}

	left := lipgloss.JoinHorizontal(lipgloss.Bottom, titleStyle.Render(m.title), hostStyle.Render(m.host))

	leftWidth := lipgloss.Width(left)
	rightWidth := m.width - leftWidth

	right = rightStyle.Width(rightWidth).Render(right)

	return lipgloss.JoinHorizontal(lipgloss.Bottom, left, right)
}
