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
	"pb/pkg/ui"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Segmented status bar matching the design mock — k9s/helix idiom.
// Left segments (mode, cluster) are sticky. Right segments (LIVE, latency,
// help hint) collapse first when width is tight.

type StatusBar struct {
	title string // mode label (SQL / PromQL)
	host  string // cluster host
	Info  string // optional info string (rows, etc.)
	Error string // error overrides Info on the right side
	width int
}

func NewStatusBar(host string, width int) StatusBar {
	return StatusBar{
		title: "parseable",
		host:  host,
		Info:  "",
		Error: "",
		width: width,
	}
}

// SetMode lets callers update the MODE segment (e.g. "SQL" / "PromQL").
func (m *StatusBar) SetMode(mode string) { m.title = mode }

func (m StatusBar) Init() tea.Cmd                         { return nil }
func (m StatusBar) Update(_ tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m StatusBar) View() string {
	p := ui.Active

	sep := lipgloss.NewStyle().Foreground(p.BorderSoft).Render(" │ ")

	label := func(s string) string {
		return lipgloss.NewStyle().Foreground(p.Faint).Render(s)
	}
	value := func(s string, fg lipgloss.Color, bold bool) string {
		st := lipgloss.NewStyle().Foreground(fg)
		if bold {
			st = st.Bold(true)
		}
		return st.Render(s)
	}

	leftParts := []string{
		label("MODE"),
		" ",
		value(strings.ToUpper(m.title), p.Accent, true),
		sep,
		label("CLUSTER"),
		" ",
		value(m.host, p.Dim, false),
	}
	left := lipgloss.JoinHorizontal(lipgloss.Bottom, leftParts...)

	var rightParts []string
	if m.Error != "" {
		rightParts = append(rightParts,
			label("ERR"),
			" ",
			value(m.Error, p.Err, true),
			sep,
		)
	} else if m.Info != "" {
		rightParts = append(rightParts,
			label("·"),
			" ",
			value(m.Info, p.Body, false),
			sep,
		)
	}
	rightParts = append(rightParts,
		label("LIVE"),
		" ",
		value("●", p.Ok, true),
	)
	right := lipgloss.JoinHorizontal(lipgloss.Bottom, rightParts...)

	// Total bordered width must equal m.width to match the help bar
	// above it. Border = 2 cells, h-padding inside = 2 cells. So
	// inner Width = m.width - 2, content area inside padding =
	// m.width - 4. Use the content area for gap math so right-aligned
	// segments don't push past the padding into the border glyph.
	innerW := m.width - 2
	if innerW < 1 {
		innerW = 1
	}
	contentW := innerW - 2
	if contentW < 1 {
		contentW = 1
	}
	if lipgloss.Width(left)+lipgloss.Width(right) > contentW {
		right = ""
	}
	gap := contentW - lipgloss.Width(left) - lipgloss.Width(right)
	if right != "" && gap < 1 {
		gap = 1
	} else if gap < 0 {
		gap = 0
	}
	row := left + strings.Repeat(" ", gap) + right
	inner := lipgloss.NewStyle().Width(innerW).Padding(0, 1).Render(row)

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(p.Border).
		Render(inner)
}
