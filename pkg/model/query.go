// Copyright (c) 2023 Cloudnatively Services Pvt Ltd
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
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"time"

	"pb/pkg/config"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	table "github.com/evertras/bubble-table/table"
	"golang.org/x/exp/slices"
	"golang.org/x/term"
)

const (
	datetimeWidth = 26
	datetimeKey   = "p_timestamp"
	tagKey        = "p_tags"
	metadataKey   = "p_metadata"
)

var (
	FocusPrimary  = lipgloss.AdaptiveColor{Light: "16", Dark: "226"}
	FocusSecondry = lipgloss.AdaptiveColor{Light: "18", Dark: "220"}

	StandardPrimary   = lipgloss.AdaptiveColor{Light: "235", Dark: "255"}
	StandardSecondary = lipgloss.AdaptiveColor{Light: "238", Dark: "254"}

	borderedStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder(), true).
			BorderForeground(StandardPrimary).
			Padding(0)

	borderedFocusStyle = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder(), true).
				BorderForeground(FocusPrimary).
				Padding(0)

	baseStyle               = lipgloss.NewStyle().BorderForeground(StandardPrimary)
	baseBoldUnderlinedStyle = lipgloss.NewStyle().BorderForeground(StandardPrimary).Bold(true)
	headerStyle             = lipgloss.NewStyle().Inherit(baseStyle).Foreground(FocusSecondry).Bold(true)
	tableStyle              = lipgloss.NewStyle().Inherit(baseStyle).Align(lipgloss.Left)

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

	additionalKeyBinds = []key.Binding{
		key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl r", "(re) run query")),
	}

	QueryNavigationMap = []string{"query", "time", "table"}
)

type (
	Mode        int
	FetchResult int
)

type FetchData struct {
	status FetchResult
	schema []string
	data   []map[string]interface{}
}

const (
	FetchOk FetchResult = iota
	FetchErr
)

const (
	OverlayNone uint = iota
	OverlayInputs
)

type QueryModel struct {
	width     int
	height    int
	table     table.Model
	query     textarea.Model
	timeRange TimeInputModel
	profile   config.Profile
	help      help.Model
	status    StatusBar
	overlay   uint
	focused   int
}

func (m *QueryModel) focusSelected() {
	m.query.Blur()
	m.table.Focused(false)

	switch m.currentFocus() {
	case "query":
		m.query.Focus()
	case "table":
		m.table.Focused(true)
	}
}

func (m *QueryModel) currentFocus() string {
	return QueryNavigationMap[m.focused]
}

func NewQueryModel(profile config.Profile, stream string, duration uint) QueryModel {
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))

	inputs := NewTimeInputModel(duration)

	columns := []table.Column{
		table.NewColumn("Id", "Id", 5),
	}

	rows := make([]table.Row, 0)

	table := table.New(columns).
		WithRows(rows).
		Filtered(true).
		HeaderStyle(headerStyle).
		SelectableRows(false).
		Border(customBorder).
		Focused(true).
		WithKeyMap(tableKeyBinds).
		WithPageSize(30).
		WithBaseStyle(tableStyle).
		WithMissingDataIndicatorStyled(table.StyledCell{
			Style: lipgloss.NewStyle().Foreground(StandardSecondary),
			Data:  "╌",
		}).WithMaxTotalWidth(100)

	query := textarea.New()
	query.MaxHeight = 0
	query.MaxWidth = 0
	query.SetHeight(2)
	query.SetWidth(70)
	query.ShowLineNumbers = true
	query.SetValue(fmt.Sprintf("select * from %s", stream))
	query.KeyMap = textAreaKeyMap
	query.Focus()

	help := help.New()
	help.Styles.FullDesc = lipgloss.NewStyle().Foreground(FocusSecondry)

	return QueryModel{
		width:     w,
		height:    h,
		table:     table,
		query:     query,
		timeRange: inputs,
		overlay:   OverlayNone,
		profile:   profile,
		help:      help,
		status:    NewStatusBar(profile.URL, stream, w),
	}
}

func (m QueryModel) Init() tea.Cmd {
	// Just return `nil`, which means "no I/O right now, please."
	return NewFetchTask(m.profile, m.query.Value(), m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc())
}

func (m QueryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height, _ = term.GetSize(int(os.Stdout.Fd()))
		m.help.Width = m.width
		m.status.width = m.width
		m.table = m.table.WithMaxTotalWidth(m.width)
		// width adjustment for time widget
		m.query.SetWidth(int(m.width - 41))
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
		// special behavior on main page
		if m.overlay == OverlayNone {
			if msg.Type == tea.KeyEnter && m.currentFocus() == "time" {
				m.overlay = OverlayInputs
				return m, nil
			}

			if msg.Type == tea.KeyTab {
				m.focused++
				if m.focused > len(QueryNavigationMap)-1 {
					m.focused = 0
				}
				m.focusSelected()
				return m, nil
			}
		}

		// special behavior on time input page
		if m.overlay == OverlayInputs {
			if msg.Type == tea.KeyEnter {
				m.overlay = OverlayNone
				m.focusSelected()
				return m, nil
			}
		}

		// common keybind
		if msg.Type == tea.KeyCtrlR {
			m.overlay = OverlayNone
			return m, NewFetchTask(m.profile, m.query.Value(), m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc())
		}

		switch msg.Type {
		// These keys should exit the program.
		case tea.KeyCtrlC:
			return m, tea.Quit
		default:
			switch m.overlay {
			case OverlayNone:
				switch m.currentFocus() {
				case "query":
					m.query, cmd = m.query.Update(msg)
				case "table":
					m.table, cmd = m.table.Update(msg)
				}
				cmds = append(cmds, cmd)
			case OverlayInputs:
				m.timeRange, cmd = m.timeRange.Update(msg)
				cmds = append(cmds, cmd)
			}
		}
	}
	return m, tea.Batch(cmds...)
}

