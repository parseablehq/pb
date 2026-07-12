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
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	table "github.com/evertras/bubble-table/table"
	"github.com/muesli/reflow/truncate"
	"github.com/parseablehq/pb/pkg/config"
	"github.com/parseablehq/pb/pkg/datasets"
	internalHTTP "github.com/parseablehq/pb/pkg/http"
	"github.com/parseablehq/pb/pkg/iterator"
	"github.com/parseablehq/pb/pkg/ui"
	"golang.org/x/exp/slices"
	"golang.org/x/term"
)

const (
	dateTimeWidth = len("TIME [LOCAL]")
	dateTimeKey   = "p_timestamp"
	tagKey        = "p_tags"
	metadataKey   = "p_metadata"

	sqlControlTimeDisplayWidth = len("02 Jun 2026, 13:25 | UTC+05:30")
	sqlControlsMinWidth        = sqlControlTimeDisplayWidth + 4 // border + row prefix
	sqlWindowSize              = 500
	sqlMaxCachedWindows        = 3
	unknownSQLTotal            = -1
)

// Theme-derived styles. All palette atoms come from pkg/ui — to swap a
// color, edit ui.Dark / ui.Light, not these vars.
var (
	FocusPrimary   = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Accent })
	FocusSecondary = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Accent2 })

	StandardPrimary   = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Body })
	StandardSecondary = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Mute })

	chromeBorder = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Border })

	baseStyle = lipgloss.NewStyle().BorderForeground(chromeBorder)
	// Header: bold + Accent fg, no background fill. Background tints
	// fight the terminal theme (especially when the user switches
	// light/dark) so we rely on weight + color contrast alone.
	headerStyle = lipgloss.NewStyle().
			Foreground(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Accent })).
			Bold(true).
			Padding(0, 1)
	// Data rows: Body fg, generous horizontal padding so columns
	// breathe and the divider glyphs don't sit flush against text.
	tableStyle = lipgloss.NewStyle().
			Foreground(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Body })).
			Align(lipgloss.Left).
			Padding(0, 1)
	// Highlight: SelRow bg + bold + Accent text on cursor row.
	highlightStyle = lipgloss.NewStyle().
			Background(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.SelRow })).
			Foreground(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Accent })).
			Bold(true)
)

var (
	// customBorder — outer box is drawn by renderResultsPane. Header
	// row gets an underline (`Bottom = "─"`) so it reads as a real
	// header strip; the top edge stays blank (a single space row)
	// because bubble-table forces BorderTop on header cells and any
	// non-blank Top char would draw a visible top rule we don't want.
	// Empty strings here render as phantom rows in lipgloss, which is
	// what caused the value column header to wrap to the line below.
	customBorder = table.Border{
		Top:    " ",
		Bottom: "─",
		Left:   "",
		Right:  "",

		TopLeft:     " ",
		TopRight:    " ",
		BottomLeft:  "─",
		BottomRight: "─",

		TopJunction:    " ",
		BottomJunction: "─",
		LeftJunction:   " ",
		RightJunction:  " ",
		InnerJunction:  "─",

		InnerDivider: "│",
	}

	queryNavigationMap = []string{"query", "time", "dataset", "columns", "table"}
)

func sqlPageSize(totalH, bottomH int) int {
	const topH = 12
	av := totalH - topH - bottomH
	if av < 6 {
		av = 6
	}
	rih := av - 3 // results pane border(2) + title(1)
	if rih < 3 {
		rih = 3
	}
	ps := rih - 5 // table overhead: header(3) + bottom-rule(1) + footer-line(1)
	if ps < 1 {
		ps = 1
	}
	return ps
}

type (
	Mode        int
	FetchResult int
)

type FetchData struct {
	status FetchResult
	schema []string
	data   []map[string]interface{}
	errMsg string
}

type sqlWindowFetchMsg struct {
	runID       int
	windowIndex int
	status      FetchResult
	schema      []string
	data        []map[string]interface{}
	errMsg      string
}

type sqlWindow struct {
	index    int
	schema   []string
	data     []map[string]interface{}
	lastUsed int
}

type sqlQueryPlan struct {
	baseQuery  string
	userLimit  int
	userOffset int
}

type schemaMsg struct {
	columns []string
	errMsg  string
}

const (
	fetchOk FetchResult = iota
	fetchErr
)

const (
	overlayNone uint = iota
	overlayInputs
)

const overlayColumn uint = 3

type QueryModel struct {
	width         int
	height        int
	table         table.Model
	query         textarea.Model
	timeRange     TimeInputModel
	timeRangeEdit TimeInputModel
	profile       config.Profile
	help          help.Model
	status        StatusBar
	spinner       spinner.Model
	loading       bool
	hasQueried    bool // true once the first query has been dispatched
	queryIterator *iterator.QueryIterator[QueryData, FetchResult]
	overlay       uint
	focused       int
	dataRows      []table.Row // actual data rows (without padding)
	fetchErrMsg   string      // last fetch error, shown in the result area
	tableRowsBase int         // global row offset represented by dataRows[0]

	windowSize      int
	globalOffset    int
	viewportStart   int
	queryLimit      int
	queryBaseOffset int
	actualTotal     int
	baseQuery       string
	queryRunID      int
	lockedStartUTC  string
	lockedEndUTC    string
	windows         map[int]sqlWindow
	loadingWindows  map[int]bool
	cacheClock      int
	windowFetchErr  string

	// dataset spotlight
	dataset            string
	spotlightFilter    textinput.Model
	allDatasets        []string
	filteredDatasets   []string
	datasetSelectedIdx int
	datasetsLoading    bool

	// column spotlight — populated when a dataset is selected
	selectedColumn    string
	columnFilter      textinput.Model
	allColumns        []string
	filteredColumns   []string
	columnSelectedIdx int
	columnsLoading    bool
	columnsDrawerOpen bool

	schema []string
}

func (m *QueryModel) focusSelected() {
	m.query.Blur()
	m.table = m.table.Focused(false)
	m.spotlightFilter.Blur()
	m.columnFilter.Blur()

	switch m.currentFocus() {
	case "query":
		m.query.Focus()
	case "table":
		m.table = m.table.Focused(true)
	case "columns":
		m.columnFilter.Focus()
	}
}

func (m QueryModel) focusOrder() []string {
	return queryNavigationMap
}

func (m QueryModel) currentFocus() string {
	order := m.focusOrder()
	if len(order) == 0 {
		return "query"
	}
	if m.focused < 0 {
		return order[0]
	}
	if m.focused >= len(order) {
		return order[len(order)-1]
	}
	return order[m.focused]
}

func (m *QueryModel) focusPane(name string) {
	for i, pane := range m.focusOrder() {
		if pane == name {
			m.focused = i
			m.focusSelected()
			return
		}
	}
	m.focused = 0
	m.focusSelected()
}

func (m *QueryModel) insertSelectedColumn() {
	if len(m.filteredColumns) == 0 {
		return
	}
	if m.columnSelectedIdx < 0 {
		m.columnSelectedIdx = 0
	}
	if m.columnSelectedIdx >= len(m.filteredColumns) {
		m.columnSelectedIdx = len(m.filteredColumns) - 1
	}
	m.selectedColumn = m.filteredColumns[m.columnSelectedIdx]
	escaped := strings.ReplaceAll(m.selectedColumn, `"`, `""`)
	m.query.InsertString(`"` + escaped + `" `)
	m.focusPane("query")
}

func (m *QueryModel) toggleColumnsDrawer() tea.Cmd {
	m.columnsDrawerOpen = !m.columnsDrawerOpen
	if !m.columnsDrawerOpen {
		m.focusPane("columns")
		return nil
	}

	selected := m.dataset
	if selected == "" {
		if extracted := extractDataset(m.query.Value()); extracted != "—" && extracted != "" {
			selected = extracted
			m.dataset = selected
		}
	}
	m.focusPane("columns")
	if selected != "" && len(m.allColumns) == 0 && !m.columnsLoading {
		m.columnsLoading = true
		return fetchStreamSchema(m.profile, selected)
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

	pageSize := sqlPageSize(h, 3)
	if pageSize < 5 {
		pageSize = 5
	}

	table := table.New(columns).
		WithRows(rows).
		Filtered(true).
		HeaderStyle(headerStyle).
		SelectableRows(false).
		Border(customBorder).
		Focused(false).
		WithKeyMap(tableKeyBinds).
		WithPageSize(pageSize).
		WithBaseStyle(tableStyle).
		HighlightStyle(highlightStyle).
		WithMissingDataIndicatorStyled(table.StyledCell{
			Style: lipgloss.NewStyle().Foreground(chromeBorder),
			Data:  "—",
		}).
		WithMaxTotalWidth(w).
		WithFooterVisibility(false)

	query := textarea.New()
	query.MaxHeight = 0
	query.MaxWidth = 0
	query.SetHeight(10)
	query.SetWidth(70)
	query.ShowLineNumbers = true
	// Hide vim-style `~` tildes — they're the textarea default end-of-
	// buffer glyph and read as "this UI is broken". Render a space so
	// the gutter stays aligned but produces no visual noise.
	query.EndOfBufferCharacter = ' '
	query.SetValue(queryStr)
	query.Placeholder = "Write your queries here"
	query.KeyMap = textAreaKeyMap

	// Theme-aware editor styles. Active-line gets a subtle bg shift
	// (EditorActive) so the cursor row stands out; line numbers in
	// Faint, prompt mark in Accent. Mirrors the mock editor look.
	applyEditorStyles(&query)
	query.Focus()

	help := help.New()
	help.Styles.FullDesc = ui.Type().Dim

	status := NewStatusBar(profile.URL, w)

	sp := spinner.New()
	sp.Spinner = spinner.Line
	sp.Style = ui.Type().Accent

	sf := textinput.New()
	sf.Placeholder = "filter datasets"
	sf.Prompt = "> "
	sf.PromptStyle = lipgloss.NewStyle().
		Foreground(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Accent })).
		Bold(true)
	sf.PlaceholderStyle = lipgloss.NewStyle().
		Foreground(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Ghost })).
		Italic(true)
	sf.Width = spotlightWidth - 8
	sf.Blur()

	cf := textinput.New()
	cf.Placeholder = "filter columns"
	cf.Prompt = "> "
	cf.PromptStyle = lipgloss.NewStyle().
		Foreground(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Accent })).
		Bold(true)
	cf.PlaceholderStyle = lipgloss.NewStyle().
		Foreground(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Ghost })).
		Italic(true)
	cf.Width = spotlightWidth - 8
	cf.Blur()

	hasQuery := strings.TrimSpace(queryStr) != ""
	initialDataset := ""
	if hasQuery {
		if extracted := extractDataset(queryStr); extracted != "—" && extracted != "" && !strings.EqualFold(extracted, "dataset") {
			initialDataset = extracted
		}
	}
	model := QueryModel{
		width:             w,
		height:            h,
		table:             table,
		query:             query,
		timeRange:         inputs,
		overlay:           overlayNone,
		profile:           profile,
		help:              help,
		spinner:           sp,
		loading:           hasQuery,
		hasQueried:        hasQuery,
		queryIterator:     nil,
		windowSize:        sqlWindowSize,
		actualTotal:       unknownSQLTotal,
		windows:           map[int]sqlWindow{},
		loadingWindows:    map[int]bool{},
		status:            status,
		spotlightFilter:   sf,
		columnFilter:      cf,
		columnsDrawerOpen: true,
		dataset:           initialDataset,
	}
	if hasQuery {
		model.prepareSQLWindowRun()
	}
	return model
}

