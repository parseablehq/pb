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

package defaultprofile

import (
	"fmt"
	"io"

	"pb/pkg/config"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	// FocusPrimary is the primary focus color
	FocusPrimary = lipgloss.AdaptiveColor{Light: "16", Dark: "226"}
	// FocusSecondry is the secondry focus color
	FocusSecondry = lipgloss.AdaptiveColor{Light: "18", Dark: "220"}
	// StandardPrimary is the primary standard color
	StandardPrimary = lipgloss.AdaptiveColor{Light: "235", Dark: "255"}
	// StandardSecondary is the secondary standard color
	StandardSecondary = lipgloss.AdaptiveColor{Light: "238", Dark: "254"}

	focusTitleStyle   = lipgloss.NewStyle().Foreground(FocusPrimary)
	focusDescStyle    = lipgloss.NewStyle().Foreground(FocusSecondry)
	focusedOuterStyle = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderLeft(true).BorderForeground(FocusPrimary)

	standardTitleStyle = lipgloss.NewStyle().Foreground(StandardPrimary)
	standardDescStyle  = lipgloss.NewStyle().Foreground(StandardSecondary)
)

type item struct {
	title, url, user string
}

func (i item) FilterValue() string { return i.title }

type itemDelegate struct{}

func (d itemDelegate) Height() int                             { return 3 }
func (d itemDelegate) Spacing() int                            { return 1 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, _ := listItem.(item)

	var titleStyle lipgloss.Style
	var descStyle lipgloss.Style

	if index == m.Index() {
		titleStyle = focusTitleStyle
		descStyle = focusDescStyle
	} else {
		titleStyle = standardTitleStyle
		descStyle = standardDescStyle
	}

	render := fmt.Sprintf(
		"%s\n%s\n%s",
		titleStyle.Render(item.title),
		descStyle.Render(item.url),
		descStyle.Render(item.user),
	)

	if index == m.Index() {
		render = focusedOuterStyle.Render(render)
	}

	fmt.Fprint(w, render)
}

// Model for profile selection command
type Model struct {
	list    list.Model
	Choice  string
	Success bool
}

func New(profiles map[string]config.Profile) Model {
	items := []list.Item{}
	for name, profile := range profiles {
		i := item{
			title: name,
			url:   profile.URL,
			user:  profile.Username,
		}
		items = append(items, i)
	}

	list := list.New(items, itemDelegate{}, 80, 19)
	list.SetShowStatusBar(false)
	list.SetShowTitle(false)

	list.Styles.PaginationStyle = list.Styles.PaginationStyle.MarginLeft(1).Padding(0)
	list.Styles.HelpStyle = list.Styles.HelpStyle.MarginLeft(1).Padding(0)

	list.Paginator.ActiveDot = "● "
	list.Paginator.InactiveDot = "○ "

	list.KeyMap.ShowFullHelp.SetEnabled(false)
	list.KeyMap.CloseFullHelp.SetEnabled(false)

	list.SetFilteringEnabled(true)

	m := Model{
		list:    list,
		Choice:  "",
		Success: false,
	}

	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		default:
			if msg.Type == tea.KeyEnter && m.list.FilterState() != list.Filtering {
				m.Success = true
				m.Choice = m.list.SelectedItem().FilterValue()
				return m, tea.Quit
			}
			m.list, cmd = m.list.Update(msg)
		}
	}
	return m, cmd
}

func (m Model) View() string {
	return lipgloss.NewStyle().PaddingLeft(1).Render(m.list.View())
}
