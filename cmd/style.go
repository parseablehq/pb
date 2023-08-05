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
	"github.com/charmbracelet/lipgloss"
)

// styling for cli outputs
var (
	FocusPrimary  = lipgloss.AdaptiveColor{Light: "16", Dark: "226"}
	FocusSecondry = lipgloss.AdaptiveColor{Light: "18", Dark: "220"}

	standardPrimary  = lipgloss.AdaptiveColor{Light: "235", Dark: "255"}
	standardSecondry = lipgloss.AdaptiveColor{Light: "238", Dark: "254"}

	inactiveStyle    = lipgloss.NewStyle().Foreground(standardPrimary)
	inactiveStyleAlt = lipgloss.NewStyle().Foreground(standardSecondry)

	selectedStyle    = lipgloss.NewStyle().Foreground(FocusPrimary).Bold(true)
	selectedStyleAlt = lipgloss.NewStyle().Foreground(FocusSecondry)

	selectedItemOuter = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderLeft(true).PaddingLeft(1).BorderForeground(FocusPrimary)
	itemOuter         = lipgloss.NewStyle().PaddingLeft(1)

	styleBold = lipgloss.NewStyle().Bold(true)
)