func (m QueryModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, fetchAllStreams(m.profile)}
	if strings.TrimSpace(m.query.Value()) != "" {
		cmds = append(cmds, m.fetchSQLWindow(0))
	}
	return tea.Batch(cmds...)
}

func (m *QueryModel) prepareSQLWindowRun() {
	query := resolveDatasetPlaceholder(m.query.Value(), m.dataset)
	query = quoteUnsafeSQLTableNames(query)
	query = quoteUnsafeSQLFieldNames(query)
	plan := buildSQLQueryPlan(query, sqlWindowSize)

	m.queryRunID++
	m.baseQuery = plan.baseQuery
	m.queryLimit = plan.userLimit
	m.queryBaseOffset = plan.userOffset
	m.actualTotal = unknownSQLTotal
	m.globalOffset = 0
	m.viewportStart = 0
	m.tableRowsBase = 0
	m.dataRows = []table.Row{}
	m.windows = map[int]sqlWindow{}
	m.loadingWindows = map[int]bool{}
	m.cacheClock = 0
	m.fetchErrMsg = ""
	m.windowFetchErr = ""
	m.lockedStartUTC = m.timeRange.StartValueUtc()
	m.lockedEndUTC = m.timeRange.EndValueUtc()
	m.loading = true
	m.hasQueried = true
}

func (m *QueryModel) startSQLWindowRun() tea.Cmd {
	m.prepareSQLWindowRun()
	return m.fetchSQLWindow(0)
}

func (m *QueryModel) resetInvalidDatasetSelection() {
	if m.dataset == "" || len(m.allDatasets) == 0 {
		return
	}
	for i, ds := range m.allDatasets {
		if ds == m.dataset {
			m.datasetSelectedIdx = i
			return
		}
	}
	m.dataset = m.allDatasets[0]
	m.datasetSelectedIdx = 0
}

func (m *QueryModel) fetchSQLWindow(windowIndex int) tea.Cmd {
	if windowIndex < 0 || m.baseQuery == "" {
		return nil
	}
	if m.queryLimit >= 0 && windowIndex*m.windowSize >= m.queryLimit {
		return nil
	}
	if _, ok := m.windows[windowIndex]; ok {
		return nil
	}
	if m.loadingWindows[windowIndex] {
		return nil
	}
	fetchLimit := m.windowSize
	if m.queryLimit >= 0 {
		remaining := m.queryLimit - windowIndex*m.windowSize
		if remaining <= 0 {
			return nil
		}
		if remaining < fetchLimit {
			fetchLimit = remaining
		}
	}
	m.loadingWindows[windowIndex] = true
	return NewSQLWindowFetchTask(m.profile, m.queryRunID, m.baseQuery, m.queryBaseOffset, m.windowSize, fetchLimit, windowIndex, m.lockedStartUTC, m.lockedEndUTC)
}

func (m *QueryModel) completeSQLWindowFetch(msg sqlWindowFetchMsg) tea.Cmd {
	if msg.runID != m.queryRunID {
		return nil
	}
	delete(m.loadingWindows, msg.windowIndex)
	m.loading = len(m.windows) == 0 && len(m.loadingWindows) > 0
	m.status.Info = ""

	if msg.status != fetchOk {
		if msg.errMsg == "" {
			msg.errMsg = "query failed"
		}
		if len(m.windows) == 0 {
			m.dataRows = []table.Row{}
			m.table = m.table.WithRows([]table.Row{})
			m.fetchErrMsg = msg.errMsg
			m.status.Error = "query failed"
			m.resetInvalidDatasetSelection()
			return nil
		}
		m.windowFetchErr = msg.errMsg
		m.status.Error = "window fetch failed"
		m.refreshVisibleRows()
		return nil
	}

	m.fetchErrMsg = ""
	m.windowFetchErr = ""
	m.status.Error = ""
	m.cacheClock++
	m.windows[msg.windowIndex] = sqlWindow{
		index:    msg.windowIndex,
		schema:   msg.schema,
		data:     msg.data,
		lastUsed: m.cacheClock,
	}
	m.evictSQLWindows()

	if len(msg.schema) > 0 {
		m.schema = msg.schema
		m.UpdateTableColumns(msg.schema, msg.data)
	}
	if len(msg.data) < m.windowSize {
		m.actualTotal = msg.windowIndex*m.windowSize + len(msg.data)
	}
	if m.queryLimit >= 0 && m.actualTotal >= 0 && m.actualTotal > m.queryLimit {
		m.actualTotal = m.queryLimit
	}
	if m.currentTotal() == 0 {
		m.globalOffset = 0
	} else if m.globalOffset >= m.currentTotal() {
		m.globalOffset = m.currentTotal() - 1
	}
	m.clampSQLViewport()
	m.refreshVisibleRows()
	m.focusPane("table")
	return m.prefetchSQLWindowIfNeeded()
}

func (m *QueryModel) evictSQLWindows() {
	for len(m.windows) > sqlMaxCachedWindows {
		evictIdx := -1
		evictUsed := int(^uint(0) >> 1)
		for idx, win := range m.windows {
			if idx == m.currentWindowIndex() {
				continue
			}
			if win.lastUsed < evictUsed {
				evictIdx = idx
				evictUsed = win.lastUsed
			}
		}
		if evictIdx < 0 {
			for idx, win := range m.windows {
				if win.lastUsed < evictUsed {
					evictIdx = idx
					evictUsed = win.lastUsed
				}
			}
		}
		if evictIdx < 0 {
			return
		}
		delete(m.windows, evictIdx)
	}
}

func (m QueryModel) currentWindowIndex() int {
	if m.windowSize <= 0 {
		return 0
	}
	return m.globalOffset / m.windowSize
}

func (m QueryModel) currentTotal() int {
	if m.actualTotal >= 0 {
		return m.actualTotal
	}
	if m.queryLimit >= 0 {
		return m.queryLimit
	}
	return 0
}

func (m *QueryModel) refreshVisibleRows() {
	total := m.currentTotal()
	if total == 0 {
		m.dataRows = []table.Row{}
		m.table = m.table.WithRows(m.dataRows)
		m.viewportStart = 0
		return
	}
	pageSize := m.tablePageSize()
	if pageSize < 1 {
		pageSize = 1
	}
	m.clampSQLViewport()
	start := m.viewportStart
	rows := make([]table.Row, 0, pageSize)
	for abs := start; abs < total && len(rows) < pageSize; abs++ {
		winIdx := abs / m.windowSize
		local := abs % m.windowSize
		win, ok := m.windows[winIdx]
		if ok && local < len(win.data) {
			m.cacheClock++
			win.lastUsed = m.cacheClock
			m.windows[winIdx] = win
			rows = append(rows, table.NewRow(m.formatSQLRow(win.data[local])))
			continue
		}
		rows = append(rows, table.NewRow(m.placeholderSQLRow(abs)))
	}
	m.tableRowsBase = start
	m.dataRows = rows
	highlight := m.globalOffset - start
	if highlight < 0 {
		highlight = 0
	}
	if highlight >= len(rows) && len(rows) > 0 {
		highlight = len(rows) - 1
	}
	m.table = m.table.WithRows(m.dataRows).WithHighlightedRow(highlight)
}

func (m *QueryModel) clampSQLViewport() {
	total := m.currentTotal()
	pageSize := m.tablePageSize()
	if pageSize < 1 {
		pageSize = 1
	}
	if total <= 0 {
		m.viewportStart = 0
		return
	}
	if m.globalOffset < 0 {
		m.globalOffset = 0
	}
	if m.globalOffset >= total {
		m.globalOffset = total - 1
	}
	maxStart := total - pageSize
	if maxStart < 0 {
		maxStart = 0
	}
	if m.viewportStart > maxStart {
		m.viewportStart = maxStart
	}
	if m.viewportStart < 0 {
		m.viewportStart = 0
	}
	if m.globalOffset < m.viewportStart {
		m.viewportStart = m.globalOffset
	}
	if m.globalOffset >= m.viewportStart+pageSize {
		m.viewportStart = m.globalOffset - pageSize + 1
	}
}

func (m QueryModel) tablePageSize() int {
	if m.height == 0 {
		return 10
	}
	bh := lipgloss.Height(buildBottomBar(m, m.width))
	ps := sqlPageSize(m.height, bh)
	if ps < 1 {
		return 1
	}
	return ps
}

func (m QueryModel) currentTablePage() int {
	pageSize := m.tablePageSize()
	if pageSize < 1 {
		pageSize = 1
	}
	return m.globalOffset/pageSize + 1
}

func (m QueryModel) maxTablePages() int {
	total := m.currentTotal()
	if total <= 0 {
		return 1
	}
	pageSize := m.tablePageSize()
	if pageSize < 1 {
		pageSize = 1
	}
	pages := (total + pageSize - 1) / pageSize
	if pages < 1 {
		return 1
	}
	return pages
}

func (m QueryModel) formatSQLRow(row map[string]interface{}) table.RowData {
	out := make(table.RowData, len(row))
	for k, v := range row {
		out[k] = v
	}
	if ts, ok := out[dateTimeKey].(string); ok {
		out[dateTimeKey] = formatTimestampToDisplayHMS(ts, m.timeRange.start.Time().Location(), m.timeRange.DisplayMode())
	}
	return out
}

func (m QueryModel) placeholderSQLRow(abs int) table.RowData {
	row := table.RowData{}
	for _, col := range m.schema {
		row[col] = "loading..."
	}
	if len(row) == 0 {
		row["Id"] = fmt.Sprintf("%d", abs+1)
	}
	return row
}

func (m *QueryModel) prefetchSQLWindowIfNeeded() tea.Cmd {
	winIdx := m.currentWindowIndex()
	local := m.globalOffset % m.windowSize
	if local < int(float64(m.windowSize)*0.9) {
		return nil
	}
	return m.fetchSQLWindow(winIdx + 1)
}

func (m *QueryModel) ensureCurrentSQLWindow() tea.Cmd {
	return m.fetchSQLWindow(m.currentWindowIndex())
}

func (m *QueryModel) moveSQLCursor(delta int) tea.Cmd {
	total := m.currentTotal()
	if total == 0 {
		return nil
	}
	next := m.globalOffset + delta
	if next < 0 {
		next = 0
	}
	if next >= total {
		next = total - 1
	}
	if next == m.globalOffset {
		return nil
	}
	m.globalOffset = next
	m.clampSQLViewport()
	m.refreshVisibleRows()
	return tea.Batch(m.ensureCurrentSQLWindow(), m.prefetchSQLWindowIfNeeded())
}

