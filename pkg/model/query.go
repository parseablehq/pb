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
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"pb/pkg/config"
	"pb/pkg/iterator"
	"strings"
	"time"

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
	dateTimeWidth = 26
	dateTimeKey   = "p_timestamp"
	tagKey        = "p_tags"
	metadataKey   = "p_metadata"
)

// Style for this widget
var (
	FocusPrimary   = lipgloss.AdaptiveColor{Light: "16", Dark: "226"}
	FocusSecondary = lipgloss.AdaptiveColor{Light: "18", Dark: "220"}

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
	headerStyle             = lipgloss.NewStyle().Inherit(baseStyle).Foreground(FocusSecondary).Bold(true)
	tableStyle              = lipgloss.NewStyle().Inherit(baseStyle).Align(lipgloss.Left)
)

var (
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

	additionalKeyBinds = []key.Binding{runQueryKey}

	paginatorKeyBinds = []key.Binding{
		key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl r", "Fetch Next Minute")),
		key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl b", "Fetch Prev Minute")),
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
	fetchOk FetchResult = iota
	fetchErr
)

const (
	overlayNone uint = iota
	overlayInputs
)

type QueryModel struct {
	width         int
	height        int
	table         table.Model
	query         textarea.Model
	timeRange     TimeInputModel
	profile       config.Profile
	help          help.Model
	status        StatusBar
	queryIterator *iterator.QueryIterator[QueryData, FetchResult]
	overlay       uint
	focused       int
	dataRows      []table.Row // actual data rows (without padding)
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

func (m *QueryModel) initIterator() {
	iter := createIteratorFromModel(m)
	m.queryIterator = iter
}

func createIteratorFromModel(m *QueryModel) *iterator.QueryIterator[QueryData, FetchResult] {
	startTime := m.timeRange.start.Time()
	endTime := m.timeRange.end.Time()

	startTime = startTime.Truncate(time.Minute)
	endTime = endTime.Truncate(time.Minute).Add(time.Minute)

	table := streamNameFromQuery(m.query.Value())
	if table != "" {
		iter := iterator.NewQueryIterator(
			startTime, endTime,
			false,
			func(t1, t2 time.Time) (QueryData, FetchResult) {
				client := &http.Client{
					Timeout: time.Second * 50,
				}
				return fetchData(client, &m.profile, m.query.Value(), t1.UTC().Format(time.RFC3339), t2.UTC().Format(time.RFC3339))
			},
			func(_, _ time.Time) bool {
				client := &http.Client{
					Timeout: time.Second * 50,
				}
				res, err := fetchData(client, &m.profile, "select count(*) as count from "+table, m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc())
				if err == fetchErr || len(res.Records) == 0 {
					return false
				}
				count, ok := res.Records[0]["count"].(float64)
				return ok && count > 0
			})
		return &iter
	}
	return nil
}

func NewQueryModel(profile config.Profile, queryStr string, startTime, endTime time.Time) QueryModel {
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))

	inputs := NewTimeInputModel(startTime, endTime)

	columns := []table.Column{
		table.NewColumn("Id", "Id", 5),
	}

	rows := make([]table.Row, 0)

	pageSize := h - 14 // header(4) + help(4) + status(1) + table-overhead(6) = 15; -1 buffer
	if pageSize < 5 {
		pageSize = 5
	}

	table := table.New(columns).
		WithRows(rows).
		Filtered(true).
		HeaderStyle(headerStyle).
		SelectableRows(false).
		Border(customBorder).
		Focused(true).
		WithKeyMap(tableKeyBinds).
		WithPageSize(pageSize).
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
	query.SetValue(queryStr)
	query.KeyMap = textAreaKeyMap
	query.Focus()

	help := help.New()
	help.Styles.FullDesc = lipgloss.NewStyle().Foreground(FocusSecondary)

	status := NewStatusBar(profile.URL, w)
	status.Info = "fetching..."

	model := QueryModel{
		width:         w,
		height:        h,
		table:         table,
		query:         query,
		timeRange:     inputs,
		overlay:       overlayNone,
		profile:       profile,
		help:          help,
		queryIterator: nil,
		status:        status,
	}
	return model
}

func (m QueryModel) Init() tea.Cmd {
	return NewFetchTask(m.profile, m.query.Value(), m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc())
}

