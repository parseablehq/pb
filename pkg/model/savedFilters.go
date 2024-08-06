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

package model

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"pb/pkg/config"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	applyFilterButton  = "a"
	deleteFilterButton = "d"
)

var docStyle = lipgloss.NewStyle().Margin(1, 2)

// FilterDetails represents the structure of filter data
type FilterDetails struct {
	FilterID   string                 `json:"filter_id"`
	FilterName string                 `json:"filter_name"`
	StreamName string                 `json:"stream_name"`
	QueryField map[string]interface{} `json:"query"`
	TimeFilter map[string]interface{} `json:"time_filter"`
}

// Item represents the structure of the filter item
type Item struct {
	id, title, stream, desc, from, to string
}

var (
	titleStyles = lipgloss.NewStyle().PaddingLeft(0).Bold(true).Foreground(lipgloss.Color("9"))
	queryStyle  = lipgloss.NewStyle().PaddingLeft(0).Foreground(lipgloss.Color("7"))
	itemStyle   = lipgloss.NewStyle().PaddingLeft(4).Foreground(lipgloss.Color("8"))
	// selectedItemStyle = lipgloss.NewStyle().PaddingLeft(4).Foreground(lipgloss.Color("170"))
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(1).Foreground(lipgloss.AdaptiveColor{Light: "16", Dark: "226"})
)

type itemDelegate struct{}

func (d itemDelegate) Height() int                             { return 4 }
func (d itemDelegate) Spacing() int                            { return 1 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(Item)
	if !ok {
		return
	}
	var str string

	if i.from != "" || i.to != "" {
		str = fmt.Sprintf("From: %s\nTo: %s", i.from, i.to)
	} else {
		str = ""
	}

	fn := itemStyle.Render
	tr := titleStyles.Render
	qr := queryStyle.Render
	if index == m.Index() {
		tr = func(s ...string) string {
			return selectedItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(tr(i.title)+"\n"+qr(i.desc)+"\n"+str))
}

func (d itemDelegate) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(
			key.WithKeys(applyFilterButton),
			key.WithHelp(applyFilterButton, "apply"),
		),
		key.NewBinding(
			key.WithKeys(deleteFilterButton),
			key.WithHelp(deleteFilterButton, "delete"),
		),
	}
}

// FullHelp returns the extended list of keybindings.
func (d itemDelegate) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{
			key.NewBinding(
				key.WithKeys(applyFilterButton),
				key.WithHelp(applyFilterButton, "apply"),
			),
			key.NewBinding(
				key.WithKeys(deleteFilterButton),
				key.WithHelp(deleteFilterButton, "delete"),
			),
		},
	}
}

var (
	selectedFilterApply  Item
	selectedFilterDelete Item
)

func (i Item) Title() string { return fmt.Sprintf("Filter:%s, Query:%s", i.title, i.desc) }

func (i Item) Description() string {
	if i.to == "" || i.from == "" {
		return ""
	}
	return fmt.Sprintf("From:%s To:%s", i.from, i.to)
}

func (i Item) FilterValue() string { return i.title }
func (i Item) FilterID() string    { return i.id }
func (i Item) Stream() string      { return i.desc }
func (i Item) StartTime() string   { return i.from }
func (i Item) EndTime() string     { return i.to }

type modelFilter struct {
	list list.Model
}

func (m modelFilter) Init() tea.Cmd {
	return nil
}

func (m modelFilter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if msg.String() == "a" || msg.Type == tea.KeyEnter {
			selectedFilterApply = m.list.SelectedItem().(Item)
			return m, tea.Quit
		}
		if msg.String() == "d" {
			selectedFilterDelete = m.list.SelectedItem().(Item)
			return m, tea.Quit

		}
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m modelFilter) View() string {
	return docStyle.Render(m.list.View())
}

// UIApp lists interactive list for the user to display all the available filters (only saved SQL filters )
func UIApp() *tea.Program {
	userConfig, err := config.ReadConfigFromFile()
	if err != nil {
		fmt.Println("Error reading Default Profile")
	}
	var userProfile config.Profile
	if profile, ok := userConfig.Profiles[userConfig.DefaultProfile]; ok {
		userProfile = profile
	}

	client := &http.Client{
		Timeout: time.Second * 60,
	}
	userFilters := fetchFilters(client, &userProfile)

	m := modelFilter{list: list.New(userFilters, itemDelegate{}, 0, 0)}
	m.list.Title = fmt.Sprintf("Saved Filters for User: %s", userProfile.Username)

	return tea.NewProgram(m, tea.WithAltScreen())
}

// fetchFilters fetches filters from the server and sends them to the channel
func fetchFilters(client *http.Client, profile *config.Profile) []list.Item {
	endpoint := fmt.Sprintf("%s/%s/%s", profile.URL, "api/v1/filters", profile.Username)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return nil
	}

	req.SetBasicAuth(profile.Username, profile.Password)
	req.Header.Add("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error making request:", err)
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response body:", err)
		return nil
	}

	var filters []FilterDetails
	err = json.Unmarshal(body, &filters)
	if err != nil {
		fmt.Println("Error unmarshalling response:", err)
		return nil
	}
	var userFilters []list.Item
	for _, filter := range filters {
		var userFilter Item
		queryBytes, _ := json.Marshal(filter.QueryField["filter_query"])

		// Extract "from" and "to" from time_filter
		var from, to string
		if fromValue, exists := filter.TimeFilter["from"]; exists {
			from = fmt.Sprintf("%v", fromValue)
		}
		if toValue, exists := filter.TimeFilter["to"]; exists {
			to = fmt.Sprintf("%v", toValue)
		}
		// filtering only SQL type filters Filter_name is tile and Stream Name is desc
		if string(queryBytes) != "null" {
			userFilter = Item{
				id:     filter.FilterID,
				title:  filter.FilterName,
				stream: filter.StreamName,
				desc:   string(queryBytes),
				from:   from,
				to:     to,
			}
			userFilters = append(userFilters, userFilter)
		}
	}
	return userFilters
}

// FilterToApply returns the selected filter by user in the interactive list to apply
func FilterToApply() Item {
	return selectedFilterApply
}

// FilterToDelete returns the selected filter by user in the interactive list to delete
func FilterToDelete() Item {
	return selectedFilterDelete
}