func (m *QueryModel) handleSQLTableNavigation(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.table.GetIsFilterInputFocused() || len(m.dataRows) == 0 {
		return false, nil
	}
	page := m.tablePageSize()
	switch {
	case key.Matches(msg, tableKeyBinds.RowUp):
		return true, m.moveSQLCursor(-1)
	case key.Matches(msg, tableKeyBinds.RowDown):
		return true, m.moveSQLCursor(1)
	case key.Matches(msg, tableKeyBinds.PageUp):
		return true, m.moveSQLCursor(-page)
	case key.Matches(msg, tableKeyBinds.PageDown):
		return true, m.moveSQLCursor(page)
	case key.Matches(msg, tableKeyBinds.PageFirst):
		m.globalOffset = 0
		m.viewportStart = 0
		m.refreshVisibleRows()
		return true, m.ensureCurrentSQLWindow()
	case key.Matches(msg, tableKeyBinds.PageLast):
		total := m.currentTotal()
		if total > 0 {
			m.globalOffset = total - 1
			m.viewportStart = total - page
			m.refreshVisibleRows()
			return true, m.ensureCurrentSQLWindow()
		}
		return true, nil
	default:
		return false, nil
	}
}

func (m QueryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {

	case spinner.TickMsg:
		if m.loading {
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = m.width
		m.status.width = m.width
		bh := lipgloss.Height(buildBottomBar(m, m.width))
		m.table = m.table.WithMaxTotalWidth(m.width).WithPageSize(sqlPageSize(m.height, bh))
		if len(m.windows) > 0 {
			m.refreshVisibleRows()
		}
		return m, nil

	case schemaMsg:
		m.columnsLoading = false
		if msg.errMsg == "" && len(msg.columns) > 0 {
			m.allColumns = msg.columns
			m.filteredColumns = msg.columns
			m.columnSelectedIdx = 0
		}
		return m, nil

	case datasetListMsg:
		m.datasetsLoading = false
		if msg.errMsg != "" {
			m.status.Error = "could not load datasets: " + msg.errMsg
		} else {
			m.allDatasets = msg.datasets
			m.filteredDatasets = msg.datasets
			m.datasetSelectedIdx = 0
			if m.dataset == "" && len(msg.datasets) > 0 {
				m.dataset = msg.datasets[0]
			}
			for i, ds := range m.filteredDatasets {
				if ds == m.dataset {
					m.datasetSelectedIdx = i
					break
				}
			}
			m.resetInvalidDatasetSelection()
			if m.columnsDrawerOpen && m.dataset != "" && len(m.allColumns) == 0 && !m.columnsLoading {
				m.columnsLoading = true
				cmds = append(cmds, fetchStreamSchema(m.profile, m.dataset))
			}
		}
		return m, tea.Batch(cmds...)

	case FetchData:
		m.loading = false
		m.status.Info = ""
		if msg.status == fetchOk {
			m.fetchErrMsg = ""
			m.UpdateTable(msg)
			m.status.Error = ""
			m.status.Info = ""
			// Recompute page size now that status.Info changed — buildBottomBar
			// height may shift, causing stored pageSize to drift from View()'s
			// computed value, which breaks cursor-to-page mapping in navigation.
			bh := lipgloss.Height(buildBottomBar(m, m.width))
			m.table = m.table.WithPageSize(sqlPageSize(m.height, bh))
			// Move focus to results table after a successful fetch.
			m.focusPane("table")
		} else {
			m.dataRows = []table.Row{}
			m.table = m.table.WithRows([]table.Row{})
			m.fetchErrMsg = msg.errMsg
			if m.fetchErrMsg == "" {
				m.fetchErrMsg = "query failed"
			}
			m.status.Error = "query failed"
		}
		return m, nil

	case sqlWindowFetchMsg:
		cmds = append(cmds, m.completeSQLWindowFetch(msg))
		return m, tea.Batch(cmds...)

		// Is it a key press?
	case tea.KeyMsg:

		// ── dataset spotlight overlay ────────────────────────────────────
		if m.overlay == overlayDataset {
			switch msg.Type {
			case tea.KeyEsc:
				m.overlay = overlayNone
				m.spotlightFilter.SetValue("")
				m.spotlightFilter.Blur()
				m.focusSelected()
				return m, nil

			case tea.KeyEnter:
				if len(m.filteredDatasets) > 0 {
					selected := m.filteredDatasets[m.datasetSelectedIdx]
					needsSchema := selected != m.dataset || len(m.allColumns) == 0
					if selected != m.dataset {
						// dataset changed — reset column state
						m.dataset = selected
						m.selectedColumn = ""
						m.columnFilter.SetValue("")
						m.allColumns = []string{}
						m.filteredColumns = []string{}
					}
					if needsSchema {
						m.columnsLoading = true
						cmds = append(cmds, fetchStreamSchema(m.profile, selected))
					}
					if strings.TrimSpace(m.query.Value()) == "" {
						m.query.SetValue("SELECT * FROM dataset LIMIT 100")
						m.query.CursorEnd()
					}
				}
				m.overlay = overlayNone
				m.spotlightFilter.SetValue("")
				m.spotlightFilter.Blur()
				m.focusPane("query")
				return m, tea.Batch(cmds...)

			case tea.KeyUp:
				if m.datasetSelectedIdx > 0 {
					m.datasetSelectedIdx--
				}
				return m, nil

			case tea.KeyDown:
				if m.datasetSelectedIdx < len(m.filteredDatasets)-1 {
					m.datasetSelectedIdx++
				}
				return m, nil

			default:
				prev := m.spotlightFilter.Value()
				m.spotlightFilter, cmd = m.spotlightFilter.Update(msg)
				cmds = append(cmds, cmd)
				if m.spotlightFilter.Value() != prev {
					m.filteredDatasets = filterDatasets(m.allDatasets, m.spotlightFilter.Value())
					m.datasetSelectedIdx = 0
				}
				return m, tea.Batch(cmds...)
			}
		}

		// ── column spotlight overlay ─────────────────────────────────────
		if m.overlay == overlayColumn {
			switch msg.Type {
			case tea.KeyEsc:
				m.overlay = overlayNone
				m.columnFilter.SetValue("")
				m.columnFilter.Blur()
				m.focusSelected()
				return m, nil

			case tea.KeyEnter:
				if len(m.filteredColumns) > 0 {
					m.selectedColumn = m.filteredColumns[m.columnSelectedIdx]
					escaped := strings.ReplaceAll(m.selectedColumn, `"`, `""`)
					m.query.InsertString(`"` + escaped + `" `)
				}
				m.overlay = overlayNone
				m.columnFilter.SetValue("")
				m.columnFilter.Blur()
				m.focused = 0
				m.focusSelected()
				return m, nil

			case tea.KeyUp:
				if m.columnSelectedIdx > 0 {
					m.columnSelectedIdx--
				}
				return m, nil

			case tea.KeyDown:
				if m.columnSelectedIdx < len(m.filteredColumns)-1 {
					m.columnSelectedIdx++
				}
				return m, nil

			default:
				prev := m.columnFilter.Value()
				m.columnFilter, cmd = m.columnFilter.Update(msg)
				cmds = append(cmds, cmd)
				if m.columnFilter.Value() != prev {
					m.filteredColumns = filterDatasets(m.allColumns, m.columnFilter.Value())
					m.columnSelectedIdx = 0
				}
				return m, tea.Batch(cmds...)
			}
		}

		// special behavior on main page
		if m.overlay == overlayNone {
			if msg.String() == "ctrl+l" {
				return m, m.toggleColumnsDrawer()
			}

			if msg.Type == tea.KeyCtrlD {
				m.overlay = overlayDataset
				m.spotlightFilter.Focus()
				m.datasetsLoading = true
				return m, fetchAllStreams(m.profile)
			}

			if msg.Type == tea.KeyEnter && m.currentFocus() == "dataset" {
				m.overlay = overlayDataset
				m.spotlightFilter.Focus()
				m.datasetsLoading = true
				return m, fetchAllStreams(m.profile)
			}

			if msg.Type == tea.KeyEnter && m.currentFocus() == "columns" {
				m.insertSelectedColumn()
				return m, nil
			}

			if msg.Type == tea.KeyEnter && m.currentFocus() == "time" {
				m.timeRangeEdit = m.timeRange
				m.timeRangeEdit.SyncPreset()
				m.overlay = overlayInputs
				return m, nil
			}

			if msg.Type == tea.KeyTab {
				m.focused++
				if m.focused > len(m.focusOrder())-1 {
					m.focused = 0
				}
				m.focusSelected()
				return m, nil
			}

			if msg.Type == tea.KeyShiftTab {
				m.focused--
				if m.focused < 0 {
					m.focused = len(m.focusOrder()) - 1
				}
				m.focusSelected()
				return m, nil
			}

			if m.currentFocus() == "columns" && m.columnsDrawerOpen {
				switch msg.Type {
				case tea.KeyUp:
					if m.columnSelectedIdx > 0 {
						m.columnSelectedIdx--
					}
					return m, nil
				case tea.KeyDown:
					if m.columnSelectedIdx < len(m.filteredColumns)-1 {
						m.columnSelectedIdx++
					}
					return m, nil
				case tea.KeyRunes:
					m.columnFilter.SetValue(m.columnFilter.Value() + string(msg.Runes))
					m.filteredColumns = filterDatasets(m.allColumns, m.columnFilter.Value())
					m.columnSelectedIdx = 0
					return m, nil
				case tea.KeyBackspace:
					value := []rune(m.columnFilter.Value())
					if len(value) > 0 {
						m.columnFilter.SetValue(string(value[:len(value)-1]))
						m.filteredColumns = filterDatasets(m.allColumns, m.columnFilter.Value())
						m.columnSelectedIdx = 0
					}
					return m, nil
				case tea.KeyEsc:
					if m.columnFilter.Value() != "" {
						m.columnFilter.SetValue("")
						m.filteredColumns = m.allColumns
						m.columnSelectedIdx = 0
						return m, nil
					}
				}
			}
		}

		// special behavior on time input page
		if m.overlay == overlayInputs {
			// Esc: close modal without applying. Returns to main view
			// with previous start/end intact.
			if msg.Type == tea.KeyEsc {
				m.overlay = overlayNone
				m.focusSelected()
				return m, nil
			}
			if msg.Type == tea.KeyEnter {
				m.timeRange = m.timeRangeEdit
				m.overlay = overlayNone
				m.focusSelected()
				m.status.Error = ""
				m.status.Info = ""
				m.loading = true
				m.hasQueried = true
				return m, tea.Batch(m.spinner.Tick, m.startSQLWindowRun())
			}
		}

		// common keybind — Ctrl+R, Alt+Enter (Cmd+Enter on macOS once
		// the terminal is configured to send Meta on Cmd) all run the
		// current query.
		isAltEnter := msg.Alt && msg.Type == tea.KeyEnter
		if msg.Type == tea.KeyCtrlR || isAltEnter {
			m.overlay = overlayNone
			m.status.Error = ""
			m.status.Info = ""
			m.loading = true
			m.hasQueried = true
			return m, tea.Batch(m.spinner.Tick, m.startSQLWindowRun())
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
					if handled, navCmd := m.handleSQLTableNavigation(msg); handled {
						cmd = navCmd
					} else {
						m.table, cmd = m.table.Update(msg)
					}
				}
				cmds = append(cmds, cmd)
			case overlayInputs:
				m.timeRangeEdit, cmd = m.timeRangeEdit.Update(msg)
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
	p := ui.Active

	// No breadcrumbs — minimal layout: editor + time on top, table,
	// helper, status. Per scope: 5 zones only.
	crumbsHeight := 0

	// ── 2. Status bar / help (precompute heights) ─────────────────────
	if m.loading {
		m.status.Info = ""
		m.status.Error = ""
	}
	m.status.SetMode("SQL")
	bottomView := buildBottomBar(m, m.width)
	bottomHeight := lipgloss.Height(bottomView)

	// ── 3. TOP row: editor (wide) + controls (narrow). Plain rectangles,
	// label-only chrome. Controls mirror Prism order: time first, dataset below.
	// Sidebar is wide enough to show formatted time without truncating:
	// "02 Jun 2026, 11:25 AM | UTC+05:30".
	sidebarW := sqlControlsMinWidth
	if m.width >= 140 {
		sidebarW = sqlControlsMinWidth + 3
	}

	bodyH := m.height - crumbsHeight - bottomHeight
	if bodyH < 12 {
		bodyH = 12
	}
	const editorH = 12
	fullHeightControls := m.columnsDrawerOpen

	// editorW reserves 1 col for the horizontal gap between editor
	// and sidebar, so the two │ borders aren't flush against each other.
	editorW := m.width - sidebarW - 1
	if editorW < 30 {
		editorW = 30
		sidebarW = m.width - editorW - 1
	}
	m.query.SetWidth(editorW - 6)
	editorBodyH := editorH - 4 // border(2) + title(1) + spacer(1)
	if editorBodyH < 1 {
		editorBodyH = 1
	}
	m.query.SetHeight(editorBodyH)
	editorPane := renderEditorPane(m.query.View(), editorW, editorH, m.currentFocus() == "query")

	// Prefer the explicitly selected dataset; fall back to parsing the FROM clause.
	dataset := m.dataset
	if dataset == "" {
		if extracted := extractDataset(m.query.Value()); extracted != "—" && extracted != "" {
			dataset = extracted
		}
	}
	if dataset == "" {
		dataset = "select-dataset"
	}

	controlsH := editorH
	if fullHeightControls {
		controlsH = bodyH
	}
	controlsPane := renderSQLControlsBox(
		formatSQLControlTime(m.timeRange.start.Time(), m.timeRange.DisplayMode()),
		formatSQLControlTime(m.timeRange.end.Time(), m.timeRange.DisplayMode()),
		dataset,
		m.filteredColumns,
		m.columnSelectedIdx,
		m.columnFilter.Value(),
		m.columnsDrawerOpen,
		m.columnsLoading,
		sidebarW,
		controlsH,
		m.currentFocus(),
		m.timeRange.DisplayMode(),
	)

	// ── 4. Results / table area ───────────────────────────────────────
	availH := bodyH - editorH
	if availH < 6 {
		availH = 6
	}
	resultsH := availH
	resultsW := m.width
	if fullHeightControls {
		resultsW = editorW
	}
	// Results pane border (2) + label row (1) = 3 rows of chrome.
	resultsInnerH := resultsH - 3
	if resultsInnerH < 3 {
		resultsInnerH = 3
	}
	resultsInnerW := resultsW - 4 // border(2) + h-padding(2)
	if resultsInnerW < 10 {
		resultsInnerW = 10
	}
	// Table overhead: header(3) + last-row-bottom(1) + inside-footer(1) = 5 lines.
	pageSize := resultsInnerH - 5
	if pageSize < 1 {
		pageSize = 1
	}
	m.table = m.table.WithPageSize(pageSize).WithRows(m.dataRows).WithMaxTotalWidth(resultsInnerW)
	// Sync currentPage to the cursor's actual position using the View()-computed
	// pageSize. Prevents the cursor disappearing at page boundaries when the
	// stored pageSize (set in WindowSizeMsg/FetchData) drifts from pageSize above.
	m.table = m.table.WithHighlightedRow(m.table.GetHighlightedRowIndex())

	var inner string
	switch {
	case !m.hasQueried:
		wordmark := lipgloss.NewStyle().
			Foreground(p.Accent).
			Bold(true).
			Render(parseableASCIIArt)
		inner = lipgloss.Place(resultsInnerW, resultsInnerH, lipgloss.Center, lipgloss.Center, wordmark,
			lipgloss.WithWhitespaceChars(" "))
	case m.loading:
		content := ui.Type().Accent.Render(m.spinner.View() + " fetching...")
		inner = lipgloss.Place(resultsInnerW, resultsInnerH, lipgloss.Center, lipgloss.Center, content,
			lipgloss.WithWhitespaceChars(" "))
	case m.fetchErrMsg != "":
		errStyle := lipgloss.NewStyle().
			Padding(1, 2).
			Foreground(p.Err).
			Width(resultsInnerW)
		rendered := errStyle.Render(m.fetchErrMsg)
		lines := strings.Split(rendered, "\n")
		if len(lines) > resultsInnerH {
			lines = lines[:resultsInnerH]
		}
		inner = strings.Join(lines, "\n")
	case len(m.dataRows) == 0:
		msg := lipgloss.NewStyle().Foreground(p.Faint).Render("no results for this query")
		inner = lipgloss.Place(resultsInnerW, resultsInnerH, lipgloss.Center, lipgloss.Center, msg,
			lipgloss.WithWhitespaceChars(" "))
	default:
		tableStr := m.table.View()
		tableLines := strings.Split(tableStr, "\n")
		for len(tableLines) > 0 && tableLines[len(tableLines)-1] == "" {
			tableLines = tableLines[:len(tableLines)-1]
		}
		// Peel off the closing "────" rule so "--" rows land inside the frame.
		var bottomRule string
		if len(tableLines) > 0 {
			bottomRule = tableLines[len(tableLines)-1]
			tableLines = tableLines[:len(tableLines)-1]
		}
		tableBodyH := len(tableLines) + 1 // +1 for the bottom rule
		paddingH := resultsInnerH - 1 - tableBodyH
		if paddingH < 0 {
			paddingH = 0
		}
		dashRow := lipgloss.NewStyle().
			Foreground(p.Ghost).
			Width(resultsInnerW).
			Render(" --")
		var leftPart, rightPart string
		if m.currentTotal() > 0 {
			leftPart = fmt.Sprintf("%d/%d", m.currentTablePage(), m.maxTablePages())
		}
		if len(m.dataRows) > 0 && m.currentTotal() > 0 {
			rightPart = fmt.Sprintf("rows %d/%d", m.globalOffset+1, m.currentTotal())
			if len(m.loadingWindows) > 0 {
				rightPart += " · loading"
			}
			if m.windowFetchErr != "" {
				rightPart += " · fetch failed"
			}
		}
		faint := lipgloss.NewStyle().Foreground(p.Faint)
		leftR, rightR := faint.Render(leftPart), faint.Render(rightPart)
		footerGap := resultsInnerW - 2 - lipgloss.Width(leftR) - lipgloss.Width(rightR)
		if footerGap < 1 {
			footerGap = 1
		}
		footerLine := lipgloss.NewStyle().Width(resultsInnerW).Padding(0, 1).
			Render(leftR + strings.Repeat(" ", footerGap) + rightR)
		parts := make([]string, 0, len(tableLines)+paddingH+3)
		parts = append(parts, strings.Join(tableLines, "\n"))
		for i := 0; i < paddingH; i++ {
			parts = append(parts, dashRow)
		}
		parts = append(parts, bottomRule)
		parts = append(parts, footerLine)
		inner = strings.Join(parts, "\n")
	}
	{
		lines := strings.Split(inner, "\n")
		if len(lines) > resultsInnerH {
			lines = lines[:resultsInnerH]
		}
		inner = strings.Join(lines, "\n")
	}
	// rowCount=0: row info lives in the footer line inside the pane body.
	resultsPane := renderResultsPane(inner, resultsW, resultsH, 0, m.currentFocus() == "table")
	resultsSection := resultsPane

	// ── 5. Compose body or overlay ────────────────────────────────────
	topGap := lipgloss.NewStyle().Width(1).Height(editorH).Render("")
	topSection := lipgloss.JoinHorizontal(lipgloss.Top, editorPane, topGap, controlsPane)
	body := lipgloss.JoinVertical(lipgloss.Left, topSection, resultsSection)
	if fullHeightControls {
		leftSection := lipgloss.JoinVertical(lipgloss.Left, editorPane, resultsSection)
		sideGap := lipgloss.NewStyle().Width(1).Height(bodyH).Render("")
		body = lipgloss.JoinHorizontal(lipgloss.Top, leftSection, sideGap, controlsPane)
	}
	var mainView string
	switch m.overlay {
	case overlayNone:
		mainView = body
	case overlayInputs:
		timeView := m.timeRangeEdit.View()
		mainView = lipgloss.Place(m.width, m.height-crumbsHeight-bottomHeight,
			lipgloss.Center, lipgloss.Center, timeView,
			lipgloss.WithWhitespaceChars(" "),
		)
	case overlayDataset:
		spotlight := m.renderSQLSpotlight()
		mainView = lipgloss.Place(m.width, m.height-crumbsHeight-bottomHeight,
			lipgloss.Center, lipgloss.Center, spotlight,
			lipgloss.WithWhitespaceChars(" "),
		)
	case overlayColumn:
		spotlight := m.renderSQLColumnSpotlight()
		mainView = lipgloss.Place(m.width, m.height-crumbsHeight-bottomHeight,
			lipgloss.Center, lipgloss.Center, spotlight,
			lipgloss.WithWhitespaceChars(" "),
		)
	}

	render := lipgloss.JoinVertical(lipgloss.Left,
		mainView,
		bottomView,
	)
	return lipgloss.NewStyle().Width(m.width).Render(render)
}

func renderEditorPane(body string, width, height int, focused bool) string {
	return renderTitledPane("EDITOR", body, width, height, focused)
}

func renderTitledPane(title, body string, width, height int, focused bool) string {
	p := ui.Active
	borderColor := p.Border
	titleFg := p.Faint
	if focused {
		borderColor = p.BorderHi
		titleFg = p.Accent
	}
	innerW := width - 2
	if innerW < 4 {
		innerW = 4
	}
	innerH := height - 2
	if innerH < 3 {
		innerH = 3
	}
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)
	titleStyle := lipgloss.NewStyle().Foreground(titleFg).Bold(focused)
	lines := []string{paneRule("┌", "┐", title, width, borderStyle, titleStyle)}
	lines = append(lines, paneBodyLines(body, width, innerH, borderStyle)...)
	lines = append(lines, borderStyle.Render("└"+strings.Repeat("─", innerW)+"┘"))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func paneRule(left, right, title string, width int, borderStyle, titleStyle lipgloss.Style) string {
	if width < 2 {
		width = 2
	}
	if title == "" {
		return borderStyle.Render(left + strings.Repeat("─", width-2) + right)
	}
	label := " " + title + " "
	prefix := left + "──"
	fill := width - lipgloss.Width(prefix) - lipgloss.Width(label) - lipgloss.Width(right)
	if fill < 0 {
		fill = 0
		titleW := width - lipgloss.Width(prefix) - lipgloss.Width(right) - 2
		if titleW < 1 {
			titleW = 1
		}
		label = " " + ui.Truncate(title, titleW) + " "
	}
	return borderStyle.Render(prefix) + titleStyle.Render(label) + borderStyle.Render(strings.Repeat("─", fill)+right)
}

func paneBodyLines(body string, width, height int, borderStyle lipgloss.Style) []string {
	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}
	textW := innerW - 2
	if textW < 1 {
		textW = 1
	}
	lines := strings.Split(body, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	out := make([]string, 0, height)
	for _, line := range lines {
		if lipgloss.Width(line) > textW {
			line = truncate.String(line, uint(textW))
		}
		pad := textW - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
		out = append(out, borderStyle.Render("│")+" "+line+strings.Repeat(" ", pad)+" "+borderStyle.Render("│"))
	}
	return out
}

// renderSQLControlsBox draws the merged SQL sidebar control panel.
// Order is intentionally TIME first, then DATASET and inline COLUMNS below.
func renderSQLControlsBox(start, end, dataset string, columns []string, columnIdx int, columnFilter string, columnsOpen, columnsLoading bool, width, height int, focusedOn string, displayMode TimeDisplayMode) string {
	p := ui.Active
	innerW := width - 2
	if innerW < 4 {
		innerW = 4
	}

	rail := lipgloss.NewStyle().Background(p.Active).Render(" ")
	dim := lipgloss.NewStyle().Foreground(p.Faint)
	body := lipgloss.NewStyle().Foreground(p.Body)
	active := lipgloss.NewStyle().Foreground(p.Active).Bold(true)
	sectionFocused := focusedOn == "time" || focusedOn == "dataset" || focusedOn == "columns"
	borderColor := p.Border
	if sectionFocused {
		borderColor = p.BorderHi
	}
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)
	timeTitleStyle := lipgloss.NewStyle().Foreground(p.Faint)
	if focusedOn == "time" {
		timeTitleStyle = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	}
	datasetTitleStyle := lipgloss.NewStyle().Foreground(p.Faint)
	if focusedOn == "dataset" {
		datasetTitleStyle = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	}
	columnsTitleStyle := lipgloss.NewStyle().Foreground(p.Faint)
	if focusedOn == "columns" {
		columnsTitleStyle = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	}

	valW := innerW - 2
	if valW < 4 {
		valW = 4
	}
	row := func(focus, label, value string, valueStyle lipgloss.Style, truncate bool) []string {
		labelStyle := dim
		prefix := "  "
		if focusedOn == focus {
			labelStyle = active
			prefix = rail + " "
		}
		display := value
		if truncate {
			display = ui.Truncate(value, valW)
		}
		return []string{
			prefix + labelStyle.Render(label),
			prefix + valueStyle.Render(display),
		}
	}

	truncateTime := valW < sqlControlTimeDisplayWidth
	timeLines := []string{}
	timeLines = append(timeLines, row("time", "FROM", start, body, truncateTime)...)
	timeLines = append(timeLines, row("time", "TO", end, body, truncateTime)...)
	timeLines = append(timeLines, "")

	datasetPrefix := "  "
	datasetStyle := body
	if focusedOn == "dataset" {
		datasetPrefix = rail + " "
		datasetStyle = active
	}
	datasetLines := []string{
		datasetPrefix + datasetStyle.Render(ui.Truncate(dataset, valW)),
		"",
	}

	columnsTitle := fmt.Sprintf("COLUMNS (%d)", len(columns))
	columnSectionH := height - 4 - len(timeLines) - len(datasetLines) // top + 2 dividers + bottom
	if columnSectionH < 1 {
		columnSectionH = 1
	}
	listAvailable := columnSectionH
	footerH := 0
	if columnsOpen && listAvailable > 1 {
		footerH = 1
		listAvailable--
	}
	if listAvailable < 1 {
		listAvailable = 1
	}
	var columnLines []string
	if !columnsOpen {
		columnLines = append(columnLines, "  "+dim.Render("Show columns"))
	} else if columnsLoading {
		columnLines = append(columnLines, "  "+dim.Render("loading columns..."))
	} else if len(columns) == 0 {
		columnLines = append(columnLines, "  "+dim.Render("no columns"))
	} else {
		if columnIdx < 0 {
			columnIdx = 0
		}
		if columnIdx >= len(columns) {
			columnIdx = len(columns) - 1
		}
		listH := listAvailable
		if listH < 1 {
			listH = 1
		}
		startIdx := 0
		if columnIdx >= listH {
			startIdx = columnIdx - listH + 1
		}
		if startIdx+listH > len(columns) {
			startIdx = len(columns) - listH
			if startIdx < 0 {
				startIdx = 0
			}
		}
		for i := startIdx; i < startIdx+listH && i < len(columns); i++ {
			col := ui.Truncate(columns[i], valW)
			if focusedOn == "columns" && i == columnIdx {
				columnLines = append(columnLines, highlightStyle.Width(valW).Render(col))
			} else {
				columnLines = append(columnLines, "  "+body.Render(col))
			}
		}
	}
	for len(columnLines) < listAvailable {
		columnLines = append(columnLines, "")
	}
	if footerH == 1 {
		columnLines = append(columnLines, renderColumnFilterFooter(columnFilter, columnIdx, len(columns), valW, focusedOn == "columns"))
	}

	lines := []string{
		paneRule("┌", "┐", "TIME RANGE "+formatResultTimeLabel(displayMode), width, borderStyle, timeTitleStyle),
	}
	lines = append(lines, paneBodyLines(lipgloss.JoinVertical(lipgloss.Left, timeLines...), width, len(timeLines), borderStyle)...)
	lines = append(lines, paneRule("├", "┤", "DATASET <ctrl-d>", width, borderStyle, datasetTitleStyle))
	lines = append(lines, paneBodyLines(lipgloss.JoinVertical(lipgloss.Left, datasetLines...), width, len(datasetLines), borderStyle)...)
	lines = append(lines, paneRule("├", "┤", columnsTitle, width, borderStyle, columnsTitleStyle))
	lines = append(lines, paneBodyLines(lipgloss.JoinVertical(lipgloss.Left, columnLines...), width, columnSectionH, borderStyle)...)
	lines = append(lines, borderStyle.Render("└"+strings.Repeat("─", innerW)+"┘"))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func renderColumnFilterFooter(columnFilter string, columnIdx, total, width int, focused bool) string {
	p := ui.Active
	dim := lipgloss.NewStyle().Foreground(p.Faint)
	body := lipgloss.NewStyle().Foreground(p.Body)

	count := "0/0"
	if total > 0 {
		current := columnIdx + 1
		if current < 1 {
			current = 1
		}
		if current > total {
			current = total
		}
		count = fmt.Sprintf("%d/%d", current, total)
	}

	cursor := ""
	cursorW := 0
	if focused {
		cursor = lipgloss.NewStyle().
			Background(p.Cursor).
			Foreground(p.InvertText).
			Render(" ")
		cursorW = 1
	}

	prefix := "filter: "
	countW := lipgloss.Width(count)
	filterW := width - lipgloss.Width(prefix) - countW - cursorW - 1
	if filterW < 0 {
		filterW = 0
	}
	left := dim.Render(prefix) + body.Render(ui.Truncate(columnFilter, filterW)) + cursor
	right := dim.Render(count)
	pad := width - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

func formatSQLControlTime(t time.Time, mode TimeDisplayMode) string {
	if mode == TimeDisplayUTC {
		return t.UTC().Format("02 Jan 2006, 15:04 | UTC")
	}
	return t.Format("02 Jan 2006, 15:04 | UTC-07:00")
}

// renderResultsPane wraps the table (or empty-state / loading / error
// body) in a flat rectangle with a single label row. Row count appears
// dim-right of the label when there is data.
func renderResultsPane(body string, width, height, rowCount int, focused bool) string {
	title := "RESULTS"
	if rowCount > 0 {
		title = fmt.Sprintf("RESULTS (%d rows)", rowCount)
	}
	return renderTitledPane(title, body, width, height, focused)
}

// buildBottomBar — single combined help+status row. Left side carries
// the focus-aware key hints; right side carries the meta block (info
// from results, then MODE, then LIVE). Replaces the previous two
// separate bordered strips.
func buildBottomBar(m QueryModel, width int) string {
	p := ui.Active

	keyStyle := lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(p.Faint)
	sepStyle := lipgloss.NewStyle().Foreground(p.BorderSoft)

	// ── Line 1: shortcuts ─────────────────────────────────────────
	hints := queryKeysForFocus(m)
	const pad = 1
	const sep = "    "
	innerW := width - pad*2
	if innerW < 1 {
		innerW = 1
	}
	padding := strings.Repeat(" ", pad)

	var keyParts []string
	used := 0
	for _, h := range hints {
		k := strings.TrimSuffix(strings.TrimPrefix(h.Key, "<"), ">")
		part := keyStyle.Render("<"+k+">") + labelStyle.Render(" "+strings.ToLower(h.Label))
		need := lipgloss.Width(part)
		if used > 0 {
			need += len(sep)
		}
		if used+need > innerW {
			break
		}
		keyParts = append(keyParts, part)
		used += need
	}
	shortcutsLine := padding + strings.Join(keyParts, sep)

	// ── Line 2: hairline ──────────────────────────────────────────
	divider := sepStyle.Render(strings.Repeat("─", width))

	// ── Line 3: Parseable <url> left · status+MODE right ─────────
	connLeft := lipgloss.JoinHorizontal(lipgloss.Bottom,
		lipgloss.NewStyle().Foreground(p.Accent).Bold(true).Render("Parseable"),
		"  ",
		labelStyle.Render(m.profile.URL),
	)
	var rightParts []string
	if m.status.Error != "" {
		rightParts = append(rightParts,
			lipgloss.NewStyle().Foreground(p.Err).Bold(true).Render(m.status.Error),
			sepStyle.Render(" │ "),
		)
	}
	rightParts = append(rightParts,
		labelStyle.Render("QUERY"),
		" ",
		lipgloss.NewStyle().Foreground(p.Accent).Bold(true).Render(strings.ToUpper(m.status.title)),
	)
	connRight := lipgloss.JoinHorizontal(lipgloss.Bottom, rightParts...)
	gap := innerW - lipgloss.Width(connLeft) - lipgloss.Width(connRight)
	if gap < 1 {
		gap = 1
	}
	statusLine := padding + connLeft + strings.Repeat(" ", gap) + connRight + padding

	return lipgloss.JoinVertical(lipgloss.Left, shortcutsLine, divider, statusLine)
}

// queryKeysForFocus returns the keybind hints shown in the HeaderStrip
// based on which pane is focused. Mirrors what bubbles help did before
// the chrome refactor — context-aware help is back.
func queryKeysForFocus(m QueryModel) []ui.KeyHint {
	common := []ui.KeyHint{
		{Key: "<tab>", Label: "Next pane"},
		{Key: "<shift+tab>", Label: "Prev pane"},
		{Key: "<ctrl-r>", Label: "Run"},
		{Key: "<ctrl-c>", Label: "Quit"},
	}
	switch m.overlay {
	case overlayInputs:
		upDownLabel := "Adjust"
		if m.timeRangeEdit.currentFocus() == "list" {
			upDownLabel = "Preset"
		} else if m.timeRangeEdit.currentFocus() == "display" {
			upDownLabel = "Display time"
		}
		return []ui.KeyHint{
			{Key: "<↑/↓>", Label: upDownLabel},
			{Key: "<tab>", Label: "Switch pane"},
			{Key: "<enter>", Label: "Apply"},
			{Key: "<esc>", Label: "Cancel"},
		}
	}
	switch m.currentFocus() {
	case "dataset":
		return append([]ui.KeyHint{
			{Key: "<enter>", Label: "Open selector"},
			{Key: "<ctrl-d>", Label: "Open selector"},
		}, common...)
	case "columns":
		if m.columnsDrawerOpen {
			return append([]ui.KeyHint{
				{Key: "<↑/↓>", Label: "Column"},
				{Key: "<enter>", Label: "Insert"},
				{Key: "<type>", Label: "Filter"},
			}, common...)
		}
		return append([]ui.KeyHint{
			{Key: "<enter>", Label: "Show columns"},
		}, common...)
	case "time":
		return append([]ui.KeyHint{
			{Key: "<enter>", Label: "Open picker"},
		}, common...)
	case "table":
		return append([]ui.KeyHint{
			{Key: "<↑/↓>", Label: "Row"},
			{Key: "<shift+↑/↓>", Label: "Page"},
			{Key: "</>", Label: "Filter"},
		}, common...)
	}
	return common
}

func formatResultTimeLabel(mode TimeDisplayMode) string {
	if mode == TimeDisplayUTC {
		return "[UTC]"
	}
	return "[LOCAL]"
}

func formatTimestampToDisplayHMS(value string, loc *time.Location, mode TimeDisplayMode) string {
	if loc == nil {
		loc = time.Local
	}
	if t, ok := parseTimestamp(value); ok {
		if mode == TimeDisplayUTC {
			return t.UTC().Format("15:04:05")
		}
		return t.In(loc).Format("15:04:05")
	}
	return trimTimestampToHMS(value)
}

func parseTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true
		}
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func trimTimestampToHMS(value string) string {
	t := strings.IndexByte(value, 'T')
	if t < 0 {
		t = strings.IndexByte(value, ' ')
	}
	if t < 0 || t+1 >= len(value) {
		return value
	}
	rest := value[t+1:]
	for i, c := range rest {
		if c == '.' || c == 'Z' || c == '+' || c == '-' {
			return rest[:i]
		}
	}
	if len(rest) >= 8 {
		return rest[:8]
	}
	return rest
}

func ensureDefaultSQLLimit(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" || hasTopLevelSQLLimit(trimmed) {
		return query
	}
	suffix := ""
	if strings.HasSuffix(trimmed, ";") {
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, ";"))
		suffix = ";"
	}
	separator := " "
	if endsInSQLLineComment(trimmed) {
		separator = "\n"
	}
	return trimmed + separator + "LIMIT 500" + suffix
}