func (m QueryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = m.width
		m.status.width = m.width
		m.table = m.table.WithMaxTotalWidth(m.width)
		m.query.SetWidth(int(m.width - 41))
		return m, nil

	case FetchData:
		m.status.Info = ""
		if msg.status == fetchOk {
			m.UpdateTable(msg)
			m.status.Error = ""
			m.status.Info = fmt.Sprintf("%d rows", len(m.dataRows))
		} else {
			m.status.Error = "query failed"
		}
		return m, nil

		// Is it a key press?
	case tea.KeyMsg:
		// special behavior on main page
		if m.overlay == overlayNone {
			if msg.Type == tea.KeyEnter && m.currentFocus() == "time" {
				m.overlay = overlayInputs
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
		if m.overlay == overlayInputs {
			if msg.Type == tea.KeyEnter {
				m.overlay = overlayNone
				m.focusSelected()
				m.status.Error = ""
				m.status.Info = "fetching..."
				return m, NewFetchTask(m.profile, m.query.Value(), m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc())
			}
		}

		// common keybind
		if msg.Type == tea.KeyCtrlR {
			m.overlay = overlayNone
			m.status.Error = ""
			m.status.Info = "fetching..."
			return m, NewFetchTask(m.profile, m.query.Value(), m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc())
		}

		if msg.Type == tea.KeyCtrlB {
			m.overlay = overlayNone
			if m.queryIterator != nil && m.queryIterator.CanFetchPrev() {
				return m, IteratorPrev(m.queryIterator)
			}
			return m, nil
		}

		switch msg.Type {
		// These keys should exit the program.
		case tea.KeyCtrlC:
			return m, tea.Quit
		default:
			switch m.overlay {
			case overlayNone:
				switch m.currentFocus() {
				case "query":
					m.query, cmd = m.query.Update(msg)
				case "table":
					m.table, cmd = m.table.Update(msg)
				}
				cmds = append(cmds, cmd)
			case overlayInputs:
				m.timeRange, cmd = m.timeRange.Update(msg)
				cmds = append(cmds, cmd)
			}
		}
	}
	return m, tea.Batch(cmds...)
}

func (m QueryModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	// Step 1: build the fixed-height components and measure them.
	timePane := lipgloss.JoinVertical(
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

	header := lipgloss.JoinHorizontal(lipgloss.Top,
		queryOuter.Render(m.query.View()),
		timeOuter.Render(timePane),
	)
	headerHeight := lipgloss.Height(header)

	statusView := m.status.View()
	statusHeight := lipgloss.Height(statusView)

	// Step 2: build help view and measure it.
	var helpKeys [][]key.Binding
	switch m.overlay {
	case overlayNone:
		switch m.currentFocus() {
		case "query":
			helpKeys = TextAreaHelpKeys{}.FullHelp()
		case "time":
			helpKeys = [][]key.Binding{
				{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select timeRange"))},
			}
			helpKeys = append(helpKeys, additionalKeyBinds)
		case "table":
			helpKeys = tableHelpBinds.FullHelp()
			helpKeys = append(helpKeys, additionalKeyBinds)
		}
	case overlayInputs:
		helpKeys = m.timeRange.FullHelp()
		helpKeys = append(helpKeys, additionalKeyBinds)
	}
	helpView := m.help.FullHelpView(helpKeys)
	helpHeight := lipgloss.Height(helpView)

	// Step 3: calculate exact table page size so everything fits.
	tableAvail := m.height - headerHeight - helpHeight - statusHeight
	pageSize := tableAvail - 6
	if pageSize < 1 {
		pageSize = 1
	}

	// Pad rows to pageSize so the table always fills its allocated height.
	// Empty rows render as blank lines inside the table border.
	displayRows := make([]table.Row, pageSize)
	copy(displayRows, m.dataRows)

	m.table = m.table.WithPageSize(pageSize).WithRows(displayRows)

	// Step 4: compose main view.
	var mainView string
	switch m.overlay {
	case overlayNone:
		mainView = lipgloss.JoinVertical(lipgloss.Left, header, tableOuter.Render(m.table.View()))
	case overlayInputs:
		mainView = m.timeRange.View()
	}

	render := lipgloss.JoinVertical(lipgloss.Left, mainView, helpView, statusView)
	return lipgloss.NewStyle().Width(m.width).Render(render)
}

type QueryData struct {
	Fields  []string                 `json:"fields"`
	Records []map[string]interface{} `json:"records"`
}

func NewFetchTask(profile config.Profile, query string, startTime string, endTime string) tea.Cmd {
	return func() (msg tea.Msg) {
		res := FetchData{
			status: fetchErr,
			schema: []string{},
			data:   []map[string]interface{}{},
		}
		defer func() {
			if r := recover(); r != nil {
				msg = res 
			}
		}()

		client := &http.Client{
			Timeout: time.Second * 50,
		}

		data, status := fetchData(client, &profile, query, startTime, endTime)

		if status == fetchOk {
			res.data = data.Records
			res.schema = data.Fields
			res.status = fetchOk
		}

		return res
	}
}

func IteratorNext(iter *iterator.QueryIterator[QueryData, FetchResult]) tea.Cmd {
	return func() tea.Msg {
		res := FetchData{
			status: fetchErr,
			schema: []string{},
			data:   []map[string]interface{}{},
		}

		data, status := iter.Next()

		if status == fetchOk {
			res.data = data.Records
			res.schema = data.Fields
			res.status = fetchOk
		}

		return res
	}
}

func IteratorPrev(iter *iterator.QueryIterator[QueryData, FetchResult]) tea.Cmd {
	return func() tea.Msg {
		res := FetchData{
			status: fetchErr,
			schema: []string{},
			data:   []map[string]interface{}{},
		}

		data, status := iter.Prev()

		if status == fetchOk {
			res.data = data.Records
			res.schema = data.Fields
			res.status = fetchOk
		}

		return res
	}
}

func fetchData(client *http.Client, profile *config.Profile, query string, startTime string, endTime string) (data QueryData, res FetchResult) {
	data = QueryData{}
	res = fetchErr

	body, err := json.Marshal(map[string]string{
		"query":     query,
		"startTime": startTime,
		"endTime":   endTime,
	})
	if err != nil {
		return
	}

	endpoint := fmt.Sprintf("%s/%s", profile.URL, "api/v1/query?fields=true")
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(body))
	if err != nil {
		return
	}
	if profile.Token != "" {
		req.Header.Set("Authorization", "Bearer "+profile.Token)
	} else {
		req.SetBasicAuth(profile.Username, profile.Password)
	}
	req.Header.Add("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return
	}

	res = fetchOk
	return
}

type colSpec struct {
	key        string
	title      string
	width      int
	filterable bool
	fixed      bool // fixed-width columns are not scaled down
}

func (m *QueryModel) UpdateTable(data FetchData) {
	if len(data.schema) == 0 {
		return
	}

	// Build column specs: timestamp pinned left, p_tags/p_metadata pinned right.
	var specs []colSpec

	if slices.Contains(data.schema, dateTimeKey) {
		specs = append(specs, colSpec{key: dateTimeKey, title: dateTimeKey, width: dateTimeWidth, fixed: true})
	}

	for _, title := range data.schema {
		switch title {
		case dateTimeKey, tagKey, metadataKey:
			continue
		default:
			w := inferWidthForColumns(title, &data.data, 100, 100) + 1
			specs = append(specs, colSpec{key: title, title: title, width: w, filterable: true})
		}
	}

	if slices.Contains(data.schema, tagKey) {
		specs = append(specs, colSpec{key: tagKey, title: tagKey, width: inferWidthForColumns(tagKey, &data.data, 100, 80), filterable: true})
	}

	if slices.Contains(data.schema, metadataKey) {
		specs = append(specs, colSpec{key: metadataKey, title: metadataKey, width: inferWidthForColumns(metadataKey, &data.data, 100, 80), filterable: true})
	}

	// Scale scalable column widths so the total table fits within the terminal.
	// Only scale when each column would still be at least minReadableWidth wide —
	// when there are too many columns (e.g. 50+), skip scaling so the first N
	// columns stay readable and > handles the rest via horizontal scroll.
	if m.width > 0 && len(specs) > 0 {
		const minReadableWidth = 8

		numBorders := len(specs) + 1
		available := m.width - numBorders

		totalWidth, fixedWidth := 0, 0
		for _, s := range specs {
			totalWidth += s.width
			if s.fixed {
				fixedWidth += s.width
			}
		}

		if totalWidth > available {
			scalableAvail := available - fixedWidth
			scalableTotal := totalWidth - fixedWidth
			numScalable := 0
			for _, s := range specs {
				if !s.fixed {
					numScalable++
				}
			}
			if scalableTotal > 0 && scalableAvail > 0 && numScalable > 0 &&
				scalableAvail/numScalable >= minReadableWidth {
				for i := range specs {
					if !specs[i].fixed {
						newW := specs[i].width * scalableAvail / scalableTotal
						if newW < minReadableWidth {
							newW = minReadableWidth
						}
						specs[i].width = newW
					}
				}
			}
		}
	}

	// Build table.Columns from scaled specs.
	columns := make([]table.Column, 0, len(specs))
	for _, s := range specs {
		col := table.NewColumn(s.key, s.title, s.width)
		if s.filterable {
			col = col.WithFiltered(true)
		}
		columns = append(columns, col)
	}

	m.dataRows = make([]table.Row, len(data.data))
	for i, rowJSON := range data.data {
		m.dataRows[i] = table.NewRow(rowJSON)
	}

	m.table = m.table.WithColumns(columns)
	m.table = m.table.WithRows(m.dataRows)
}

func inferWidthForColumns(column string, data *[]map[string]interface{}, maxRecords int, maxWidth int) (width int) {
	width = 2
	records := 0

	if len(*data) < maxRecords {
		records = len(*data)
	} else {
		records = maxRecords
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
			if w < maxWidth {
				width = w
			} else {
				width = maxWidth
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

func streamNameFromQuery(query string) string {
	stream := ""
	tokens := strings.Split(query, " ")
	for i, token := range tokens {
		if token == "from" {
			stream = tokens[i+1]
			break
		}
	}
	return stream
}
