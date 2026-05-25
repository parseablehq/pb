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
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// PBLogo is the small "PB" ASCII mark shown in the top-right of the
// HeaderStrip. Five lines, matches the mock in terminal/page.tsx.
const PBLogo = `  ____   ____
 |  _ \ | __ )
 | |_) ||  _ \
 |  __/ | |_) |
 |_|    |____/ `

// RenderLogo paints the PB mark in the active palette's accent color.
func RenderLogo() string {
	p := Active
	return lipgloss.NewStyle().Foreground(p.Accent).Render(PBLogo)
}

// LogoLines reports the number of rows in PBLogo. Used to set column
// height when stacking next to the KV / keybind columns.
func LogoLines() int { return strings.Count(PBLogo, "\n") + 1 }