func buildSQLQueryPlan(query string, defaultLimit int) sqlQueryPlan {
	base, limit, offset := stripTopLevelSQLLimitOffset(query)
	if strings.TrimSpace(base) == "" {
		base = strings.TrimSpace(query)
	}
	if limit < 0 {
		limit = defaultLimit
	}
	return sqlQueryPlan{
		baseQuery:  base,
		userLimit:  limit,
		userOffset: offset,
	}
}

func injectSQLWindow(query string, limit, offset int) string {
	base, _, _ := stripTopLevelSQLLimitOffset(query)
	base = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(base), ";"))
	if offset < 0 {
		offset = 0
	}
	return fmt.Sprintf("%s LIMIT %d OFFSET %d", base, limit, offset)
}

func stripTopLevelSQLLimitOffset(query string) (base string, limit int, offset int) {
	limit = -1
	offset = 0
	limitPos, offsetPos := findTopLevelSQLLimitOffset(query)
	stripPos := -1
	if limitPos >= 0 {
		stripPos = limitPos
		limit = readSQLClauseInt(query, limitPos+len("limit"))
	}
	if offsetPos >= 0 {
		if stripPos < 0 || offsetPos < stripPos {
			stripPos = offsetPos
		}
		if parsed := readSQLClauseInt(query, offsetPos+len("offset")); parsed >= 0 {
			offset = parsed
		}
	}
	if stripPos < 0 {
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(query), ";")), limit, offset
	}
	suffix := ""
	trimmed := strings.TrimSpace(query)
	if strings.HasSuffix(trimmed, ";") {
		suffix = ";"
	}
	base = strings.TrimSpace(query[:stripPos])
	base = strings.TrimSpace(strings.TrimSuffix(base, ";"))
	if suffix != "" {
		base = strings.TrimSpace(strings.TrimSuffix(base, ";"))
	}
	return base, limit, offset
}

