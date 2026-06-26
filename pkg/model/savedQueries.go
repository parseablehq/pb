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

	"github.com/parseablehq/pb/pkg/config"
	internalHTTP "github.com/parseablehq/pb/pkg/http"
	"github.com/parseablehq/pb/pkg/ui"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	applyQueryButton = "a"
	backButton       = "b"
	confirmDelete    = "y"
	cancelDelete     = "n"
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

type itemDelegate struct{}

func (d itemDelegate) Height() int                             { return 3 }
func (d itemDelegate) Spacing() int                            { return 1 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(Item)
	if !ok {
		return
	}
	p := ui.Active

	titleFg := lipgloss.NewStyle().Foreground(p.Body).Bold(true)
	queryFg := lipgloss.NewStyle().Foreground(p.Faint)
	metaFg := lipgloss.NewStyle().Foreground(p.Faint)
	prefix := "    "

	if index == m.Index() {
		rail := lipgloss.NewStyle().Background(p.Active).Render(" ")
		prefix = rail + "   "
		titleFg = lipgloss.NewStyle().Foreground(p.Active).Bold(true)
	}

	// Truncate to the list's allocated row width so long SELECTs
	// don't wrap and push the right border off-screen. Reserve 4
	// cells for the leading prefix.
	maxW := m.Width() - 4
	if maxW < 10 {
		maxW = 10
	}
	clip := func(s string) string {
		if len(s) <= maxW {
			return s
		}
		if maxW <= 3 {
			return s[:maxW]
		}
		return s[:maxW-1] + "…"
	}

	titleRow := prefix + titleFg.Render(clip(i.title))
	rows := titleRow

	desc := strings.TrimSpace(i.desc)
	if desc == "" {
		desc = "(empty query)"
	}
	rows += "\n    " + queryFg.Render(clip(desc))

	if i.from != "" || i.to != "" {
		meta := fmt.Sprintf("from %s · to %s", i.from, i.to)
		rows += "\n    " + metaFg.Render(clip(meta))
	}
	fmt.Fprint(w, rows)
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
	list   list.Model
	width  int
	height int
}

func (m modelSavedQueries) Init() tea.Cmd {
	return nil
}

func (m modelSavedQueries) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "b":
			return m, tea.Quit

		case "a", "enter":
			if item, ok := m.list.SelectedItem().(Item); ok {
				selectedQueryApply = item
			}
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		bodyW := msg.Width - 6
		bodyH := msg.Height - 9
		if bodyW < 20 {
			bodyW = 20
		}
		if bodyH < 5 {
			bodyH = 5
		}
		m.list.SetSize(bodyW, bodyH)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m modelSavedQueries) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	p := ui.Active

	titleStyle := lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	keyStyle := lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(p.Faint)

	mainInner := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("SAVED QUERIES"),
		"",
		m.list.View(),
	)
	mainCard := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(p.Border).
		Padding(1, 2).
		Width(m.width - 2).
		Render(mainInner)

	hintRow := keyStyle.Render("<a/enter>") + hintStyle.Render(" apply    ") +
		keyStyle.Render("<↑/↓>") + hintStyle.Render(" navigate    ") +
		keyStyle.Render("<→/←>") + hintStyle.Render(" pages    ") +
		keyStyle.Render("<b/ctrl-c>") + hintStyle.Render(" quit")
	footerInner := lipgloss.NewStyle().
		Width(m.width-2).
		Padding(0, 1).
		Render(hintRow)
	footer := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(p.Border).
		Render(footerInner)

	return lipgloss.JoinVertical(lipgloss.Left, mainCard, footer)
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
	m.list.SetShowTitle(false)
	m.list.SetShowStatusBar(false)
	m.list.SetShowHelp(false)
	m.list.SetShowPagination(true)

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

	internalHTTP.AddAuthHeaders(req, profile)
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
		userSavedQuery := Item{
			id:     filter.FilterID,
			title:  filter.FilterName,
			stream: filter.StreamName,
			desc:   *filter.Query.FilterQuery,
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
	internalHTTP.AddAuthHeaders(req, profile)
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
