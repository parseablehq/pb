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
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
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

	baseStyle               = lipgloss.NewStyle().BorderForeground(chromeBorder)
	baseBoldUnderlinedStyle = lipgloss.NewStyle().BorderForeground(chromeBorder).Bold(true)
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

	additionalKeyBinds = []key.Binding{runQueryKey}

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
	errMsg string
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
	spinner       spinner.Model
	loading       bool
	hasQueried    bool // true once the first query has been dispatched
	queryIterator *iterator.QueryIterator[QueryData, FetchResult]
	overlay       uint
	focused       int
	dataRows      []table.Row // actual data rows (without padding)
	fetchErrMsg   string      // last fetch error, shown in the result area
}

func (m *QueryModel) focusSelected() {
	m.query.Blur()
	m.table = m.table.Focused(false)

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
		HighlightStyle(highlightStyle).
		WithMissingDataIndicatorStyled(table.StyledCell{
			// Near-invisible nulls — sits at Border, lets real data pop.
			Style: lipgloss.NewStyle().Foreground(chromeBorder),
			Data:  "—",
		}).WithMaxTotalWidth(w)

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

	hasQuery := strings.TrimSpace(queryStr) != ""
	model := QueryModel{
		width:         w,
		height:        h,
		table:         table,
		query:         query,
		timeRange:     inputs,
		overlay:       overlayNone,
		profile:       profile,
		help:          help,
		spinner:       sp,
		loading:       hasQuery,
		hasQueried:    hasQuery,
		queryIterator: nil,
		status:        status,
	}
	return model
}

