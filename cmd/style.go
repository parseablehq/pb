// Copyright (c) 2023 Cloudnatively Services Pvt Ltd
//
// This file is part of MinIO Object Storage stack
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
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

// styling for cli outputs
var (
	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "4", Dark: "11"}).Bold(true)
	styleBold = lipgloss.NewStyle().Bold(true)
)

func listingTableStyle() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.Border(lipgloss.NormalBorder(), false, false, true, false)
	s.Selected = selectedStyle
	return s
}
