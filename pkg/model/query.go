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
	"pb/pkg/config"
	"pb/pkg/iterator"
	"pb/pkg/ui"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	table "github.com/evertras/bubble-table/table"
	"golang.org/x/exp/slices"
	"golang.org/x/term"
)

const (
	// Trimmed display width — HH:MM:SS = 8 cells + slack.
	dateTimeWidth = 10
	dateTimeKey   = "p_timestamp"
	tagKey        = "p_tags"
	metadataKey   = "p_metadata"
)

// Theme-derived styles. All palette atoms come from pkg/ui — to swap a
// color, edit ui.Dark / ui.Light, not these vars.
var (
	FocusPrimary   = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Accent })
	FocusSecondary = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Accent2 })

	StandardPrimary   = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Body })
	StandardSecondary = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Mute })

	chromeBorder = ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Border })

	borderedStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder(), true).
			BorderForeground(chromeBorder).
			Padding(0)

	// Focused pane: single rounded border in brand accent. No double
	// border (read as "alert" in TUI) — accent color carries the focus
	// signal on its own.
	borderedFocusStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder(), true).
				BorderForeground(FocusPrimary).
				Padding(0)

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

	QueryNavigationMap = []string{"query", "dataset", "column", "time", "table"}
)

func sqlPageSize(totalH, bottomH int) int {
	const topH = 14
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
	}
}

func (m *QueryModel) currentFocus() string {
	return QueryNavigationMap[m.focused]
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
	model := QueryModel{
		width:           w,
		height:          h,
		table:           table,
		query:           query,
		timeRange:       inputs,
		overlay:         overlayNone,
		profile:         profile,
		help:            help,
		spinner:         sp,
		loading:         hasQuery,
		hasQueried:      hasQuery,
		queryIterator:   nil,
		status:          status,
		spotlightFilter: sf,
		columnFilter:    cf,
	}
	return model
}

