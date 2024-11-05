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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"pb/pkg/config"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	applyQueryButton = "a"
	backButton       = "b"
	confirmDelete    = "y"
	cancelDelete     = "n"
)

var (
	docStyle = lipgloss.NewStyle().Margin(1, 2, 3)
)

type Filter struct {
	Version    string     `json:"version"`
	UserID     string     `json:"user_id"`
	StreamName string     `json:"stream_name"`
	FilterName string     `json:"filter_name"`
	FilterID   string     `json:"filter_id"`
	Query      Query      `json:"query"`
	TimeFilter TimeFilter `json:"time_filter"`
}

type TimeFilter struct {
	To   string `json:"to"`
	From string `json:"from"`
}
type Query struct {
	FilterType    string         `json:"filter_type"`
	FilterQuery   *string        `json:"filter_query,omitempty"`   // SQL query as string or null
	FilterBuilder *FilterBuilder `json:"filter_builder,omitempty"` // Builder or null
}

type FilterBuilder struct {
	ID         string    `json:"id"`
	Combinator string    `json:"combinator"`
	Rules      []RuleSet `json:"rules"`
}

type RuleSet struct {
	ID         string `json:"id"`
	Combinator string `json:"combinator"`
	Rules      []Rule `json:"rules"`
}

type Rule struct {
	ID       string `json:"id"`
	Field    string `json:"field"`
	Value    string `json:"value"`
	Operator string `json:"operator"`
}

// Item represents the struct of the saved query item
type Item struct {
	id, title, stream, desc, from, to string
}

var (
	titleStyles       = lipgloss.NewStyle().PaddingLeft(0).Bold(true).Foreground(lipgloss.Color("9"))
	queryStyle        = lipgloss.NewStyle().PaddingLeft(0).Foreground(lipgloss.Color("7"))
	itemStyle         = lipgloss.NewStyle().PaddingLeft(4).Foreground(lipgloss.Color("8"))
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

// Implement itemDelegate ShortHelp to show only relevant bindings.
func (d itemDelegate) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(
			key.WithKeys(applyQueryButton),
			key.WithHelp(applyQueryButton, "apply"),
		),
		key.NewBinding(
			key.WithKeys(backButton),
			key.WithHelp(backButton, "back"),
		),
	}
}

// Implement FullHelp to show only "apply" and "back" key bindings.
func (d itemDelegate) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{
			key.NewBinding(
				key.WithKeys(applyQueryButton),
				key.WithHelp(applyQueryButton, "apply"),
			),
			key.NewBinding(
				key.WithKeys(backButton),
				key.WithHelp(backButton, "back"),
			),
		},
	}
}

var (
	selectedQueryApply  Item
	selectedQueryDelete Item
)

func (i Item) Title() string { return fmt.Sprintf("Title:%s, Query:%s", i.title, i.desc) }

func (i Item) Description() string {
	if i.to == "" || i.from == "" {
		return ""
	}
	return fmt.Sprintf("From:%s To:%s", i.from, i.to)
}

func (i Item) FilterValue() string  { return i.title }
func (i Item) SavedQueryID() string { return i.id }
func (i Item) Stream() string       { return i.desc }
func (i Item) StartTime() string    { return i.from }
func (i Item) EndTime() string      { return i.to }

type modelSavedQueries struct {
	list          list.Model
	commandOutput string
	viewport      viewport.Model
	queryExecuted bool // New field to track query execution
}

func (m modelSavedQueries) Init() tea.Cmd {
	return nil
}

// Define a message type for command results
type commandResultMsg string

