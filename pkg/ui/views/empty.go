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

package views

import (
	"github.com/parseablehq/pb/pkg/ui"

	tea "github.com/charmbracelet/bubbletea"
)

// EmptyView is a transitional placeholder for views that are not yet
// ported. Each greenfield PR replaces one of these with the real
// implementation.
type EmptyView struct {
	Title string
	Hint  string
	Keys  []ui.KeyHint
}

func (EmptyView) Init() tea.Cmd                          { return nil }
func (EmptyView) Update(_ tea.Msg, _ *ui.AppCtx) tea.Cmd { return nil }
func (v EmptyView) Render(width, height int, _ *ui.AppCtx) string {
	return renderEmptyBody(width, height, v.Title, v.Hint)
}
func (v EmptyView) HeaderKeys() []ui.KeyHint { return v.Keys }