func findTopLevelSQLLimitOffset(query string) (limitPos int, offsetPos int) {
	limitPos, offsetPos = -1, -1
	depth := 0
	for i := 0; i < len(query); {
		switch query[i] {
		case '\'':
			i = skipSQLString(query, i)
		case '"':
			i = skipSQLQuotedIdentifier(query, i)
		case '-':
			if i+1 < len(query) && query[i+1] == '-' {
				i += 2
				for i < len(query) && query[i] != '\n' {
					i++
				}
				continue
			}
			i++
		case '/':
			if i+1 < len(query) && query[i+1] == '*' {
				i += 2
				for i+1 < len(query) {
					if query[i] == '*' && query[i+1] == '/' {
						i += 2
						break
					}
					i++
				}
				continue
			}
			i++
		case '(':
			depth++
			i++
		case ')':
			if depth > 0 {
				depth--
			}
			i++
		default:
			if depth == 0 {
				if limitPos < 0 && isSQLLimitTokenAt(query, i) {
					limitPos = i
					i += len("limit")
					continue
				}
				if offsetPos < 0 && isSQLKeywordAt(query, i, "offset") {
					offsetPos = i
					i += len("offset")
					continue
				}
			}
			i++
		}
	}
	return limitPos, offsetPos
}

func readSQLClauseInt(query string, pos int) int {
	for pos < len(query) && isSQLSpace(query[pos]) {
		pos++
	}
	start := pos
	for pos < len(query) && query[pos] >= '0' && query[pos] <= '9' {
		pos++
	}
	if start == pos {
		return -1
	}
	value, err := strconv.Atoi(query[start:pos])
	if err != nil {
		return -1
	}
	return value
}

