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
	"github.com/charmbracelet/lipgloss"
)

// styling for cli outputs
var (
	FocusPrimary   = lipgloss.AdaptiveColor{Light: "16", Dark: "226"}
	FocusSecondary = lipgloss.AdaptiveColor{Light: "18", Dark: "220"}

	StandardPrimary   = lipgloss.AdaptiveColor{Light: "235", Dark: "255"}
	StandardSecondary = lipgloss.AdaptiveColor{Light: "238", Dark: "254"}
	StandardStyle     = lipgloss.NewStyle().Foreground(StandardPrimary)
	StandardStyleBold = lipgloss.NewStyle().Foreground(StandardPrimary).Bold(true)
	StandardStyleAlt  = lipgloss.NewStyle().Foreground(StandardSecondary)
	SelectedStyle     = lipgloss.NewStyle().Foreground(FocusPrimary).Bold(true)
	SelectedStyleAlt  = lipgloss.NewStyle().Foreground(FocusSecondary)
	SelectedItemOuter = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderLeft(true).PaddingLeft(1).BorderForeground(FocusPrimary)
	ItemOuter         = lipgloss.NewStyle().PaddingLeft(1)

	StyleBold = lipgloss.NewStyle().Bold(true)
)