func (m QueryModel) Init() tea.Cmd {
	if strings.TrimSpace(m.query.Value()) == "" {
		return m.spinner.Tick
	}
	return tea.Batch(
		m.spinner.Tick,
		NewFetchTask(m.profile, m.query.Value(), m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc()),
	)
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
		m.table = m.table.WithMaxTotalWidth(m.width)
		return m, nil

	case FetchData:
		m.loading = false
		m.status.Info = ""
		if msg.status == fetchOk {
			m.fetchErrMsg = ""
			m.UpdateTable(msg)
			m.status.Error = ""
			m.status.Info = fmt.Sprintf("%d rows", len(m.dataRows))
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

			if msg.Type == tea.KeyShiftTab {
				m.focused--
				if m.focused < 0 {
					m.focused = len(QueryNavigationMap) - 1
				}
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
				return m, tea.Batch(m.spinner.Tick, NewFetchTask(m.profile, m.query.Value(), m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc()))
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
			return m, tea.Batch(m.spinner.Tick, NewFetchTask(m.profile, m.query.Value(), m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc()))
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
	if len(m.dataRows) > 0 {
		page := m.table.CurrentPage()
		total := m.table.MaxPages()
		if total < 1 {
			total = 1
		}
		m.status.Info = fmt.Sprintf("rows %d · %d/%d", len(m.dataRows), page, total)
	}
	bottomView := buildBottomBar(m, m.width)
	bottomHeight := lipgloss.Height(bottomView)

	// ── 3. TOP row: editor (wide) + date (narrow). Plain rectangles,
	// label-only chrome. Date pane stays compact per mock.
	// Sidebar holds DATASET + FROM + TO = 8 content rows; topH must
	// stay >= 11 (innerH = 9 fits 8 + spare) or the sidebar overflows
	// and pushes the top border off-screen.
	topH := 11
	// Sidebar width matches PromQL so the two views read symmetric.
	sidebarW := 30
	if m.width >= 140 {
		sidebarW = 34
	}
	if m.width < 100 {
		sidebarW = 26
	}
	// editorW reserves 1 col for the horizontal gap between editor
	// and date pane, so the two `│` borders aren't flush against
	// each other.
	editorW := m.width - sidebarW - 1
	if editorW < 30 {
		editorW = 30
		sidebarW = m.width - editorW - 1
	}
	m.query.SetWidth(editorW - 6)
	// Editor body height must match the pane's inner content area —
	// innerH(topH-2) minus the title row (1). If textarea is taller
	// than the pane, it overflows and pushes the top borders/labels
	// off-screen.
	editorBodyH := topH - 4 // border(2) + title(1) + spacer(1)
	if editorBodyH < 1 {
		editorBodyH = 1
	}
	m.query.SetHeight(editorBodyH)
	editorPane := renderEditorPane(m.query.View(), editorW, topH, m.currentFocus() == "query")
	dataset := extractDataset(m.query.Value())
	if dataset == "—" || dataset == "" {
		dataset = "—"
	}
	// Two stacked sidebar boxes — DATASET (read-only) on top, DATE
	// (FROM/TO) below. Borders touch (same zero-gap join used
	// between the top section and the results pane).
	// Controls(4) + Date(7) = 11 = topH.
	datasetBox := renderSQLDatasetBox(dataset, sidebarW, 4)
	dateBox := renderSQLDateBox(
		m.timeRange.start.Value(),
		m.timeRange.end.Value(),
		sidebarW, 7,
		m.currentFocus() == "time",
	)
	sidebarPane := lipgloss.JoinVertical(lipgloss.Left, datasetBox, dateBox)

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
	pageSize := resultsInnerH - 1
	if pageSize < 1 {
		pageSize = 1
	}
	m.table = m.table.WithPageSize(pageSize).WithRows(m.dataRows).WithMaxTotalWidth(resultsInnerW)

	var inner string
	switch {
	case !m.hasQueried:
		wordmark := lipgloss.NewStyle().
			Foreground(p.Accent).
			Bold(true).
			Render(parseableAsciiArt)
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
	default:
		inner = m.table.View()
	}
	{
		lines := strings.Split(inner, "\n")
		if len(lines) > resultsInnerH {
			lines = lines[:resultsInnerH]
		}
		inner = strings.Join(lines, "\n")
	}
	resultsPane := renderResultsPane(inner, m.width, availH, len(m.dataRows), m.currentFocus() == "table")

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
		Height(innerH - 2).
		Padding(0, 1).
		Render(body)
	stack := lipgloss.JoinVertical(lipgloss.Left, titleRow, spacer, bodyPane)
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Render(stack)
}

// renderSQLDatasetBox draws the top SQL sidebar card: read-only
// DATASET label + value parsed from the SQL FROM clause. SQL doesn't
// have a separate dataset focus, so this card never lights.
func renderSQLDatasetBox(dataset string, width, height int) string {
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
	lines := []string{
		"  " + dim.Render("DATASET"),
		"  " + val.Render(dataset),
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
		Height(innerH - 1).
		Padding(0, 1).
		Render(body)
	stack := lipgloss.JoinVertical(lipgloss.Left, titleRow, bodyPane)
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Render(stack)
}

// renderTopSection (legacy: combined pane) — kept only to avoid
// breaking callers; not used by the main View() any more.
func renderTopSection(editorBody, start, end string, editorW, sidebarW, height int, focusEditor, focusTime bool) string {
	p := ui.Active
	borderColor := p.Border
	if focusEditor || focusTime {
		borderColor = p.BorderHi
	}

	innerH := height - 2 // outer border top+bottom
	if innerH < 4 {
		innerH = 4
	}

	// ── Left half: editor ────────────────────────────────────────────
	leftInner := editorW - 2
	if leftInner < 4 {
		leftInner = 4
	}
	editorTitleFg := p.Ghost
	if focusEditor {
		editorTitleFg = p.Accent
	}
	editorTitleText := lipgloss.NewStyle().
		Foreground(editorTitleFg).
		Bold(focusEditor).
		Render("EDITOR")
	editorRail := " "
	if focusEditor {
		editorRail = lipgloss.NewStyle().Background(p.Accent).Render(" ")
	}
	editorTitleRow := lipgloss.NewStyle().
		Width(leftInner).
		Padding(0, 1).
		Render(editorRail + " " + editorTitleText)
	editorRule := lipgloss.NewStyle().
		Foreground(p.BorderSoft).
		Render(strings.Repeat("─", leftInner))
	editorBodyPane := lipgloss.NewStyle().
		Width(leftInner).
		Height(innerH - 2).
		Padding(0, 1).
		Render(editorBody)
	leftStack := lipgloss.JoinVertical(lipgloss.Left, editorTitleRow, editorRule, editorBodyPane)

	// ── Vertical inner divider ───────────────────────────────────────
	vDivLine := strings.TrimRight(strings.Repeat("│\n", innerH), "\n")
	vDiv := lipgloss.NewStyle().
		Foreground(p.BorderSoft).
		Render(vDivLine)

	// ── Right half: time sidebar ─────────────────────────────────────
	rightInner := sidebarW - 3
	if rightInner < 4 {
		rightInner = 4
	}
	timeTitleFg := p.Ghost
	if focusTime {
		timeTitleFg = p.Accent
	}
	timeTitleText := lipgloss.NewStyle().
		Foreground(timeTitleFg).
		Bold(focusTime).
		Render("TIME")
	timeRail := " "
	if focusTime {
		timeRail = lipgloss.NewStyle().Background(p.Accent).Render(" ")
	}
	timeTitleRow := lipgloss.NewStyle().
		Width(rightInner).
		Padding(0, 1).
		Render(timeRail + " " + timeTitleText)
	timeRule := lipgloss.NewStyle().
		Foreground(p.BorderSoft).
		Render(strings.Repeat("─", rightInner))

	label := lipgloss.NewStyle().Foreground(p.Ghost).Bold(true)
	tsVal := lipgloss.NewStyle().Foreground(p.Accent)
	hintKey := lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	hintLabel := lipgloss.NewStyle().Foreground(p.Ghost)
	hint := hintKey.Render("enter") + hintLabel.Render("  edit range")
	timeContent := lipgloss.JoinVertical(lipgloss.Left,
		label.Render("START"),
		tsVal.Render(start),
		"",
		label.Render("END"),
		tsVal.Render(end),
	)
	padRows := (innerH - 2) - lipgloss.Height(timeContent) - lipgloss.Height(hint)
	if padRows < 0 {
		padRows = 0
	}
	timeStack := timeContent + strings.Repeat("\n", padRows+1) + hint
	timeBodyPane := lipgloss.NewStyle().
		Width(rightInner).
		Height(innerH - 2).
		Padding(0, 1).
		Render(timeStack)
	rightStack := lipgloss.JoinVertical(lipgloss.Left, timeTitleRow, timeRule, timeBodyPane)

	row := lipgloss.JoinHorizontal(lipgloss.Top, leftStack, vDiv, rightStack)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Render(row)
}

// buildBreadcrumbs renders the top tab row with FILLED active tab —
// active = Accent bg + InvertText fg + bold; idle = Mute fg on
// PanelAlt bg (raised vs canvas so the row reads as distinct chrome).
// Bottom hairline in Border separates from body.
func buildBreadcrumbs(active string, rowCount, savedCount, width int) string {
	p := ui.Active
	items := []struct {
		id, label string
		count     int
	}{
		{"query", "query", 0},
		{"time", "time", 0},
		{"results", "results", rowCount},
		{"metrics", "metrics", 0},
		{"saved", "saved", savedCount},
		{"help", "help", 0},
	}

	var tabs []string
	for _, it := range items {
		isActive := it.id == active
		st := lipgloss.NewStyle().Padding(0, 2)
		if isActive {
			st = st.Background(p.Accent).Foreground(p.InvertText).Bold(true)
		} else {
			st = st.Background(p.PanelAlt).Foreground(p.Mute)
		}
		text := it.label
		if it.count > 0 {
			text = fmt.Sprintf("%s %d", it.label, it.count)
		}
		tabs = append(tabs, st.Render(text))
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	used := lipgloss.Width(row)
	if used < width {
		row = row + lipgloss.NewStyle().Width(width-used).Background(p.PanelAlt).Render("")
	}
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(p.Border).
		Render(row)
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

	// ── Left: key hints ────────────────────────────────────────────
	hints := queryKeysForFocus(m)
	var keyParts []string
	for _, h := range hints {
		k := strings.TrimSuffix(strings.TrimPrefix(h.Key, "<"), ">")
		keyParts = append(keyParts,
			keyStyle.Render("<"+k+">")+labelStyle.Render(" "+strings.ToLower(h.Label)),
		)
	}
	left := strings.Join(keyParts, "    ")

	// ── Right: info · MODE · LIVE ──────────────────────────────────
	sep := sepStyle.Render(" │ ")
	var rightParts []string
	if m.status.Error != "" {
		rightParts = append(rightParts,
			labelStyle.Render("ERR"),
			" ",
			lipgloss.NewStyle().Foreground(p.Err).Bold(true).Render(m.status.Error),
			sep,
		)
	} else if m.status.Info != "" {
		rightParts = append(rightParts,
			lipgloss.NewStyle().Foreground(p.Body).Render(m.status.Info),
			sep,
		)
	}
	rightParts = append(rightParts,
		labelStyle.Render("MODE"),
		" ",
		lipgloss.NewStyle().Foreground(p.Accent).Bold(true).Render(strings.ToUpper(m.status.title)),
		sep,
		labelStyle.Render("LIVE"),
		" ",
		lipgloss.NewStyle().Foreground(p.Ok).Bold(true).Render("●"),
	)
	right := lipgloss.JoinHorizontal(lipgloss.Bottom, rightParts...)

	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}
	contentW := innerW - 2
	if contentW < 1 {
		contentW = 1
	}
	gap := contentW - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	row := left + strings.Repeat(" ", gap) + right
	inner := lipgloss.NewStyle().Width(innerW).Padding(0, 1).Render(row)
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(p.Border).
		Render(inner)
}


// buildQueryBreadcrumbs surfaces the current mode/overlay as a tab row.
// Active crumb fills with accent; idle crumbs read on bg.
func buildQueryBreadcrumbs(m QueryModel) string {
	active := "query"
	switch m.overlay {
	case overlayInputs:
		active = "time"
	default:
		switch m.currentFocus() {
		case "table":
			active = "results"
		case "time":
			active = "time"
		default:
			active = "query"
		}
	}
	items := []ui.Breadcrumb{
		{ID: "query", Label: "query", Active: active == "query"},
		{ID: "time", Label: "time", Active: active == "time"},
		{ID: "results", Label: "results", Active: active == "results"},
		{ID: "saved", Label: "saved"},
		{ID: "help", Label: "help", Active: active == "help"},
	}
	return ui.Breadcrumbs(m.width, items)
}

// buildQueryHeaderStrip renders the top chrome bar for the SQL view. KV
// block left, keybind grid middle, PB logo right (logo only at >=92 cols).
func buildQueryHeaderStrip(m QueryModel) string {
	dataset := ""
	q := m.query.Value()
	// best-effort: pull "FROM <dataset>" from the SQL — purely cosmetic.
	if i := strings.Index(strings.ToLower(q), " from "); i >= 0 {
		rest := strings.TrimSpace(q[i+6:])
		if sp := strings.IndexAny(rest, " ,;\n\t"); sp > 0 {
			dataset = rest[:sp]
		} else {
			dataset = rest
		}
	}
	if dataset == "" {
		dataset = "—"
	}

	rowsVal := "—"
	if len(m.dataRows) > 0 {
		rowsVal = fmt.Sprintf("%d", len(m.dataRows))
	}
	latencyVal := "—"
	if m.loading {
		latencyVal = "…"
	}

	user := m.profile.Username
	if user == "" {
		user = "—"
	}

	ctx := []ui.KV{
		{Key: "Cluster", Value: m.profile.URL, Variant: ui.KVMute},
		{Key: "User", Value: user},
		{Key: "Dataset", Value: dataset, Variant: ui.KVAccent},
		{Key: "Rows", Value: rowsVal, Variant: ui.KVMute},
		{Key: "Latency", Value: latencyVal, Variant: ui.KVMute},
	}

	keys := queryKeysForFocus(m)
	return ui.HeaderStrip(m.width, ctx, keys)
}

// queryKeysForFocus returns the keybind hints shown in the HeaderStrip
// based on which pane is focused. Mirrors what bubbles help did before
// the chrome refactor — context-aware help is back.
func queryKeysForFocus(m QueryModel) []ui.KeyHint {
	common := []ui.KeyHint{
		{Key: "<ctrl-r>", Label: "Run"},
		{Key: "<tab>", Label: "Next pane"},
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
	case "query":
		return append([]ui.KeyHint{
			{Key: "<ctrl-/>", Label: "Comment"},
			{Key: "<ctrl-d>", Label: "Dup line"},
			{Key: "<home/end>", Label: "Line"},
		}, common...)
	case "time":
		return append([]ui.KeyHint{
			{Key: "<enter>", Label: "Open picker"},
		}, common...)
	case "table":
		return append([]ui.KeyHint{
			{Key: "<↑/↓>", Label: "Row"},
			{Key: "</>", Label: "Filter"},
			{Key: "<g/G>", Label: "Top/End"},
			{Key: "<ctrl-b>", Label: "Prev page"},
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

// parseableAsciiArt is the block-letter wordmark shown in the empty
// state. Five rows tall, ~58 cells wide. Rendered in Accent.
const parseableAsciiArt = ` ____   _    ____  ____  _____    _    ____  _     _____
|  _ \ / \  |  _ \/ ___|| ____|  / \  | __ )| |   | ____|
| |_) / _ \ | |_) \___ \|  _|   / _ \ |  _ \| |   |  _|
|  __/ ___ \|  _ < ___) | |___ / ___ \| |_) | |___| |___
|_| /_/   \_\_| \_\____/|_____/_/   \_\____/|_____|_____|`

// applyEditorStyles maps bubbles/textarea styling slots onto the ui
// palette. No background colors — the editor inherits the terminal's
// own background so it blends with whatever theme the user has set.
// Without this, dark-palette EditorBg paints a near-black box on
// light terminals (and vice versa).
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

// buildPaneTitle renders the small uppercase title row used above the
// editor / results panes. Focused pane gets accent fg + accent rail.
func buildPaneTitle(label string, focused bool, width int) string {
	p := ui.Active
	titleFg := p.Dim
	if focused {
		titleFg = p.Accent
	}
	rail := lipgloss.NewStyle().
		Background(p.Border).
		Render(" ")
	if focused {
		rail = lipgloss.NewStyle().Background(p.Accent).Render(" ")
	}
	title := lipgloss.NewStyle().
		Foreground(titleFg).
		Bold(focused).
		Padding(0, 2).
		Background(p.Panel).
		Render(label)
	pad := width - lipgloss.Width(rail) - lipgloss.Width(title)
	if pad < 0 {
		pad = 0
	}
	fill := lipgloss.NewStyle().
		Width(pad).
		Background(p.Panel).
		Render("")
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(p.Border).
		Render(lipgloss.JoinHorizontal(lipgloss.Top, rail, title, fill))
}

// extractDataset best-effort parses the dataset name out of a SQL
// string by looking for `FROM <name>`. Used only for the editor card's
// meta subtitle — falls back to "—" when nothing matches.
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

// buildTimeBody is the inner content of the TIME RANGE card: START
// label + value, blank row, END label + value, hint. Caller wraps in
// ui.Card so the border/title comes from one source.
func buildTimeBody(start, end string, width int) string {
	label := ui.Type().Label.Bold(true)
	val := ui.Type().Body
	hint := ui.Type().Dim.Render("<enter> edit")
	return lipgloss.NewStyle().Width(width).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			label.Render("START"),
			val.Render(start),
			"",
			label.Render("END"),
			val.Render(end),
			"",
			hint,
		),
	)
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