func skipSQLString(query string, start int) int {
	i := start + 1
	for i < len(query) {
		if query[i] == '\'' {
			i++
			if i < len(query) && query[i] == '\'' {
				i++
				continue
			}
			break
		}
		i++
	}
	return i
}

func skipSQLQuotedIdentifier(query string, start int) int {
	i := start + 1
	for i < len(query) {
		if query[i] == '"' {
			i++
			if i < len(query) && query[i] == '"' {
				i++
				continue
			}
			break
		}
		i++
	}
	return i
}

func hasTopLevelSQLLimit(query string) bool {
	depth := 0
	for i := 0; i < len(query); {
		switch query[i] {
		case '\'':
			i++
			for i < len(query) {
				if query[i] == '\'' {
					i++
					if i < len(query) && query[i] == '\'' {
						i++
						continue
					}
					break
				}
				i++
			}
		case '"':
			i++
			for i < len(query) {
				if query[i] == '"' {
					i++
					break
				}
				i++
			}
		case '-':
			if i+1 < len(query) && query[i+1] == '-' {
				i += 2
				for i < len(query) && query[i] != '\n' {
					i++
				}
				continue
			}
			i++
		case '/':
			if i+1 < len(query) && query[i+1] == '*' {
				i += 2
				for i+1 < len(query) {
					if query[i] == '*' && query[i+1] == '/' {
						i += 2
						break
					}
					i++
				}
				continue
			}
			i++
		case '(':
			depth++
			i++
		case ')':
			if depth > 0 {
				depth--
			}
			i++
		default:
			if depth == 0 && isSQLLimitTokenAt(query, i) {
				return true
			}
			i++
		}
	}
	return false
}

func isSQLLimitTokenAt(query string, idx int) bool {
	const token = "limit"
	if idx+len(token) > len(query) || !strings.EqualFold(query[idx:idx+len(token)], token) {
		return false
	}
	if idx > 0 && isIdentChar(rune(query[idx-1])) {
		return false
	}
	next := idx + len(token)
	return next >= len(query) || !isIdentChar(rune(query[next]))
}

