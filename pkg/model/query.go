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
	"regexp"
	"strings"
	"sync"
	"time"

	"pb/pkg/config"
	"pb/pkg/iterator"

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

	additionalKeyBinds = []key.Binding{
		key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl r", "(re) run query")),
	}

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

	regex := regexp.MustCompile(`^select\s+(?:\*|\w+(?:,\s*\w+)*)\s+from\s+(\w+)(?:\s+;)?$`)
	matches := regex.FindStringSubmatch(m.query.Value())
	if matches == nil {
		return nil
	}
	table := matches[1]
	iter := iterator.NewQueryIterator(
		startTime, endTime,
		false,
		func(t1, t2 time.Time) (QueryData, FetchResult) {
			client := &http.Client{
				Timeout: time.Second * 50,
			}
			return fetchData(client, &m.profile, m.query.Value(), t1.UTC().Format(time.RFC3339), t2.UTC().Format(time.RFC3339))
		},
		func(t1, t2 time.Time) bool {
			client := &http.Client{
				Timeout: time.Second * 50,
			}
			res, err := fetchData(client, &m.profile, "select count(*) as count from "+table, m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc())
			if err == fetchErr {
				return false
			}
			count := res.Records[0]["count"].(float64)
			return count > 0
		})
	return &iter
}

func NewQueryModel(profile config.Profile, queryStr string, startTime, endTime time.Time) QueryModel {
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))

	inputs := NewTimeInputModel(startTime, endTime)

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
	query.SetValue(queryStr)
	query.KeyMap = textAreaKeyMap
	query.Focus()

	help := help.New()
	help.Styles.FullDesc = lipgloss.NewStyle().Foreground(FocusSecondary)

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
		status:        NewStatusBar(profile.URL, w),
	}
	model.queryIterator = createIteratorFromModel(&model)
	return model
}

func (m QueryModel) Init() tea.Cmd {
	return func() tea.Msg {
		var ready sync.WaitGroup
		ready.Add(1)
		go func() {
			m.initIterator()
			for !m.queryIterator.Ready() {
				time.Sleep(time.Millisecond * 100)
			}
			ready.Done()
		}()
		ready.Wait()
		if m.queryIterator.Finished() {
			return nil
		}

		return IteratorNext(m.queryIterator)()
	}
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
		if msg.status == fetchOk {
			m.UpdateTable(msg)
		} else {
			m.status.Error = "failed to query"
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
				return m, nil
			}
		}

		// common keybind
		if msg.Type == tea.KeyCtrlR {
			m.overlay = overlayNone
			if m.queryIterator == nil {
				return m, NewFetchTask(m.profile, m.query.Value(), m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc())
			}
			if m.queryIterator.Ready() && !m.queryIterator.Finished() {
				return m, IteratorNext(m.queryIterator)
			}
			return m, nil
		}

		if msg.Type == tea.KeyCtrlB {
			m.overlay = overlayNone
			if m.queryIterator.CanFetchPrev() {
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
					m.initIterator()
				case "table":
					m.table, cmd = m.table.Update(msg)
				}
				cmds = append(cmds, cmd)
			case overlayInputs:
				m.timeRange, cmd = m.timeRange.Update(msg)
				m.initIterator()
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

	mainViewRenderElements := []string{lipgloss.JoinHorizontal(lipgloss.Top, queryOuter.Render(m.query.View()), timeOuter.Render(time)), tableOuter.Render(m.table.View())}

	if m.queryIterator != nil {
		inactiveStyle := lipgloss.NewStyle().Foreground(StandardPrimary)
		activeStyle := lipgloss.NewStyle().Foreground(FocusPrimary)
		var line strings.Builder

		if m.queryIterator.CanFetchPrev() {
			line.WriteString(activeStyle.Render("<<"))
		} else {
			line.WriteString(inactiveStyle.Render("<<"))
		}

		fmt.Fprintf(&line, " %d of many ", m.table.TotalRows())

		if m.queryIterator.Ready() && !m.queryIterator.Finished() {
			line.WriteString(activeStyle.Render(">>"))
		} else {
			line.WriteString(inactiveStyle.Render(">>"))
		}

		mainViewRenderElements = append(mainViewRenderElements, line.String())
	}

	switch m.overlay {
	case overlayNone:
		mainView = lipgloss.JoinVertical(lipgloss.Left, mainViewRenderElements...)
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
	case overlayInputs:
		mainView = m.timeRange.View()
		helpKeys = m.timeRange.FullHelp()
	}

	if m.queryIterator != nil {
		helpKeys = append(helpKeys, paginatorKeyBinds)
	} else {
		helpKeys = append(helpKeys, additionalKeyBinds)
	}

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

func NewFetchTask(profile config.Profile, query string, startTime string, endTime string) func() tea.Msg {
	return func() tea.Msg {
		res := FetchData{
			status: fetchErr,
			schema: []string{},
			data:   []map[string]interface{}{},
		}

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

func IteratorNext(iter *iterator.QueryIterator[QueryData, FetchResult]) func() tea.Msg {
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

func IteratorPrev(iter *iterator.QueryIterator[QueryData, FetchResult]) func() tea.Msg {
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

	queryTemplate := `{
    "query": "%s",
    "startTime": "%s",
    "endTime": "%s"
	}
	`

	finalQuery := fmt.Sprintf(queryTemplate, query, startTime, endTime)

	endpoint := fmt.Sprintf("%s/%s", profile.URL, "api/v1/query?fields=true")
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer([]byte(finalQuery)))
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

	res = fetchOk
	return
}

func (m *QueryModel) UpdateTable(data FetchData) {
	// pin p_timestamp to left if available
	containsTimestamp := slices.Contains(data.schema, dateTimeKey)
	containsTags := slices.Contains(data.schema, tagKey)
	containsMetadata := slices.Contains(data.schema, metadataKey)
	columns := make([]table.Column, len(data.schema))
	columnIndex := 0

	if containsTimestamp {
		columns[0] = table.NewColumn(dateTimeKey, dateTimeKey, dateTimeWidth)
		columnIndex++
	}

	if containsTags {
		columns[len(columns)-2] = table.NewColumn(tagKey, tagKey, inferWidthForColumns(tagKey, &data.data, 100, 80)).WithFiltered(true)
	}

	if containsMetadata {
		columns[len(columns)-1] = table.NewColumn(metadataKey, metadataKey, inferWidthForColumns(metadataKey, &data.data, 100, 80)).WithFiltered(true)
	}

	for _, title := range data.schema {
		switch title {
		case dateTimeKey, tagKey, metadataKey:
			continue
		default:
			width := inferWidthForColumns(title, &data.data, 100, 100) + 1
			columns[columnIndex] = table.NewColumn(title, title, width).WithFiltered(true)
			columnIndex++
		}
	}

	rows := make([]table.Row, len(data.data))
	for i := 0; i < len(data.data); i++ {
		rowJSON := data.data[i]
		rows[i] = table.NewRow(rowJSON)
	}

	m.table = m.table.WithColumns(columns)
	m.table = m.table.WithRows(rows)
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
