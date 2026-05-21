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

func (m StatusBar) Init() tea.Cmd                        { return nil }
func (m StatusBar) Update(_ tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m StatusBar) View() string {
	p := ui.Active

	sep := lipgloss.NewStyle().
		Foreground(p.BorderSoft).
		Background(p.Panel).
		Render(" │ ")

	label := func(s string) string {
		return lipgloss.NewStyle().
			Foreground(p.Faint).
			Background(p.Panel).
			Render(s)
	}
	value := func(s string, fg lipgloss.Color, bold bool) string {
		st := lipgloss.NewStyle().Foreground(fg).Background(p.Panel)
		if bold {
			st = st.Bold(true)
		}
		return st.Render(s)
	}

	// ── Left: MODE · CLUSTER ──
	leftParts := []string{
		" ",
		label("MODE"),
		" ",
		value(strings.ToUpper(m.title), p.Accent, true),
		sep,
		label("CLUSTER"),
		" ",
		value(m.host, p.Dim, false),
	}
	left := lipgloss.JoinHorizontal(lipgloss.Bottom, leftParts...)

	// ── Right: ROWS · LIVE · t · ? ──
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
		sep,
		label("?"),
		" ",
		value("help", p.Dim, false),
		" ",
	)
	right := lipgloss.JoinHorizontal(lipgloss.Bottom, rightParts...)

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	pad := lipgloss.NewStyle().
		Width(gap).
		Background(p.Panel).
		Render("")

	row := lipgloss.JoinHorizontal(lipgloss.Bottom, left, pad, right)

	return lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(p.BorderSoft).
		Width(m.width).
		Render(row)
}
