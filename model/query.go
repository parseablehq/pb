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

	"cli/config"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	table "github.com/evertras/bubble-table/table"
	"golang.org/x/term"
)

const datetime_width = 26

var (
	backgroundColor = lipgloss.Color("#050F10")
	baseStyle       = lipgloss.NewStyle().
			Background(backgroundColor).
			MarginBackground(backgroundColor).
			BorderBackground(backgroundColor)
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

var navigation_map [][]string = [][]string{
	{"query", "time", "execute"},
	{"table"},
}

type Mode int
type FetchResult int

type FetchData struct {
	status FetchResult
	schema []string
	data   []map[string]interface{}
}

const (
	navigation Mode = iota
	active
	inactive
)

const (
	FetchOk FetchResult = iota
	FetchErr
)

type QueryModel struct {
	width      int
	height     int
	query      textarea.Model
	time_range timeRangeModel
	table      table.Model
	focus      struct {
		x uint
		y uint
	}
	mode    Mode
	profile config.Profile
	stream  string
}

func NewQueryModel(profile config.Profile, stream string) QueryModel {
	query := textarea.New()
	query.ShowLineNumbers = false
	query.SetHeight(1)
	query.SetWidth(50)

	query.Placeholder = "select * from app"
	query.InsertString("select * from app")
	query.Focus()

	var w, h, _ = term.GetSize(int(os.Stdout.Fd()))

	currentTime := time.Now().UTC()
	startTime := currentTime.Add(-5 * time.Minute)
	time_start := DateInput()
	time_start.SetValue(startTime.Format(time.RFC3339))
	time_end := DateInput()
	time_end.SetValue(currentTime.Format(time.RFC3339))

	columns := []table.Column{
		table.NewColumn("Id", "Id", 5),
	}

	rows := make([]table.Row, 0)

	keys := table.DefaultKeyMap()
	keys.RowDown.SetKeys("j", "down", "s")
	keys.RowUp.SetKeys("k", "up", "w")

	table := table.New(columns).
		WithRows(rows).
		HeaderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)).
		SelectableRows(false).
		Border(customBorder).
		Focused(true).
		WithKeyMap(keys).
		WithPageSize(20).
		WithBaseStyle(
			lipgloss.NewStyle().
				Inherit(baseStyle).
				BorderForeground(lipgloss.Color("#a38")).
				Foreground(lipgloss.Color("#aaa")).
				Align(lipgloss.Left),
		).
		WithMissingDataIndicatorStyled(table.StyledCell{
			Style: lipgloss.NewStyle().Foreground(lipgloss.Color("#faa")),
			Data:  "╌",
		}).WithMaxTotalWidth(100).WithHorizontalFreezeColumnCount(1)

	return QueryModel{
		width:      w,
		height:     h,
		query:      query,
		time_range: NewTimeRangeModel(),
		table:      table,
		focus: struct {
			x uint
			y uint
		}{0, 0},
		mode:    navigation,
		profile: profile,
		stream:  stream,
	}
}

func (m *QueryModel) currentFocus() string {
	return navigation_map[m.focus.y][m.focus.x]
}

func (m *QueryModel) Blur() {
	switch m.currentFocus() {
	case "query":
		m.query.Blur()
	case "time":
		m.time_range.Blur()
	case "table":
		m.table.Focused(false)
	default:
		return
	}
}

func (m *QueryModel) Focus(id string) {
	switch id {
	case "query":
		m.query.Focus()
	case "time":
		m.time_range.Focus()
	case "table":
		m.table.Focused(true)
	}
}

func (m *QueryModel) Navigate(key tea.KeyMsg) {
	switch key.String() {
	case "enter":
		m.mode = active
		m.Focus(m.currentFocus())

	case "up", "w":
		if m.focus.y > 0 {
			m.focus.y -= 1
			m.focus.x = 0
		}
	case "down", "s":
		if m.focus.y < uint(len(navigation_map))-1 {
			m.focus.y += 1
			m.focus.x = 0
		}
	case "left", "a":
		if m.focus.x > 0 {
			m.focus.x -= 1
		}
	case "right", "d":
		if m.focus.x < uint(len(navigation_map[m.focus.y]))-1 {
			m.focus.x += 1
		}
	default:
		return
	}
}