func (m QueryModel) View() string {
	outer := lipgloss.NewStyle().Inherit(baseStyle).
		UnsetMaxHeight().Width(m.width).Height(m.height)

	m.table = m.table.WithMaxTotalWidth(m.width - 2)

	var mainView string
	var helpKeys [][]key.Binding
	var helpView string

	statusView := lipgloss.PlaceVertical(2, lipgloss.Bottom, m.status.View())
	statusHeight := lipgloss.Height(statusView)

	time := lipgloss.JoinVertical(
		lipgloss.Left,
		fmt.Sprintf("%s %s ", baseBoldUnderlinedStyle.Render(" start "), m.timeRange.start.Value()),
		fmt.Sprintf("%s %s ", baseBoldUnderlinedStyle.Render("  end  "), m.timeRange.end.Value()),
	)

	queryOuter, timeOuter := &borderedStyle, &borderedStyle
	tableOuter := lipgloss.NewStyle()

	switch m.currentFocus() {
	case "query":
		queryOuter = &borderedFocusStyle
	case "time":
		timeOuter = &borderedFocusStyle
	case "table":
		tableOuter = tableOuter.Border(lipgloss.DoubleBorder(), false, false, false, true).
			BorderForeground(FocusPrimary)
	}

	switch m.overlay {
	case OverlayNone:
		mainView = lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.JoinHorizontal(lipgloss.Top, queryOuter.Render(m.query.View()), timeOuter.Render(time)),
			tableOuter.Render(m.table.View()),
		)
		switch m.currentFocus() {
		case "query":
			helpKeys = TextAreaHelpKeys{}.FullHelp()
		case "time":
			helpKeys = [][]key.Binding{
				{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select timeRange"))},
			}
		case "table":
			helpKeys = tableHelpBinds.FullHelp()
		}
	case OverlayInputs:
		mainView = m.timeRange.View()
		helpKeys = m.timeRange.FullHelp()
	}
	helpKeys = append(helpKeys, additionalKeyBinds)
	helpView = m.help.FullHelpView(helpKeys)

	helpHeight := lipgloss.Height(helpView)
	tableBoxHeight := m.height - statusHeight - helpHeight
	render := fmt.Sprintf(
		"%s\n%s\n%s",
		lipgloss.PlaceVertical(tableBoxHeight, lipgloss.Top, mainView),
		helpView,
		statusView)

	return outer.Render(render)
}

type QueryData struct {
	Fields  []string                 `json:"fields"`
	Records []map[string]interface{} `json:"records"`
}

func NewFetchTask(profile config.Profile, query string, start_time string, end_time string) func() tea.Msg {
	return func() tea.Msg {
		res := FetchData{
			status: FetchErr,
			schema: []string{},
			data:   []map[string]interface{}{},
		}

		client := &http.Client{
			Timeout: time.Second * 50,
		}

		data, status := fetchData(client, &profile, query, start_time, end_time)
		if status == FetchErr {
			return res
		} else {
			res.data = data.Records
			res.schema = data.Fields
		}

		res.status = FetchOk

		return res
	}
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

	endpoint := fmt.Sprintf("%s/%s", profile.URL, "api/v1/query?fields=true")
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
	// pin p_timestamp to left if available
	contains_timestamp := slices.Contains(data.schema, datetimeKey)
	contains_tags := slices.Contains(data.schema, tagKey)
	contains_metadata := slices.Contains(data.schema, metadataKey)
	columns := make([]table.Column, len(data.schema))
	columnIndex := 0

	if contains_timestamp {
		columns[0] = table.NewColumn(datetimeKey, datetimeKey, datetimeWidth)
		columnIndex++
	}

	if contains_tags {
		columns[len(columns)-2] = table.NewColumn(tagKey, tagKey, inferWidthForColumns(tagKey, &data.data, 100, 80)).WithFiltered(true)
	}

	if contains_metadata {
		columns[len(columns)-1] = table.NewColumn(metadataKey, metadataKey, inferWidthForColumns(metadataKey, &data.data, 100, 80)).WithFiltered(true)
	}

	for _, title := range data.schema {
		switch title {
		case datetimeKey, tagKey, metadataKey:
			continue
		default:
			width := inferWidthForColumns(title, &data.data, 100, 100) + 1
			columns[columnIndex] = table.NewColumn(title, title, width).WithFiltered(true)
			columnIndex++
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

	if len(column) > width {
		width = len(column)
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