func (m QueryModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick, fetchAllStreams(m.profile)}
	if strings.TrimSpace(m.query.Value()) != "" {
		cmds = append(cmds, NewFetchTask(m.profile, resolveColumnPlaceholder(resolveDatasetPlaceholder(m.query.Value(), m.dataset), m.selectedColumn), m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc()))
	}
	return tea.Batch(cmds...)
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
		}
		return m, nil

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
			m.focused = 4
			m.focusSelected()
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
					if selected != m.dataset {
						// dataset changed — reset column and fetch fresh schema
						m.dataset = selected
						m.selectedColumn = ""
						m.allColumns = []string{}
						m.filteredColumns = []string{}
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
				m.focused = 0 // focus query editor after dataset select
				m.focusSelected()
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
					m.query.InsertString("column ")
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

			if msg.Type == tea.KeyEnter && m.currentFocus() == "column" {
				m.overlay = overlayColumn
				m.columnFilter.Focus()
				return m, nil
			}

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

			if msg.Type == tea.KeyShiftTab {
				m.focused--
				if m.focused < 0 {
					m.focused = len(QueryNavigationMap) - 1
				}
				m.focusSelected()
				return m, nil
			}

			// up/down arrows navigate between dataset and column rows
			if m.currentFocus() == "dataset" && msg.Type == tea.KeyDown {
				m.focused = 2 // column
				m.focusSelected()
				return m, nil
			}
			if m.currentFocus() == "column" && msg.Type == tea.KeyUp {
				m.focused = 1 // dataset
				m.focusSelected()
				return m, nil
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
				m.overlay = overlayNone
				m.focusSelected()
				m.status.Error = ""
				m.status.Info = ""
				m.loading = true
				m.hasQueried = true
				return m, tea.Batch(m.spinner.Tick, NewFetchTask(m.profile, resolveColumnPlaceholder(resolveDatasetPlaceholder(m.query.Value(), m.dataset), m.selectedColumn), m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc()))
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
			return m, tea.Batch(m.spinner.Tick, NewFetchTask(m.profile, resolveColumnPlaceholder(resolveDatasetPlaceholder(m.query.Value(), m.dataset), m.selectedColumn), m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc()))
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

	// ── 3. TOP row: editor (wide) + date (narrow). Plain rectangles,
	// label-only chrome. Date pane stays compact per mock.
	// Sidebar holds DATASET + FROM + TO = 8 content rows; topH must
	// stay >= 11 (innerH = 9 fits 8 + spare) or the sidebar overflows
	// and pushes the top border off-screen.
	// Sidebar width matches PromQL so the two views read symmetric.
	sidebarW := 30
	if m.width >= 140 {
		sidebarW = 34
	}
	if m.width < 100 {
		sidebarW = 26
	}

	// topH = dateBox(7) + combined dataset+column box(7) = 14
	const topH = 14

	// editorW reserves 1 col for the horizontal gap between editor
	// and sidebar, so the two │ borders aren't flush against each other.
	editorW := m.width - sidebarW - 1
	if editorW < 30 {
		editorW = 30
		sidebarW = m.width - editorW - 1
	}
	m.query.SetWidth(editorW - 6)
	editorBodyH := topH - 4 // border(2) + title(1) + spacer(1)
	if editorBodyH < 1 {
		editorBodyH = 1
	}
	m.query.SetHeight(editorBodyH)
	editorPane := renderEditorPane(m.query.View(), editorW, topH, m.currentFocus() == "query")

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

	// Two stacked sidebar boxes — DATE on top, combined DATASET+COLUMN below.
	dateBox := renderSQLDateBox(
		m.timeRange.start.Value(),
		m.timeRange.end.Value(),
		sidebarW, 7,
		m.currentFocus() == "time",
	)
	colLabel := m.selectedColumn
	if colLabel == "" {
		colLabel = "<select column>"
	}
	datasetColumnBox := renderSQLDatasetColumnBox(
		dataset, colLabel, m.columnsLoading,
		sidebarW, 7,
		m.currentFocus(),
	)
	sidebarPane := lipgloss.JoinVertical(lipgloss.Left, datasetColumnBox, dateBox)

	gap := lipgloss.NewStyle().Width(1).Height(topH).Render("")
	topSection := lipgloss.JoinHorizontal(lipgloss.Top, editorPane, gap, sidebarPane)

	// ── 4. Results / table area ───────────────────────────────────────
	availH := m.height - crumbsHeight - topH - bottomHeight
	if availH < 6 {
		availH = 6
	}
	// Results pane border (2) + label row (1) = 3 rows of chrome.
	resultsInnerH := availH - 3
	if resultsInnerH < 3 {
		resultsInnerH = 3
	}
	resultsInnerW := m.width - 4 // border(2) + h-padding(2)
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
		if m.table.MaxPages() > 1 {
			leftPart = fmt.Sprintf("%d/%d", m.table.CurrentPage(), m.table.MaxPages())
		}
		if len(m.dataRows) > 0 {
			curRow := m.table.GetHighlightedRowIndex() + 1
			rightPart = fmt.Sprintf("%d | %d rows", curRow, len(m.dataRows))
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
	resultsPane := renderResultsPane(inner, m.width, availH, 0, m.currentFocus() == "table")

	// ── 5. Compose body or overlay ────────────────────────────────────
	body := lipgloss.JoinVertical(lipgloss.Left, topSection, resultsPane)
	var mainView string
	switch m.overlay {
	case overlayNone:
		mainView = body
	case overlayInputs:
		timeView := m.timeRange.View()
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

// renderEditorPane draws a flat rectangle with a single label row
// (Faint, top-left) and body below. NormalBorder per design — matches
// the wireframe "plain rectangle with label inside" idiom.
func renderEditorPane(body string, width, height int, focused bool) string {
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
	title := lipgloss.NewStyle().Foreground(titleFg).Bold(focused).Render("EDITOR")
	titleRow := lipgloss.NewStyle().Width(innerW).Padding(0, 1).Render(title)
	spacer := lipgloss.NewStyle().Width(innerW).Render("")
	bodyPane := lipgloss.NewStyle().
		Width(innerW).
		Height(innerH-2).
		Padding(0, 1).
		Render(body)
	stack := lipgloss.JoinVertical(lipgloss.Left, titleRow, spacer, bodyPane)
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Render(stack)
}

// renderSQLDatasetColumnBox draws a single sidebar card with both DATASET and
// COLUMN rows. focusedOn is "dataset", "column", or "" (neither focused).
// The active-rail highlight tracks which row is focused independently.
func renderSQLDatasetColumnBox(dataset, column string, columnsLoading bool, width, height int, focusedOn string) string {
	p := ui.Active
	innerW := width - 2
	if innerW < 4 {
		innerW = 4
	}
	innerH := height - 2
	if innerH < 1 {
		innerH = 1
	}

	rail := lipgloss.NewStyle().Background(p.Active).Render(" ")
	dim := lipgloss.NewStyle().Foreground(p.Faint)
	body := lipgloss.NewStyle().Foreground(p.Body)
	ghost := lipgloss.NewStyle().Foreground(p.Ghost).Italic(true)
	active := lipgloss.NewStyle().Foreground(p.Active).Bold(true)

	// DATASET row
	dsLabel, dsPrefix := dim, "  "
	if focusedOn == "dataset" {
		dsLabel = active
		dsPrefix = rail + " "
	}

	// COLUMN row
	colLabel, colPrefix := dim, "  "
	if focusedOn == "column" {
		colLabel = active
		colPrefix = rail + " "
	}
	colDisplay := column
	colValStyle := body
	if columnsLoading {
		colDisplay = "loading…"
		colValStyle = ghost
	} else if column == "<select column>" {
		colValStyle = ghost
	}

	maxVal := innerW - lipgloss.Width(dsPrefix)
	if maxVal < 4 {
		maxVal = 4
	}
	lines := []string{
		dsPrefix + dsLabel.Render("DATASET"),
		dsPrefix + body.Render(ui.Truncate(dataset, maxVal)),
		"",
		colPrefix + colLabel.Render("COLUMN"),
		colPrefix + colValStyle.Render(ui.Truncate(colDisplay, maxVal)),
	}
	content := lipgloss.NewStyle().
		Width(innerW).
		Height(innerH).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))

	borderColor := p.Border
	if focusedOn == "dataset" || focusedOn == "column" {
		borderColor = p.BorderHi
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Render(content)
}

// renderSQLDateBox draws the bottom SQL sidebar card: FROM + TO.
// Focused state uses the Active (sky-blue) rail + bold label
// convention shared with PromQL.
func renderSQLDateBox(start, end string, width, height int, focused bool) string {
	p := ui.Active
	innerW := width - 2
	if innerW < 4 {
		innerW = 4
	}
	innerH := height - 2
	if innerH < 1 {
		innerH = 1
	}
	dim := lipgloss.NewStyle().Foreground(p.Faint)
	val := lipgloss.NewStyle().Foreground(p.Body)
	label := dim
	prefix := "  "
	if focused {
		label = lipgloss.NewStyle().Foreground(p.Active).Bold(true)
		prefix = lipgloss.NewStyle().Background(p.Active).Render(" ") + " "
	}
	lines := []string{
		prefix + label.Render("FROM"),
		prefix + val.Render(start),
		"",
		prefix + label.Render("TO"),
		prefix + val.Render(end),
	}
	body := lipgloss.NewStyle().
		Width(innerW).
		Height(innerH).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(p.Border).
		Render(body)
}

// renderResultsPane wraps the table (or empty-state / loading / error
// body) in a flat rectangle with a single label row. Row count appears
// dim-right of the label when there is data.
func renderResultsPane(body string, width, height, rowCount int, focused bool) string {
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
	left := lipgloss.NewStyle().Foreground(titleFg).Bold(focused).Render("RESULTS")
	var right string
	if rowCount > 0 {
		right = lipgloss.NewStyle().
			Foreground(p.Faint).
			Render(fmt.Sprintf("%d rows", rowCount))
	}
	gap := innerW - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	titleRow := lipgloss.NewStyle().Width(innerW).Padding(0, 1).Render(
		left + strings.Repeat(" ", gap) + right,
	)
	bodyPane := lipgloss.NewStyle().
		Width(innerW).
		Height(innerH-1).
		Padding(0, 1).
		Render(body)
	stack := lipgloss.JoinVertical(lipgloss.Left, titleRow, bodyPane)
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Render(stack)
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
	var keyParts []string
	for _, h := range hints {
		k := strings.TrimSuffix(strings.TrimPrefix(h.Key, "<"), ">")
		keyParts = append(keyParts,
			keyStyle.Render("<"+k+">")+labelStyle.Render(" "+strings.ToLower(h.Label)),
		)
	}
	const pad = 1
	innerW := width - pad*2
	if innerW < 1 {
		innerW = 1
	}
	padding := strings.Repeat(" ", pad)

	shortcutsLine := padding + strings.Join(keyParts, "    ")

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
		labelStyle.Render("MODE"),
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
		return []ui.KeyHint{
			{Key: "<↑/↓>", Label: "Preset"},
			{Key: "<tab>", Label: "Field"},
			{Key: "<ctrl-{>", Label: "End → now"},
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
	case "column":
		return append([]ui.KeyHint{
			{Key: "<enter>", Label: "Open selector"},
			{Key: "<ctrl-d>", Label: "Dataset"},
		}, common...)
	case "time":
		return append([]ui.KeyHint{
			{Key: "<enter>", Label: "Open picker"},
		}, common...)
	case "table":
		return append([]ui.KeyHint{
			{Key: "<↑/↓>", Label: "Row"},
			{Key: "</>", Label: "Filter"},
		}, common...)
	}
	return common
}

// trimTimestampToHMS extracts the HH:MM:SS portion of an RFC3339-ish
// timestamp (or any string containing `T<time>`). Used to keep the
// timestamp column narrow in the results table. Full value is still
// stored — only the display string is trimmed.
func trimTimestampToHMS(s string) string {
	// Look for `T` separator (RFC3339) — take what follows, then crop
	// at the first dot or zone marker.
	t := strings.IndexByte(s, 'T')
	if t < 0 {
		// Fallback: try a space (some formats use space, not T).
		t = strings.IndexByte(s, ' ')
	}
	if t < 0 || t+1 >= len(s) {
		return s
	}
	rest := s[t+1:]
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

// resolveColumnPlaceholder replaces the literal word "column" in the query
// with the selected column name wrapped in double quotes — mirrors how
// resolveDatasetPlaceholder works for the dataset placeholder.
func resolveColumnPlaceholder(query, column string) string {
	if column == "" {
		return query
	}
	return strings.ReplaceAll(query, "column", `"`+column+`"`)
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

		data, status, errMsg := fetchData(client, &profile, query, startTime, endTime)

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

func fetchData(client *http.Client, profile *config.Profile, query string, startTime string, endTime string) (data QueryData, res FetchResult, errMsg string) {
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

	endpoint, _ := url.JoinPath(profile.URL, "api/v1/query")
	endpoint += "?fields=true"
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(body))
	if err != nil {
		errMsg = err.Error()
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

	m.schema = data.schema

	// Build column specs: timestamp pinned left, p_tags/p_metadata pinned right.
	var specs []colSpec

	if slices.Contains(data.schema, dateTimeKey) {
		// Display label "time" — full p_timestamp gets truncated to
		// `P_TIMESTA…` at width 10. Short label fits cleanly.
		specs = append(specs, colSpec{key: dateTimeKey, title: "time", width: dateTimeWidth, fixed: true})
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

	m.dataRows = make([]table.Row, len(data.data))
	for i, rowJSON := range data.data {
		// Mutate timestamp display in-place — HH:MM:SS only. Full value
		// stays accessible when the row is expanded.
		if ts, ok := rowJSON[dateTimeKey].(string); ok {
			rowJSON[dateTimeKey] = trimTimestampToHMS(ts)
		}
		m.dataRows[i] = table.NewRow(rowJSON)
	}

	m.table = m.table.WithColumns(columns).WithMaxTotalWidth(m.width)
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

// fetchAllStreams fetches every log stream from the server and returns them
// as a datasetListMsg. Used by the SQL dataset spotlight (all streams are
// valid SQL targets, unlike PromQL which prefers "metrics" streams).
func fetchAllStreams(profile config.Profile) tea.Cmd {
	return func() tea.Msg {
		reqURL, err := url.JoinPath(profile.URL, "api/v1/logstream")
		if err != nil {
			return datasetListMsg{errMsg: err.Error()}
		}
		client := &http.Client{Timeout: 15 * time.Second}
		req, err := http.NewRequest("GET", reqURL, nil)
		if err != nil {
			return datasetListMsg{errMsg: err.Error()}
		}
		if profile.Token != "" {
			req.Header.Set("Authorization", "Bearer "+profile.Token)
		} else {
			req.SetBasicAuth(profile.Username, profile.Password)
		}
		resp, err := client.Do(req)
		if err != nil {
			return datasetListMsg{errMsg: err.Error()}
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var items []struct {
			Name       string `json:"name"`
			StreamType string `json:"stream_type"`
		}
		if err := json.Unmarshal(body, &items); err != nil {
			return datasetListMsg{errMsg: err.Error()}
		}
		// Prefer server-side stream_type (matches exactly what the UI shows).
		// Fall back to name-based exclusion only for servers that don't
		// return stream_type — never return everything unfiltered.
		hasType := false
		for _, item := range items {
			if item.StreamType != "" {
				hasType = true
				break
			}
		}
		datasets := make([]string, 0, len(items))
		for _, item := range items {
			if hasType {
				if item.StreamType != "UserDefined" {
					continue
				}
			} else {
				low := strings.ToLower(item.Name)
				if strings.Contains(low, "traces") || strings.Contains(low, "metrics") {
					continue
				}
			}
			datasets = append(datasets, item.Name)
		}
		sort.Strings(datasets)
		return datasetListMsg{datasets: datasets}
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
		client := &http.Client{Timeout: 15 * time.Second}

		// Primary: GET /api/v1/logstream/{stream}/schema
		schemaURL, _ := url.JoinPath(profile.URL, "api/v1/logstream", stream, "schema")
		req, err := http.NewRequest("GET", schemaURL, nil)
		if err == nil {
			if profile.Token != "" {
				req.Header.Set("Authorization", "Bearer "+profile.Token)
			} else {
				req.SetBasicAuth(profile.Username, profile.Password)
			}
			if resp, err := client.Do(req); err == nil {
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
		data, res, errMsg := fetchData(client, &profile,
			fmt.Sprintf("SELECT * FROM '%s' LIMIT 1", stream),
			"2000-01-01T00:00:00+00:00", endTime)
		if res != fetchOk {
			return schemaMsg{errMsg: errMsg}
		}
		return schemaMsg{columns: data.Fields}
	}
}