func endsInSQLLineComment(query string) bool {
	inSingleQuote := false
	inDoubleQuote := false
	inBlockComment := false
	lineCommentStart := -1

	for i := 0; i < len(query); i++ {
		if inSingleQuote {
			if query[i] == '\'' {
				if i+1 < len(query) && query[i+1] == '\'' {
					i++
					continue
				}
				inSingleQuote = false
			}
			continue
		}
		if inDoubleQuote {
			if query[i] == '"' {
				if i+1 < len(query) && query[i+1] == '"' {
					i++
					continue
				}
				inDoubleQuote = false
			}
			continue
		}
		if inBlockComment {
			if query[i] == '*' && i+1 < len(query) && query[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		switch query[i] {
		case '\'':
			inSingleQuote = true
		case '"':
			inDoubleQuote = true
		case '-':
			if i+1 < len(query) && query[i+1] == '-' {
				lineCommentStart = i
				i++
			}
		case '/':
			if i+1 < len(query) && query[i+1] == '*' {
				inBlockComment = true
				i++
			}
		case '\n', '\r':
			lineCommentStart = -1
		}
	}
	if lineCommentStart < 0 {
		return false
	}
	return strings.TrimSpace(query[lineCommentStart+2:]) != ""
}

// parseableASCIIArt is the block-letter wordmark shown in the empty
// state. Five rows tall, ~58 cells wide. Rendered in Accent.
const parseableASCIIArt = ` ____   _    ____  ____  _____    _    ____  _     _____
|  _ \ / \  |  _ \/ ___|| ____|  / \  | __ )| |   | ____|
| |_) / _ \ | |_) \___ \|  _|   / _ \ |  _ \| |   |  _|
|  __/ ___ \|  _ < ___) | |___ / ___ \| |_) | |___| |___
|_| /_/   \_\_| \_\____/|_____/_/   \_\____/|_____|_____|`

func applyEditorStyles(t *textarea.Model) {
	p := ui.Active

	t.FocusedStyle.Base = lipgloss.NewStyle().Foreground(p.Mute)
	t.FocusedStyle.Text = lipgloss.NewStyle().Foreground(p.Mute)
	t.FocusedStyle.LineNumber = lipgloss.NewStyle().
		Foreground(p.Faint).
		PaddingRight(1)
	t.FocusedStyle.CursorLine = lipgloss.NewStyle()
	t.FocusedStyle.CursorLineNumber = lipgloss.NewStyle().
		Foreground(p.Accent).
		Bold(true).
		PaddingRight(1)
	t.FocusedStyle.Placeholder = lipgloss.NewStyle().
		Foreground(p.Ghost).
		Italic(true)
	t.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(p.Accent)
	t.FocusedStyle.EndOfBuffer = lipgloss.NewStyle()

	t.BlurredStyle.Base = lipgloss.NewStyle().Foreground(p.Mute)
	t.BlurredStyle.Text = lipgloss.NewStyle().Foreground(p.Mute)
	t.BlurredStyle.LineNumber = lipgloss.NewStyle().
		Foreground(p.Ghost).
		PaddingRight(1)
	t.BlurredStyle.CursorLine = lipgloss.NewStyle()
	t.BlurredStyle.CursorLineNumber = lipgloss.NewStyle().
		Foreground(p.Ghost).
		PaddingRight(1)
	t.BlurredStyle.Placeholder = lipgloss.NewStyle().
		Foreground(p.Ghost).
		Italic(true)
	t.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(p.Faint)
	t.BlurredStyle.EndOfBuffer = lipgloss.NewStyle()

	t.Cursor.Style = lipgloss.NewStyle().Background(p.Cursor)
	t.Cursor.TextStyle = lipgloss.NewStyle().Foreground(p.InvertText)
	t.Prompt = "  "
}

func extractDataset(sql string) string {
	low := strings.ToLower(sql)
	i := strings.Index(low, " from ")
	if i < 0 {
		i = strings.Index(low, "\nfrom ")
	}
	if i < 0 {
		return "—"
	}
	rest := strings.TrimSpace(sql[i+6:])
	if strings.HasPrefix(rest, `"`) {
		var b strings.Builder
		for i := 1; i < len(rest); i++ {
			if rest[i] == '"' {
				if i+1 < len(rest) && rest[i+1] == '"' {
					b.WriteByte('"')
					i++
					continue
				}
				return b.String()
			}
			b.WriteByte(rest[i])
		}
		return strings.TrimPrefix(rest, `"`)
	}
	if strings.HasPrefix(rest, `'`) {
		var b strings.Builder
		for i := 1; i < len(rest); i++ {
			if rest[i] == '\'' {
				if i+1 < len(rest) && rest[i+1] == '\'' {
					b.WriteByte('\'')
					i++
					continue
				}
				return b.String()
			}
			b.WriteByte(rest[i])
		}
		return strings.TrimPrefix(rest, `'`)
	}
	if sp := strings.IndexAny(rest, " ,;\n\t)"); sp > 0 {
		return rest[:sp]
	}
	return rest
}

type QueryData struct {
	Fields  []string                 `json:"fields"`
	Records []map[string]interface{} `json:"records"`
}

// resolveDatasetPlaceholder replaces the literal word "dataset" (case-insensitive)
// in a FROM clause with the actual selected dataset name.
func resolveDatasetPlaceholder(query, dataset string) string {
	if dataset == "" {
		return query
	}
	lower := strings.ToLower(query)
	idx := strings.Index(lower, " from ")
	if idx < 0 {
		return query
	}
	rest := strings.TrimSpace(query[idx+6:])
	restLower := strings.ToLower(rest)
	if strings.HasPrefix(restLower, "dataset") {
		after := rest[len("dataset"):]
		if len(after) == 0 || !isIdentChar(rune(after[0])) {
			query = query[:idx+6] + "'" + dataset + "'" + after
		}
	}
	return query
}

func isIdentChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

func quoteUnsafeSQLTableNames(query string) string {
	var result strings.Builder
	i, n := 0, len(query)
	for i < n {
		switch query[i] {
		case '\'':
			i = copySQLStringLiteral(&result, query, i)
		case '"':
			i = copySQLQuotedIdentifier(&result, query, i)
		default:
			if isSQLTableClauseAt(query, i) {
				j := i + sqlTableClauseLen(query, i)
				result.WriteString(query[i:j])
				i = j

				for i < n && isSQLSpace(query[i]) {
					result.WriteByte(query[i])
					i++
				}
				if i >= n {
					continue
				}
				if query[i] == '"' || query[i] == '\'' || query[i] == '(' {
					continue
				}

				start := i
				for i < n && !isSQLTableTokenEnd(query[i]) {
					i++
				}
				tableName := query[start:i]
				if shouldQuoteSQLTableName(tableName) {
					result.WriteByte('"')
					result.WriteString(strings.ReplaceAll(tableName, `"`, `""`))
					result.WriteByte('"')
				} else {
					result.WriteString(tableName)
				}
				continue
			}
			result.WriteByte(query[i])
			i++
		}
	}
	return result.String()
}

// quoteUnsafeSQLFieldNames matches the command-mode SQL normalizer: dotted
// columns like service.name and mixed-case columns like StatusCode must be
// quoted for DataFusion to read them as Parseable field names.
func quoteUnsafeSQLFieldNames(query string) string {
	var result strings.Builder
	i, n := 0, len(query)
	for i < n {
		ch := query[i]
		switch ch {
		case '\'':
			i = copySQLStringLiteral(&result, query, i)
		case '"':
			i = copySQLQuotedIdentifier(&result, query, i)
		default:
			if isSQLIdentifierStart(ch) {
				j := i + 1
				for j < n && isSQLIdentifierChar(query[j]) {
					j++
				}

				k, hasDot := j, false
				for k < n && query[k] == '.' && k+1 < n && isSQLIdentifierChar(query[k+1]) {
					hasDot = true
					k++
					for k < n && isSQLIdentifierChar(query[k]) {
						k++
					}
				}

				identifier := query[i:k]
				if hasDot || shouldQuoteSQLFieldName(identifier, query, k) {
					result.WriteByte('"')
					result.WriteString(identifier)
					result.WriteByte('"')
					i = k
				} else {
					result.WriteString(query[i:j])
					i = j
				}
				continue
			}
			result.WriteByte(ch)
			i++
		}
	}
	return result.String()
}

func copySQLStringLiteral(result *strings.Builder, query string, start int) int {
	result.WriteByte(query[start])
	i := start + 1
	for i < len(query) {
		c := query[i]
		result.WriteByte(c)
		i++
		if c == '\'' {
			if i < len(query) && query[i] == '\'' {
				result.WriteByte(query[i])
				i++
				continue
			}
			break
		}
	}
	return i
}

func copySQLQuotedIdentifier(result *strings.Builder, query string, start int) int {
	result.WriteByte(query[start])
	i := start + 1
	for i < len(query) {
		c := query[i]
		result.WriteByte(c)
		i++
		if c == '"' {
			if i < len(query) && query[i] == '"' {
				result.WriteByte(query[i])
				i++
				continue
			}
			break
		}
	}
	return i
}

func isSQLTableClauseAt(query string, idx int) bool {
	return isSQLKeywordAt(query, idx, "from") || isSQLKeywordAt(query, idx, "join")
}

func sqlTableClauseLen(query string, idx int) int {
	if isSQLKeywordAt(query, idx, "from") {
		return len("from")
	}
	return len("join")
}

func isSQLKeywordAt(query string, idx int, keyword string) bool {
	if idx+len(keyword) > len(query) || !strings.EqualFold(query[idx:idx+len(keyword)], keyword) {
		return false
	}
	if idx > 0 && isIdentChar(rune(query[idx-1])) {
		return false
	}
	next := idx + len(keyword)
	return next >= len(query) || !isIdentChar(rune(query[next]))
}

func isSQLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func isSQLTableTokenEnd(c byte) bool {
	return isSQLSpace(c) || c == ',' || c == ';' || c == '(' || c == ')'
}

func shouldQuoteSQLTableName(tableName string) bool {
	if tableName == "" || strings.EqualFold(tableName, "dataset") {
		return false
	}
	if !isSQLIdentifierStart(tableName[0]) {
		return true
	}
	for i := 1; i < len(tableName); i++ {
		if !isSQLIdentifierChar(tableName[i]) {
			return true
		}
	}
	return false
}

var sqlFieldKeywords = map[string]struct{}{
	"all": {}, "and": {}, "as": {}, "asc": {}, "between": {}, "by": {}, "case": {}, "cast": {},
	"desc": {}, "distinct": {}, "else": {}, "end": {}, "false": {}, "from": {}, "full": {},
	"group": {}, "having": {}, "in": {}, "inner": {}, "is": {}, "join": {}, "left": {}, "like": {},
	"limit": {}, "not": {}, "null": {}, "on": {}, "or": {}, "order": {}, "outer": {}, "right": {},
	"select": {}, "then": {}, "true": {}, "when": {}, "where": {},
}

func shouldQuoteSQLFieldName(identifier, query string, end int) bool {
	if _, ok := sqlFieldKeywords[strings.ToLower(identifier)]; ok {
		return false
	}
	if nextNonSpaceByte(query, end) == '(' {
		return false
	}
	return hasMixedCaseASCII(identifier)
}

func hasMixedCaseASCII(s string) bool {
	hasUpper, hasLower := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			hasUpper = true
		}
		if c >= 'a' && c <= 'z' {
			hasLower = true
		}
	}
	return hasUpper && hasLower
}

func nextNonSpaceByte(s string, start int) byte {
	for start < len(s) {
		if !isSQLSpace(s[start]) {
			return s[start]
		}
		start++
	}
	return 0
}

func isSQLIdentifierStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func isSQLIdentifierChar(c byte) bool {
	return isSQLIdentifierStart(c) || (c >= '0' && c <= '9')
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

		client := internalHTTP.DefaultClient(&profile)
		client.Client.Timeout = time.Second * 50

		data, status, errMsg := fetchData(&client, &profile, query, startTime, endTime)

		if status == fetchOk {
			res.data = data.Records
			res.schema = data.Fields
			res.status = fetchOk
		} else {
			res.errMsg = errMsg
		}

		return res
	}
}

