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

package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"time"

	"pb/pkg/config"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	table "github.com/evertras/bubble-table/table"
	"golang.org/x/term"
)

const datetime_width = 26

var (
	baseStyle   = lipgloss.NewStyle().BorderForeground(lipgloss.AdaptiveColor{Light: "236", Dark: "248"})
	headerStyle = lipgloss.NewStyle().Inherit(baseStyle).Foreground(lipgloss.AdaptiveColor{Light: "#023047", Dark: "#90E0EF"}).Bold(true)
	tableStyle  = lipgloss.NewStyle().Inherit(baseStyle).Align(lipgloss.Left)

	customBorder = table.Border{
		Top:    "─",
		Left:   "│",
		Right:  "│",
		Bottom: "─",

		TopRight:    "╮",
		TopLeft:     "╭",
		BottomRight: "╯",
		BottomLeft:  "╰",

		TopJunction:    "╥",
		LeftJunction:   "├",
		RightJunction:  "┤",
		BottomJunction: "╨",
		InnerJunction:  "╫",

		InnerDivider: "║",
	}
)

type Mode int
type FetchResult int

type FetchData struct {
	status FetchResult
	schema []string
	data   []map[string]interface{}
}

const (
	FetchOk FetchResult = iota
	FetchErr
)

type QueryModel struct {
	width      int
	height     int
	query      string
	time_range timeRangeModel
	table      table.Model
	profile    config.Profile
	stream     string
	help       help.Model
	status     StatusBar
}

func NewQueryModel(profile config.Profile, stream string, duration uint) QueryModel {
	query := fmt.Sprintf("select * from %s", stream)
	var w, h, _ = term.GetSize(int(os.Stdout.Fd()))

	columns := []table.Column{
		table.NewColumn("Id", "Id", 5),
	}

	rows := make([]table.Row, 0)

	keys := table.DefaultKeyMap()
	keys.RowDown.SetKeys("j", "down", "s")
	keys.RowUp.SetKeys("k", "up", "w")

	table := table.New(columns).
		WithRows(rows).
		HeaderStyle(headerStyle).
		SelectableRows(false).
		Border(customBorder).
		Focused(true).
		WithKeyMap(keys).
		WithPageSize(30).
		WithBaseStyle(tableStyle).
		WithMissingDataIndicatorStyled(table.StyledCell{
			Style: lipgloss.NewStyle().Foreground(lipgloss.Color("#faa")),
			Data:  "╌",
		}).WithMaxTotalWidth(100).WithHorizontalFreezeColumnCount(1)

	return QueryModel{
		width:      w,
		height:     h,
		query:      query,
		time_range: NewTimeRangeModel(duration),
		table:      table,
		profile:    profile,
		stream:     stream,
		help:       help.New(),
		status:     NewStatusBar(profile.Url, stream, w),
	}
}

func (m QueryModel) Init() tea.Cmd {
	// Just return `nil`, which means "no I/O right now, please."
	return NewFetchTask(m.profile, m.stream, m.query, m.time_range.StartValueUtc(), m.time_range.EndValueUtc())
}

