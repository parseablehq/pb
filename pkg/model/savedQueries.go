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
	"os/exec"
	"strings"
	"time"

	"pb/pkg/config"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	applyQueryButton  = "a"
	deleteQueryButton = "d"
	confirmDelete     = "y"
	cancelDelete      = "n"
)

var (
	docStyle              = lipgloss.NewStyle().Margin(1, 2, 3)
	deleteSavedQueryState = false
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
	confirmModal      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "16", Dark: "226"})
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
	if deleteSavedQueryState {
		return []key.Binding{
			key.NewBinding(
				key.WithKeys(confirmDelete),
				key.WithHelp(confirmDelete, confirmModal.Render("confirm delete")),
			),
			key.NewBinding(
				key.WithKeys(cancelDelete),
				key.WithHelp(cancelDelete, confirmModal.Render("cancel delete")),
			),
		}
	}
	return []key.Binding{
		key.NewBinding(
			key.WithKeys(applyQueryButton),
			key.WithHelp(applyQueryButton, "apply"),
		),
		key.NewBinding(
			key.WithKeys(deleteQueryButton),
			key.WithHelp(deleteQueryButton, "delete"),
		),
	}
}

// FullHelp returns the extended list of keybindings.
func (d itemDelegate) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{
			key.NewBinding(
				key.WithKeys(applyQueryButton),
				key.WithHelp(applyQueryButton, "apply"),
			),
			key.NewBinding(
				key.WithKeys(deleteQueryButton),
				key.WithHelp(deleteQueryButton, "delete"),
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
}

func (m modelSavedQueries) Init() tea.Cmd {
	return nil
}

// Define a message type for command results
type commandResultMsg string

// RunCommand executes a command based on the selected item
func RunCommand(item Item) (string, error) {
	// Clean the description by removing any backslashes
	cleaned := strings.ReplaceAll(item.desc, "\\", "") // Remove any backslashes
	cleaned = strings.TrimSpace(cleaned)               // Trim any leading/trailing whitespace
	cleanedStr := strings.ReplaceAll(cleaned, `"`, "")

	// Prepare the command with the cleaned SQL query
	fmt.Printf("Executing command: pb query run %s\n", cleanedStr) // Log the command for debugging
	//qExecuting command: pb query run "SELECT * FROM "frontend" LIMIT 1000"
	// Execute the command without adding extra quotes
	// TODO: add timestamp
	cmd := exec.Command("pb", "query", "run", cleanedStr) // Directly pass cleaned

	// Set up pipes to capture stdout and stderr
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output // Capture both stdout and stderr in the same buffer

	// Run the command
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("error executing command: %v, output: %s", err, output.String())
	}

	// Log the raw output for debugging
	fmt.Printf("Raw output: %s\n", output.String())

	// Format the output as pretty-printed JSON
	var jsonResponse interface{}
	if err := json.Unmarshal(output.Bytes(), &jsonResponse); err != nil {
		return "", fmt.Errorf("invalid JSON output: %s, error: %v", output.String(), err)
	}

	prettyOutput, err := json.MarshalIndent(jsonResponse, "", "  ")
	if err != nil {
		return "", fmt.Errorf("error formatting JSON output: %v", err)
	}

	// Return the output as a string
	return string(prettyOutput), nil
}

func (m modelSavedQueries) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if msg.String() == "a" || msg.Type == tea.KeyEnter {
			selectedQueryApply = m.list.SelectedItem().(Item)

			// Run the command and capture the output in a function
			cmd := func() tea.Msg {
				output, err := RunCommand(selectedQueryApply)
				if err != nil {
					return commandResultMsg(fmt.Sprintf("Error: %s", err))
				}
				return commandResultMsg(output)
			}

			return m, cmd
		}
		if msg.String() == "d" {
			deleteSavedQueryState = true
			return m, nil
		}
		if msg.String() != "d" {
			deleteSavedQueryState = false
		}
		if msg.String() == "y" {
			selectedQueryDelete = m.list.SelectedItem().(Item)
			return m, tea.Quit
		}
		if msg.String() == "n" {
			deleteSavedQueryState = false
			return m, nil
		}
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)

	// Handle the command result
	case commandResultMsg:
		m.commandOutput = string(msg) // Set the output in the model for display
		return m, nil
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m modelSavedQueries) View() string {
	if m.commandOutput != "" {
		return fmt.Sprintf("Command output:%s\nPress any key to go back.", m.commandOutput)
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

// // Convert a saved query to executable pb query
// func savedQueryToPbQuery(query string, start string, end string) string {
// 	var timeStamps string
// 	if start == "" || end == "" {
// 		timeStamps = ``
// 	} else {
// 		startFormatted := formatToRFC3339(start)
// 		endFormatted := formatToRFC3339(end)
// 		timeStamps = ` --from=` + startFormatted + ` --to=` + endFormatted
// 	}
// 	queryTemplate := "pb query run" + query + timeStamps
// 	fmt.Printf("\nCopy and paste the command")
// 	fmt.Printf("%s", queryTemplate)
// 	return queryTemplate
// }

// // Parses all UTC time format from string to time interface
// func parseTimeToFormat(input string) (time.Time, error) {
// 	// List of possible formats
// 	formats := []string{
// 		time.RFC3339,
// 		"2006-01-02 15:04:05",
// 		"2006-01-02",
// 		"01/02/2006 15:04:05",
// 		"02-Jan-2006 15:04:05 MST",
// 		"2006-01-02T15:04:05Z",
// 		"02-Jan-2006",
// 	}

// 	var err error
// 	var t time.Time

// 	for _, format := range formats {
// 		t, err = time.Parse(format, input)
// 		if err == nil {
// 			return t, nil
// 		}
// 	}

// 	return t, fmt.Errorf("unable to parse time: %s", input)
// }

// // Converts to RFC3339
// func convertTime(input string) (string, error) {
// 	t, err := parseTimeToFormat(input)
// 	if err != nil {
// 		return "", err
// 	}

// 	return t.Format(time.RFC3339), nil
// }

// // Converts User inputted time to string type RFC3339 time
// func formatToRFC3339(time string) string {
// 	var formattedTime string
// 	if len(strings.Fields(time)) > 1 {
// 		newTime := strings.Fields(time)[0:2]
// 		rfc39990time, err := convertTime(strings.Join(newTime, " "))
// 		if err != nil {
// 			fmt.Println("error formatting time")
// 		}
// 		formattedTime = rfc39990time
// 	} else {
// 		rfc39990time, err := convertTime(time)
// 		if err != nil {
// 			fmt.Println("error formatting time")
// 		}
// 		formattedTime = rfc39990time
// 	}
// 	return formattedTime
// }

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

		//var userSavedQuery Item

		queryBytes, _ := json.Marshal(filter.Query.FilterQuery)

		fmt.Println(string(queryBytes))
		// Extract "from" and "to" from time_filter
		// var from, to string
		// if fromValue, exists := filter.TimeFilter["from"]; exists {
		// 	from = fmt.Sprintf("%v", fromValue)
		// }
		// if toValue, exists := filter.TimeFilter["to"]; exists {
		// 	to = fmt.Sprintf("%v", toValue)
		// }
		// filtering only SQL type filters..  **Filter_name is title and Stream Name is desc
		//if string(queryBytes) != "null" {
		userSavedQuery := Item{
			id:     filter.FilterID,
			title:  filter.FilterName,
			stream: filter.StreamName,
			desc:   string(queryBytes),
			from:   filter.TimeFilter.From,
			to:     filter.TimeFilter.To,
		}
		userSavedQueries = append(userSavedQueries, userSavedQuery)
		//}
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
