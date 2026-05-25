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

// Package views holds the per-screen render + Update logic. Each file
// implements one ViewID. App in pkg/ui wires them.
package views

import (
	"pb/pkg/ui"

	"github.com/charmbracelet/lipgloss"
)

// Placeholder renders the "not built yet" body for views that have not
// been implemented during the greenfield migration. It accepts the
// active view label and a one-line hint.
//
// Once each view (results / metrics / time / saved / help) gets its
// real implementation, this helper is deleted.
type Placeholder struct {
	Title string
	Hint  string
}

func (Placeholder) Init() (cmd interface{ Run() }) { return nil }

func renderEmptyBody(width, height int, title, hint string) string {
	p := ui.Active
	t := lipgloss.NewStyle().
		Foreground(p.Faint).
		Bold(true).
		Render(title)
	h := lipgloss.NewStyle().
		Foreground(p.Ghost).
		MarginTop(1).
		Render(hint)
	body := lipgloss.JoinVertical(lipgloss.Center, t, h)
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Background(p.Bg).
		Align(lipgloss.Center, lipgloss.Center).
		Render(body)
}