func (m QueryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height, _ = term.GetSize(int(os.Stdout.Fd()))
		m.help.Width = m.width
		return m, nil

	case FetchData:
		if msg.status == FetchOk {
			m.UpdateTable(msg)
		} else {
			m.status.Error = "failed to query"
		}

		return m, nil

	// Is it a key press?
	case tea.KeyMsg:
		switch msg.Type {
		// These keys should exit the program.
		case tea.KeyCtrlC:
			return m, tea.Quit

		default:
			m.table, cmd = m.table.Update(msg)
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m QueryModel) View() string {
	var outer = lipgloss.NewStyle().Inherit(baseStyle).
		UnsetMaxHeight().Width(m.width).Height(m.height)

	m.table.WithMaxTotalWidth(m.width - 10)

	statusHeight := 1
	tableHeight := m.height - statusHeight - 4
	m.status.width = m.width
	m.help.ShowAll = true
	m.help.Styles.FullDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	help := m.help.View(m.table.KeyMap())

	f, _ := tea.LogToFile("debug.log", help)
	defer f.Close()

	render := fmt.Sprintf("%s\n%s\n%s", lipgloss.PlaceVertical(tableHeight, lipgloss.Top, m.table.View()), help, m.status.View())

	return outer.Render(render)

}

type Field struct {
	Name string
}

type SchemaResp struct {
	Fields []Field
}

type QueryData []map[string]interface{}

func NewFetchTask(profile config.Profile, stream string, query string, start_time string, end_time string) func() tea.Msg {
	return func() tea.Msg {
		res := FetchData{
			status: FetchErr,
			schema: []string{},
			data:   []map[string]interface{}{},
		}

		client := &http.Client{
			Timeout: time.Second * 30,
		}

		fields, status := fetchSchema(client, &profile, stream)
		if status == FetchErr {
			return res
		} else {
			res.schema = fields
		}

		data, status := fetchData(client, &profile, query, start_time, end_time)
		if status == FetchErr {
			return res
		} else {
			res.data = data
		}

		res.status = FetchOk

		return res
	}
}

func fetchSchema(client *http.Client, profile *config.Profile, stream string) (fields []string, res FetchResult) {
	fields = []string{}
	res = FetchErr

	endpoint := fmt.Sprintf("%s/%s", profile.Url, fmt.Sprintf("api/v1/logstream/%s/schema", stream))
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return
	}
	req.SetBasicAuth(profile.Username, profile.Password)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var schema SchemaResp
	json.Unmarshal(body, &schema)
	for _, field := range schema.Fields {
		fields = append(fields, field.Name)
	}

	res = FetchOk
	return
}

func fetchData(client *http.Client, profile *config.Profile, query string, start_time string, end_time string) (data QueryData, res FetchResult) {
	data = QueryData{}
	res = FetchErr

	query_template := `{
    "query": "%s",
    "startTime": "%s",
    "endTime": "%s"
	}
	`

	final_query := fmt.Sprintf(query_template, query, start_time, end_time)

	endpoint := fmt.Sprintf("%s/%s", profile.Url, "api/v1/query")
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer([]byte(final_query)))
	if err != nil {
		return
	}
	req.SetBasicAuth(profile.Username, profile.Password)
	req.Header.Add("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return
	}

	err = json.NewDecoder(resp.Body).Decode(&data)
	defer resp.Body.Close()
	if err != nil {
		return
	}

	res = FetchOk
	return
}

func (m *QueryModel) UpdateTable(data FetchData) {
	columns := make([]table.Column, len(data.schema))
	columns[0] = table.NewColumn("p_timestamp", "p_timestamp", 24)
	columnIndex := 1

	for _, title := range data.schema {
		switch title {
		case "p_timestamp", "p_metadata", "p_tags":
			continue
		default:
			width := inferWidthForColumns(title, &data.data, 100, 80) + 3
			columns[columnIndex] = table.NewColumn(title, title, width)
			columnIndex += 1
		}
	}

	rows := make([]table.Row, len(data.data))
	for i := 0; i < len(data.data); i++ {
		row_json := data.data[i]
		rows[i] = table.NewRow(row_json)
	}

	m.table = m.table.WithColumns(columns)
	m.table = m.table.WithRows(rows)
}

func inferWidthForColumns(column string, data *[]map[string]interface{}, max_records int, max_width int) (width int) {
	width = 2
	records := 0

	if len(*data) < max_records {
		records = len(*data)
	} else {
		records = max_records
	}

	for i := 0; i < records; i++ {
		w := 0
		value, exists := (*data)[i][column]
		if exists {
			switch value := value.(type) {
			case string:
				w = len(value)
			case int:
				w = countDigits(value)
			}
		}

		if w > width {
			if w < max_width {
				width = w
			} else {
				width = max_width
				return
			}
		}
	}

	return
}

func countDigits(num int) int {
	if num == 0 {
		return 1
	}
	// Using logarithm base 10 to calculate the number of digits
	numDigits := int(math.Log10(math.Abs(float64(num)))) + 1
	return numDigits
}
