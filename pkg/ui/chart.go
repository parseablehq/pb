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

package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/guptarohit/asciigraph"
)

// ASCIIChart renders a single-series time chart using guptarohit/asciigraph.
// Multi-series rendering is intentionally left to the caller — overlay
// passes work better with explicit color control.
//
// width / height are in cells. Caller should pre-fill or downsample series
// so len(series) <= width-10 (left margin for y-axis labels).
func ASCIIChart(series []float64, width, height int, caption string) string {
	p := Active

	if len(series) == 0 {
		return lipgloss.NewStyle().
			Foreground(p.Faint).
			Width(width).
			Height(height).
			Align(lipgloss.Center, lipgloss.Center).
			Render("(no data)")
	}

	if width < 20 {
		width = 20
	}
	if height < 6 {
		height = 6
	}

	plot := asciigraph.Plot(
		series,
		asciigraph.Width(width-10),
		asciigraph.Height(height-2),
		asciigraph.Caption(caption),
	)

	return lipgloss.NewStyle().
		Foreground(p.Accent).
		Render(plot)
}
