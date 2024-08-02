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
package model

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"pb/pkg/config"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var docStyle = lipgloss.NewStyle().Margin(1, 2)

// FilterDetails represents the structure of filter data
type FilterDetails struct {
	FilterId   string                 `json:"filter_id"`
	FilterName string                 `json:"filter_name"`
	StreamName string                 `json:"stream_name"`
	QueryField map[string]interface{} `json:"query"`
	TimeFilter map[string]interface{} `json:"time_filter"`
}

type item struct {
	id,title, stream, desc, from, to string
}




func (i item) Title() string { return i.title }

func (i item) Description() string {
	if i.to == "" || i.from==""{
		return i.desc
	}else{
	 return fmt.Sprintf("%s From:%s To:%s",i.desc,i.from,i.to)
	} 
	}

func (i item) FilterValue() string { return i.title }

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

func UiApp() *tea.Program {

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

	m := modelFilter{list: list.New(userFilters, list.NewDefaultDelegate(), 0, 0)}
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
		fmt.Println("Error unmarshaling response:", err)
		return nil
	}
	var userFilters []list.Item
	for _, filter := range filters {
		var userFilter item
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
		if string(queryBytes) != "null" {userFilter = item{
			id:filter.FilterId,
			title: filter.FilterName,
			stream: filter.StreamName,
			desc: string(queryBytes),
			from: from,
			to: to,
		}
		userFilters = append(userFilters, userFilter)}
	}
	return userFilters

}
