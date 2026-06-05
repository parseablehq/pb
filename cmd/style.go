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

package cmd

import (
	"github.com/parseablehq/pb/pkg/ui"

	"github.com/charmbracelet/lipgloss"
)

// Styles for the cobra command CLI outputs (prompts, error messages, list
// items rendered outside the bubbletea TUI). Sourced from the shared
// ui.Palette so any palette change auto-propagates here.
//
// Names kept stable for backwards compatibility with existing call sites
// across cmd/ and pkg/model/.
var (
	// FocusPrimary / FocusSecondary used to be yellow (ANSI 226/220). Now
	// brand indigo — same role (selected / active item highlight) but
	// matches the rest of the design system.
	FocusPrimary   = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Accent })
	FocusSecondary = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Accent2 })

	StandardPrimary   = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Body })
	StandardSecondary = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Mute })

	StandardStyle     = lipgloss.NewStyle().Foreground(StandardPrimary)
	StandardStyleBold = lipgloss.NewStyle().Foreground(StandardPrimary).Bold(true)
	StandardStyleAlt  = lipgloss.NewStyle().Foreground(StandardSecondary)
	StandardStyleRule = lipgloss.NewStyle().Foreground(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Border }))
	SelectedStyle     = lipgloss.NewStyle().Foreground(FocusPrimary).Bold(true)
	SelectedStyleAlt  = lipgloss.NewStyle().Foreground(FocusSecondary)
	SelectedItemOuter = lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderLeft(true).
				PaddingLeft(1).
				BorderForeground(FocusPrimary)
	ItemOuter = lipgloss.NewStyle().PaddingLeft(1)

	StyleBold = lipgloss.NewStyle().Bold(true)
)