func (m modelSavedQueries) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "a", "enter":
			// Only execute if a query hasn't already been run
			if m.queryExecuted {
				return m, nil // Skip execution if already executed
			}
			selectedQueryApply := m.list.SelectedItem().(Item)
			m.queryExecuted = true // Mark as executed

			cmd := func() tea.Msg {
				// Load user profile configuration
				userConfig, err := config.ReadConfigFromFile()
				if err != nil {
					return commandResultMsg(fmt.Sprintf("Error: %s", err))
				}

				profile, profileExists := userConfig.Profiles[userConfig.DefaultProfile]
				if !profileExists {
					return commandResultMsg("Error: Profile not found")
				}

				// Clean the query string
				cleanedQuery := strings.TrimSpace(strings.ReplaceAll(selectedQueryApply.desc, `\`, ""))
				cleanedQuery = strings.ReplaceAll(cleanedQuery, `"`, "")

				// Log the command for debugging
				fmt.Printf("Executing command: pb query run %s\n", cleanedQuery)

				// Prepare HTTP client
				client := &http.Client{Timeout: 60 * time.Second}

				// Determine query time range
				startTime := selectedQueryApply.StartTime()
				endTime := selectedQueryApply.EndTime()

				// If start and end times are not set, use a default range
				if startTime == "" && endTime == "" {
					startTime = "10m"
					endTime = "now"
				}

				// Run the query
				data, err := RunQuery(client, &profile, cleanedQuery, startTime, endTime)
				if err != nil {
					return commandResultMsg(fmt.Sprintf("Error: %s", err))
				}
				return commandResultMsg(data)
			}
			return m, cmd

		case "b": // 'b' to go back to the saved query list
			m.commandOutput = ""      // Clear the command output
			m.viewport.SetContent("") // Clear viewport content
			m.viewport.GotoTop()      // Reset viewport to the top
			m.queryExecuted = false   // Reset the execution flag to allow a new query
			return m, nil

		case "down", "j":
			m.viewport.LineDown(1) // Scroll down in the viewport

		case "up", "k":
			m.viewport.LineUp(1) // Scroll up in the viewport
		}

	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)
		m.viewport.Width = msg.Width - h
		m.viewport.Height = msg.Height - v

	case commandResultMsg:
		m.commandOutput = string(msg)
		m.viewport.SetContent(m.commandOutput) // Update viewport content with command output
		return m, nil
	}

	// Update the list and return
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}
func (m modelSavedQueries) View() string {
	if m.commandOutput != "" {
		return m.viewport.View()
	}
	return m.list.View()
}

// SavedQueriesMenu is a TUI which lists all available saved queries for the active user (only SQL queries )
func SavedQueriesMenu() *tea.Program {
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
	userSavedQueries := fetchFilters(client, &userProfile)

	m := modelSavedQueries{list: list.New(userSavedQueries, itemDelegate{}, 0, 0)}
	m.list.Title = fmt.Sprintf("Saved Queries for User: %s", userProfile.Username)

	return tea.NewProgram(m, tea.WithAltScreen())
}

// fetchFilters fetches saved SQL queries for the active user from the server
func fetchFilters(client *http.Client, profile *config.Profile) []list.Item {
	endpoint := fmt.Sprintf("%s/%s", profile.URL, "api/v1/filters")
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

	var filters []Filter
	err = json.Unmarshal(body, &filters)
	if err != nil {
		fmt.Println("Error unmarshalling response:", err)
		return nil
	}

	// This returns only the SQL type filters
	var userSavedQueries []list.Item
	for _, filter := range filters {
		if filter.Query.FilterQuery == nil {
			continue // Skip this filter if FilterQuery is null
		}
		queryBytes, _ := json.Marshal(filter.Query.FilterQuery)

		userSavedQuery := Item{
			id:     filter.FilterID,
			title:  filter.FilterName,
			stream: filter.StreamName,
			desc:   string(queryBytes),
			from:   filter.TimeFilter.From,
			to:     filter.TimeFilter.To,
		}
		userSavedQueries = append(userSavedQueries, userSavedQuery)

	}
	return userSavedQueries
}

// QueryToApply returns the selected saved query by user in the interactive list to apply
func QueryToApply() Item {
	return selectedQueryApply
}

// QueryToDelete returns the selected saved query by user in the interactive list to delete
func QueryToDelete() Item {
	return selectedQueryDelete
}

func RunQuery(client *http.Client, profile *config.Profile, query string, startTime string, endTime string) (string, error) {
	queryTemplate := `{
		"query": "%s",
		"startTime": "%s",
		"endTime": "%s"
	}`

	finalQuery := fmt.Sprintf(queryTemplate, query, startTime, endTime)

	endpoint := fmt.Sprintf("%s/%s", profile.URL, "api/v1/query")
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer([]byte(finalQuery)))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(profile.Username, profile.Password)
	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var jsonResponse []map[string]interface{}

		// Read and parse the JSON response
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}

		// Decode JSON into a map
		if err := json.Unmarshal(body, &jsonResponse); err != nil {
			return "", err
		}

		// Pretty-print the JSON response
		jsonData, err := json.MarshalIndent(jsonResponse, "", "  ")
		if err != nil {
			return "", err
		}
		return string(jsonData), nil
	}

	return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}