func NewSQLWindowFetchTask(profile config.Profile, runID int, baseQuery string, baseOffset, windowSize, fetchLimit, windowIndex int, startTime string, endTime string) tea.Cmd {
	return func() (msg tea.Msg) {
		res := sqlWindowFetchMsg{
			runID:       runID,
			windowIndex: windowIndex,
			status:      fetchErr,
			schema:      []string{},
			data:        []map[string]interface{}{},
		}
		defer func() {
			if r := recover(); r != nil {
				res.errMsg = "query failed"
				msg = res
			}
		}()

		client := internalHTTP.DefaultClient(&profile)
		client.Client.Timeout = time.Second * 50
		query := injectSQLWindow(baseQuery, fetchLimit, baseOffset+windowIndex*windowSize)
		data, status, errMsg := fetchDataRaw(&client, &profile, query, startTime, endTime)
		if status == fetchOk {
			res.data = data.Records
			res.schema = data.Fields
			res.status = fetchOk
		} else {
			res.errMsg = errMsg
		}
		msg = res
		return msg
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

func fetchData(client *internalHTTP.HTTPClient, profile *config.Profile, query string, startTime string, endTime string) (data QueryData, res FetchResult, errMsg string) {
	query = quoteUnsafeSQLTableNames(query)
	query = quoteUnsafeSQLFieldNames(query)
	query = ensureDefaultSQLLimit(query)
	return fetchDataRaw(client, profile, query, startTime, endTime)
}

func fetchDataRaw(client *internalHTTP.HTTPClient, _ *config.Profile, query string, startTime string, endTime string) (data QueryData, res FetchResult, errMsg string) {
	data = QueryData{}
	res = fetchErr
	body, err := json.Marshal(map[string]string{
		"query":     query,
		"startTime": startTime,
		"endTime":   endTime,
	})
	if err != nil {
		errMsg = err.Error()
		return
	}

	req, err := client.NewRequest("POST", "query", bytes.NewBuffer(body))
	if err != nil {
		errMsg = err.Error()
		return
	}
	queryParams := req.URL.Query()
	queryParams.Set("fields", "true")
	req.URL.RawQuery = queryParams.Encode()

	resp, err := client.Client.Do(req)
	if err != nil {
		errMsg = err.Error()
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		errMsg = strings.TrimSpace(string(b))
		if errMsg == "" {
			errMsg = resp.Status
		}
		return
	}

	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		errMsg = err.Error()
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

	m.UpdateTableColumns(data.schema, data.data)

	m.dataRows = make([]table.Row, len(data.data))
	for i, rowJSON := range data.data {
		m.dataRows[i] = table.NewRow(m.formatSQLRow(rowJSON))
	}

	m.tableRowsBase = 0
	m.table = m.table.WithRows(m.dataRows)
}

func (m *QueryModel) UpdateTableColumns(schema []string, sample []map[string]interface{}) {
	if len(schema) == 0 {
		return
	}

	m.schema = schema

	// Build column specs: timestamp pinned left, p_tags/p_metadata pinned right.
	var specs []colSpec
	resultTimeLabel := formatResultTimeLabel(m.timeRange.DisplayMode())

	if slices.Contains(schema, dateTimeKey) {
		specs = append(specs, colSpec{key: dateTimeKey, title: "time " + resultTimeLabel, width: dateTimeWidth, fixed: true})
	}

	for _, title := range schema {
		switch title {
		case dateTimeKey, tagKey, metadataKey:
			continue
		default:
			w := inferWidthForColumns(title, &sample, 100, 100) + 1
			specs = append(specs, colSpec{key: title, title: title, width: w, filterable: true})
		}
	}

	if slices.Contains(schema, tagKey) {
		specs = append(specs, colSpec{key: tagKey, title: tagKey, width: inferWidthForColumns(tagKey, &sample, 100, 80), filterable: true})
	}

	if slices.Contains(schema, metadataKey) {
		specs = append(specs, colSpec{key: metadataKey, title: metadataKey, width: inferWidthForColumns(metadataKey, &sample, 100, 80), filterable: true})
	}

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

	// Build table.Columns from scaled specs. Header titles are
	// uppercased for visual weight + scanability (mirrors mock §5.3).
	columns := make([]table.Column, 0, len(specs))
	for _, s := range specs {
		col := table.NewColumn(s.key, strings.ToUpper(s.title), s.width)
		if s.filterable {
			col = col.WithFiltered(true)
		}
		columns = append(columns, col)
	}

	m.table = m.table.WithColumns(columns).WithMaxTotalWidth(m.width)
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

// fetchAllStreams fetches Prism's dataset metadata and keeps log datasets,
// matching the web UI's Logs section.
func fetchAllStreams(profile config.Profile) tea.Cmd {
	return func() tea.Msg {
		items, err := datasets.FetchHomeDatasets(profile)
		if err != nil {
			return datasetListMsg{errMsg: err.Error()}
		}
		return datasetListMsg{datasets: datasets.NamesByType(items, datasets.TypeLogs)}
	}
}

// renderSQLSpotlight renders the dataset picker overlay for the SQL model.
// Layout mirrors the PromQL spotlight so both views feel consistent.
func (m QueryModel) renderSQLSpotlight() string {
	p := ui.Active
	innerW := spotlightWidth - 6
	if innerW < 20 {
		innerW = 20
	}

	titleLeft := lipgloss.NewStyle().Foreground(p.Accent).Bold(true).Render("SELECT DATASET")
	countTxt := ""
	if !m.datasetsLoading {
		countTxt = fmt.Sprintf("%d datasets", len(m.filteredDatasets))
	}
	titleRight := lipgloss.NewStyle().Foreground(p.Faint).Render(countTxt)
	gap := innerW - lipgloss.Width(titleLeft) - lipgloss.Width(titleRight)
	if gap < 1 {
		gap = 1
	}
	header := titleLeft + strings.Repeat(" ", gap) + titleRight
	rule := lipgloss.NewStyle().Foreground(p.BorderSoft).Render(strings.Repeat("─", innerW))
	searchRow := lipgloss.NewStyle().Width(innerW).Render(m.spotlightFilter.View())

	var listLines []string
	switch {
	case m.datasetsLoading:
		listLines = append(listLines, lipgloss.NewStyle().
			Foreground(p.Faint).Width(innerW).Padding(1, 0).
			Render("  "+m.spinner.View()+" loading…"))
	case len(m.filteredDatasets) == 0:
		listLines = append(listLines, lipgloss.NewStyle().
			Foreground(p.Faint).Width(innerW).Padding(1, 0).
			Render("  no datasets found"))
	default:
		limit := len(m.filteredDatasets)
		if limit > spotlightMaxItems {
			limit = spotlightMaxItems
		}
		start := 0
		if m.datasetSelectedIdx >= spotlightMaxItems {
			start = m.datasetSelectedIdx - spotlightMaxItems + 1
		}
		rail := lipgloss.NewStyle().Background(p.Active).Render(" ")
		for i := start; i < start+limit && i < len(m.filteredDatasets); i++ {
			ds := m.filteredDatasets[i]
			if i == m.datasetSelectedIdx {
				row := rail + " " + lipgloss.NewStyle().
					Foreground(p.Active).Bold(true).Width(innerW-2).
					Render(ds)
				listLines = append(listLines, row)
			} else {
				row := "  " + lipgloss.NewStyle().
					Foreground(p.Body).Width(innerW-2).
					Render(ds)
				listLines = append(listLines, row)
			}
		}
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		header, rule, searchRow, rule,
		lipgloss.JoinVertical(lipgloss.Left, listLines...),
	)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Accent).
		Padding(0, 2).
		Render(body)
}

// renderSQLColumnSpotlight renders the column picker overlay for the SQL model.
func (m QueryModel) renderSQLColumnSpotlight() string {
	p := ui.Active
	innerW := spotlightWidth - 6
	if innerW < 20 {
		innerW = 20
	}

	titleLeft := lipgloss.NewStyle().Foreground(p.Accent).Bold(true).Render("SELECT COLUMN")
	countTxt := ""
	if !m.columnsLoading {
		countTxt = fmt.Sprintf("%d columns", len(m.filteredColumns))
	}
	titleRight := lipgloss.NewStyle().Foreground(p.Faint).Render(countTxt)
	gap := innerW - lipgloss.Width(titleLeft) - lipgloss.Width(titleRight)
	if gap < 1 {
		gap = 1
	}
	header := titleLeft + strings.Repeat(" ", gap) + titleRight
	rule := lipgloss.NewStyle().Foreground(p.BorderSoft).Render(strings.Repeat("─", innerW))
	searchRow := lipgloss.NewStyle().Width(innerW).Render(m.columnFilter.View())

	var listLines []string
	switch {
	case m.columnsLoading:
		listLines = append(listLines, lipgloss.NewStyle().
			Foreground(p.Faint).Width(innerW).Padding(1, 0).
			Render("  "+m.spinner.View()+" loading…"))
	case len(m.filteredColumns) == 0:
		listLines = append(listLines, lipgloss.NewStyle().
			Foreground(p.Faint).Width(innerW).Padding(1, 0).
			Render("  no columns — select a dataset first"))
	default:
		limit := len(m.filteredColumns)
		if limit > spotlightMaxItems {
			limit = spotlightMaxItems
		}
		start := 0
		if m.columnSelectedIdx >= spotlightMaxItems {
			start = m.columnSelectedIdx - spotlightMaxItems + 1
		}
		rail := lipgloss.NewStyle().Background(p.Active).Render(" ")
		for i := start; i < start+limit && i < len(m.filteredColumns); i++ {
			col := m.filteredColumns[i]
			if i == m.columnSelectedIdx {
				row := rail + " " + lipgloss.NewStyle().
					Foreground(p.Active).Bold(true).Width(innerW-2).
					Render(col)
				listLines = append(listLines, row)
			} else {
				row := "  " + lipgloss.NewStyle().
					Foreground(p.Body).Width(innerW-2).
					Render(col)
				listLines = append(listLines, row)
			}
		}
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		header, rule, searchRow, rule,
		lipgloss.JoinVertical(lipgloss.Left, listLines...),
	)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Accent).
		Padding(0, 2).
		Render(body)
}

// fetchStreamSchema fetches field names for the given stream.
// Uses a Parseable-native schema endpoint; falls back to a minimal query
// with a wide time window if the endpoint is unavailable.
func fetchStreamSchema(profile config.Profile, stream string) tea.Cmd {
	return func() tea.Msg {
		if stream == "" {
			return schemaMsg{}
		}
		client := internalHTTP.DefaultClient(&profile)
		client.Client.Timeout = 15 * time.Second

		// Primary: GET /api/v1/logstream/{stream}/schema
		schemaURL, _ := url.JoinPath(profile.URL, "api/v1/logstream", stream, "schema")
		req, err := http.NewRequest("GET", schemaURL, nil)
		if err == nil {
			if err := internalHTTP.AddAuthHeaders(req, &profile); err != nil {
				return schemaMsg{errMsg: err.Error()}
			}
			if resp, err := client.Client.Do(req); err == nil {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					// Arrow schema format: {"fields": [{"name": "col", ...}, ...]}
					var arrow struct {
						Fields []struct {
							Name string `json:"name"`
						} `json:"fields"`
					}
					if json.Unmarshal(body, &arrow) == nil && len(arrow.Fields) > 0 {
						names := make([]string, 0, len(arrow.Fields))
						for _, f := range arrow.Fields {
							if f.Name != "" {
								names = append(names, f.Name)
							}
						}
						if len(names) > 0 {
							return schemaMsg{columns: names}
						}
					}
				}
			}
		}

		// Fallback: query with single-quoted name and all-time range.
		// Stream names with hyphens (e.g. my-stream) are invalid bare SQL
		// identifiers, so single-quote them — same as resolveDatasetPlaceholder.
		endTime := time.Now().UTC().Format(time.RFC3339)
		data, res, errMsg := fetchData(&client, &profile,
			fmt.Sprintf("SELECT * FROM '%s' LIMIT 1", stream),
			"2000-01-01T00:00:00+00:00", endTime)
		if res != fetchOk {
			return schemaMsg{errMsg: errMsg}
		}
		return schemaMsg{columns: data.Fields}
	}
}