func (m QueryModel) HandleKeyPress(key tea.KeyMsg) (QueryModel, tea.Cmd) {
	var cmd tea.Cmd

	if key.Type == tea.KeyEsc && (m.time_range.mode != active) {
		m.mode = navigation
		m.Blur()
		return m, nil
	}

	if m.mode == navigation {
		if key.Type == tea.KeyEnter && m.currentFocus() == "execute" {
			m.mode = inactive
			cmd = NewFetchTask(m.profile, m.stream, m.query.Value(), m.time_range.StartValueUtc(), m.time_range.EndValueUtc())
		} else {
			m.Navigate(key)
		}
	} else {
		focused := navigation_map[m.focus.y][m.focus.x]
		switch focused {
		case "query":
			m.query, cmd = m.query.Update(key)
		case "time":
			m.time_range, cmd = m.time_range.Update(key)
		case "table":
			m.table, cmd = m.table.Update(key)
		}
	}

	return m, cmd
}

func (m QueryModel) Init() tea.Cmd {
	// Just return `nil`, which means "no I/O right now, please."
	return nil
}

func (m QueryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height, _ = term.GetSize(int(os.Stdout.Fd()))
		return m, nil

	case FetchData:
		if msg.status == FetchOk {
			m.UpdateTable(msg)
		}
		m.mode = navigation
		return m, nil

		// Is it a key press?
	case tea.KeyMsg:
		switch msg.Type {
		// These keys should exit the program.
		case tea.KeyCtrlC:
			return m, tea.Quit

		default:
			if m.mode != inactive {
				m, cmd = m.HandleKeyPress(msg)
				cmds = append(cmds, cmd)
			}
		}
	}

	return m, tea.Batch(cmds...)
}

func (m QueryModel) View() string {
	var outer = lipgloss.NewStyle().Inherit(baseStyle).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("228")).
		UnsetMaxWidth().
		UnsetMaxHeight().Width(m.width - 2).Height(m.height - 2)

	var input_style = lipgloss.NewStyle().
		Inherit(baseStyle).
		Border(lipgloss.RoundedBorder(), true).
		Margin(0)

	var query_style = input_style.Copy()
	var time_style = input_style.Copy()
	var execute_style = input_style.Copy()

	focused := navigation_map[m.focus.y][m.focus.x]

	switch focused {
	case "query":
		query_style.BorderStyle(lipgloss.ThickBorder())
	case "time":
		time_style.BorderStyle(lipgloss.ThickBorder())
	case "execute":
		execute_style.BorderStyle(lipgloss.ThickBorder())
	case "table":
		m.table.WithBaseStyle(lipgloss.NewStyle().BorderBackground(lipgloss.Color("#EEE")))
	}

	m.table.WithMaxTotalWidth(m.width - 10)

	button := "execute"

	if m.mode == inactive {
		button = "loading"
	}

	var inputs = lipgloss.JoinHorizontal(
		lipgloss.Bottom,
		query_style.Render(m.query.View()),
		time_style.Render(fmt.Sprintf("%s\n%s", m.time_range.StartValue(), m.time_range.EndValue())),
		execute_style.Render(button),
	)

	render := fmt.Sprintf("Parseable View %s %s\n%s\n%s", m.profile.Url, m.stream, inputs, m.table.View())

	if m.mode == active && focused == "time" {
		return outer.Render(lipgloss.Place(m.width-4, m.height-4, lipgloss.Center, lipgloss.Center, m.time_range.View()))
	} else {
		return outer.Render(render)
	}

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

	for i := 0; i < len(data.schema); i++ {
		title := data.schema[i]
		if title == "p_timestamp" {
			continue
		}
		width := inferWidthForColumns(title, &data.data, 100, 10) + 3
		columns[i] = table.NewColumn(title, title, width)
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

func DateInput() (x textinput.Model) {
	x = textinput.New()
	x.TextStyle.Background(baseStyle.GetBackground())
	x.PlaceholderStyle.Background(baseStyle.GetBackground())
	x.Prompt = ""
	x.Width = datetime_width
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
