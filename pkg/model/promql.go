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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"pb/pkg/config"
	"sort"
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
	"golang.org/x/term"
)

// ─── constants ───────────────────────────────────────────────────────────────

const (
	promqlTimestampKey   = "timestamp"
	promqlMetricKey      = "metric"
	promqlValueKey       = "value"
	promqlTimestampWidth = 20

	// header panel outer widths (inner = outer - 2 for borders)
	datasetPanelOuter  = 30
	timePanelOuter     = 38
	stepModePanelOuter = 14

	// spotlight modal width
	spotlightWidth    = 58
	spotlightMaxItems = 12
)

// overlay states (overlayNone and overlayInputs are defined in query.go)
const overlayDataset uint = 2

var PromqlNavigationMap = []string{"dataset", "query", "time", "step", "table"}

var promqlAdditionalKeyBinds = []key.Binding{runQueryKey}

// ─── response types ──────────────────────────────────────────────────────────

type promqlRespModel struct {
	Status    string          `json:"status"`
	Data      promqlDataModel `json:"data"`
	Error     string          `json:"error,omitempty"`
	ErrorType string          `json:"errorType,omitempty"`
}

type promqlDataModel struct {
	ResultType string              `json:"resultType"`
	Result     []promqlSeriesModel `json:"result"`
}

type promqlSeriesModel struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value,omitempty"`
	Values [][]any           `json:"values,omitempty"`
}

// ─── message types ───────────────────────────────────────────────────────────

// PromqlFetchData is the message returned by NewPromqlFetchTask.
type PromqlFetchData struct {
	status      FetchResult
	resultType  string
	rows        []table.Row
	seriesCount int
	metricWidth int
	valueWidth  int
	errMsg      string
}

// datasetListMsg carries the list of streams fetched from the server.
type datasetListMsg struct {
	datasets []string
	errMsg   string
}

// ─── model ───────────────────────────────────────────────────────────────────

// PromqlModel is the Bubble Tea model for interactive PromQL queries.
type PromqlModel struct {
	width, height int
	table         table.Model
	query         textarea.Model
	timeRange     TimeInputModel
	profile       config.Profile
	help          help.Model
	status        StatusBar
	spinner       spinner.Model

	loading        bool
	hasQueried     bool
	overlay        uint
	focused        int
	dataRows       []table.Row
	fetchErrMsg    string
	lastResultType string
	seriesCount    int

	// query parameters
	dataset string
	step    string
	instant bool

	// step panel state
	stepInput textinput.Model

	// dataset spotlight state
	spotlightFilter    textinput.Model
	allDatasets        []string
	filteredDatasets   []string
	datasetSelectedIdx int
	datasetsLoading    bool
}

func (m *PromqlModel) focusSelected() {
	m.query.Blur()
	m.table = m.table.Focused(false)
	m.spotlightFilter.Blur()
	m.stepInput.Blur()
	switch m.currentFocus() {
	case "query":
		m.query.Focus()
	case "step":
		m.stepInput.Focus()
	case "table":
		m.table = m.table.Focused(true)
	}
}

func (m *PromqlModel) currentFocus() string {
	return PromqlNavigationMap[m.focused]
}

func (m *PromqlModel) queryWidth() int {
	w := m.width - datasetPanelOuter - timePanelOuter - stepModePanelOuter - 2
	if w < 30 {
		w = 30
	}
	return w
}

// ─── constructor ─────────────────────────────────────────────────────────────

func NewPromqlModel(profile config.Profile, expr string, startTime, endTime time.Time, step, dataset string, instant bool) PromqlModel {
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))

	inputs := NewTimeInputModel(startTime, endTime)
	inputs.SetInstant(instant)

	columns := []table.Column{
		table.NewColumn(promqlTimestampKey, "timestamp", promqlTimestampWidth),
		table.NewFlexColumn(promqlMetricKey, "metric", 1),
		table.NewColumn(promqlValueKey, "value", 10),
	}

	pageSize := h - 14
	if pageSize < 5 {
		pageSize = 5
	}

	tbl := table.New(columns).
		WithRows([]table.Row{}).
		Filtered(true).
		HeaderStyle(headerStyle).
		SelectableRows(false).
		Border(customBorder).
		Focused(false).
		WithKeyMap(tableKeyBinds).
		WithPageSize(pageSize).
		WithBaseStyle(tableStyle).
		WithMissingDataIndicatorStyled(table.StyledCell{
			Style: lipgloss.NewStyle().Foreground(StandardSecondary),
			Data:  "╌",
		}).WithTargetWidth(w)

	qw := w - datasetPanelOuter - timePanelOuter - stepModePanelOuter - 2
	if qw < 30 {
		qw = 30
	}
	q := textarea.New()
	q.MaxHeight = 0
	q.MaxWidth = 0
	q.SetHeight(2)
	q.SetWidth(qw)
	q.ShowLineNumbers = true
	q.SetValue(expr)
	q.Placeholder = "write your PromQL expression here..."
	q.KeyMap = textAreaKeyMap
	q.Focus()

	si := textinput.New()
	si.SetValue(step)
	si.Width = stepModePanelOuter - 10
	si.Blur()

	sf := textinput.New()
	sf.Placeholder = "search datasets..."
	sf.Width = spotlightWidth - 6
	sf.Blur()

	hlp := help.New()
	hlp.Styles.FullDesc = lipgloss.NewStyle().Foreground(FocusSecondary)

	stat := NewStatusBar(profile.URL, w)

	sp := spinner.New()
	sp.Spinner = spinner.Line
	sp.Style = lipgloss.NewStyle().Foreground(FocusPrimary)

	hasQuery := strings.TrimSpace(expr) != ""
	return PromqlModel{
		width:      w,
		height:     h,
		table:      tbl,
		query:      q,
		timeRange:  inputs,
		overlay:    overlayNone,
		profile:    profile,
		help:       hlp,
		spinner:    sp,
		loading:    hasQuery,
		hasQueried: hasQuery,
		status:     stat,
		dataset:    dataset,
		step:       step,
		instant:    instant,

		stepInput:       si,
		spotlightFilter: sf,
	}
}

// ─── bubbletea lifecycle ─────────────────────────────────────────────────────

func (m PromqlModel) Init() tea.Cmd {
	if strings.TrimSpace(m.query.Value()) == "" {
		return m.spinner.Tick
	}
	return tea.Batch(
		m.spinner.Tick,
		NewPromqlFetchTask(m.profile, m.query.Value(), m.dataset, m.step,
			m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc(), m.instant),
	)
}

func (m PromqlModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		m.query.SetWidth(m.queryWidth())
		m.stepInput.Width = stepModePanelOuter - 10
		m.spotlightFilter.Width = spotlightWidth - 6
		m.updateTableColumns(0, 0) // reflow columns to new terminal width
		return m, nil

	case datasetListMsg:
		m.datasetsLoading = false
		if msg.errMsg != "" {
			m.status.Error = "could not load datasets: " + msg.errMsg
		} else {
			m.allDatasets = msg.datasets
			m.filteredDatasets = msg.datasets
			m.datasetSelectedIdx = 0
			// pre-select current dataset
			for i, ds := range m.filteredDatasets {
				if ds == m.dataset {
					m.datasetSelectedIdx = i
					break
				}
			}
		}
		return m, nil

	case PromqlFetchData:
		m.loading = false
		m.status.Info = ""
		if msg.status == fetchOk {
			m.fetchErrMsg = ""
			m.status.Error = ""
			m.dataRows = msg.rows
			m.lastResultType = msg.resultType
			m.seriesCount = msg.seriesCount
			mode := "range"
			if m.instant {
				mode = "instant"
			}
			m.status.Info = fmt.Sprintf("%d rows  %d series  %s  step=%s  ds=%s",
				len(m.dataRows), m.seriesCount, mode, m.step, m.dataset)
			m.updateTableColumns(msg.metricWidth, msg.valueWidth)
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

	case tea.KeyMsg:
		// ── dataset spotlight overlay ────────────────────────────────────────
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
					m.dataset = m.filteredDatasets[m.datasetSelectedIdx]
				}
				m.overlay = overlayNone
				m.spotlightFilter.SetValue("")
				m.spotlightFilter.Blur()
				m.focusSelected()
				return m, nil

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

		// ── time overlay ─────────────────────────────────────────────────────
		if m.overlay == overlayInputs {
			if msg.Type == tea.KeyEnter {
				m.overlay = overlayNone
				m.focusSelected()
				m.status.Error = ""
				m.status.Info = ""
				m.loading = true
				m.hasQueried = true
				return m, tea.Batch(m.spinner.Tick,
					NewPromqlFetchTask(m.profile, m.query.Value(), m.dataset, m.step,
						m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc(), m.instant))
			}
			m.timeRange, cmd = m.timeRange.Update(msg)
			return m, cmd
		}

		// ── main navigation ──────────────────────────────────────────────────
		if msg.Type == tea.KeyTab {
			m.focused++
			if m.focused > len(PromqlNavigationMap)-1 {
				m.focused = 0
			}
			m.focusSelected()
			return m, nil
		}
		if msg.Type == tea.KeyShiftTab {
			m.focused--
			if m.focused < 0 {
				m.focused = len(PromqlNavigationMap) - 1
			}
			m.focusSelected()
			return m, nil
		}

		// Enter on dataset → open spotlight
		if msg.Type == tea.KeyEnter && m.currentFocus() == "dataset" {
			m.overlay = overlayDataset
			m.spotlightFilter.Focus()
			m.datasetsLoading = true
			return m, tea.Batch(fetchDatasetList(m.profile))
		}

		// Enter on time → open time overlay
		if msg.Type == tea.KeyEnter && m.currentFocus() == "time" {
			m.overlay = overlayInputs
			return m, nil
		}

		// Ctrl+R → run query
		if msg.Type == tea.KeyCtrlR {
			m.overlay = overlayNone
			m.status.Error = ""
			m.status.Info = ""
			m.loading = true
			m.hasQueried = true
			return m, tea.Batch(m.spinner.Tick,
				NewPromqlFetchTask(m.profile, m.query.Value(), m.dataset, m.step,
					m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc(), m.instant))
		}

		// Space on step panel toggles instant/range mode
		if msg.Type == tea.KeySpace && m.currentFocus() == "step" {
			m.instant = !m.instant
			m.timeRange.SetInstant(m.instant)
			if m.instant {
				// default end to now-1h so instant query lands within data range
				m.timeRange.SetEnd(time.Now().Add(-1 * time.Hour))
			} else {
				// switching back to range: reset end to now so presets work correctly
				m.timeRange.SetEnd(time.Now())
			}
			return m, nil
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		default:
			switch m.currentFocus() {
			case "query":
				m.query, cmd = m.query.Update(msg)
				cmds = append(cmds, cmd)
			case "step":
				m.stepInput, cmd = m.stepInput.Update(msg)
				m.step = m.stepInput.Value()
				cmds = append(cmds, cmd)
			case "table":
				m.table, cmd = m.table.Update(msg)
				cmds = append(cmds, cmd)
			}
		}
	}

	return m, tea.Batch(cmds...)
}

// ─── view ────────────────────────────────────────────────────────────────────

func (m PromqlModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	// ── header panels ────────────────────────────────────────────────────────
	dsName := m.dataset
	if len(dsName) > datasetPanelOuter-4 {
		dsName = dsName[:datasetPanelOuter-7] + "..."
	}
	datasetPane := lipgloss.JoinVertical(lipgloss.Left,
		baseBoldUnderlinedStyle.Render(" dataset "),
		dsName,
	)

	mode := "range"
	modeColor := lipgloss.AdaptiveColor{Light: "28", Dark: "82"} // green = range
	if m.instant {
		mode = "instant"
		modeColor = lipgloss.AdaptiveColor{Light: "208", Dark: "214"} // orange = instant
	}
	modeLabel := lipgloss.NewStyle().Foreground(modeColor).Bold(true).Render(mode)

	var stepRow string
	if m.instant {
		dimmed := lipgloss.NewStyle().Foreground(StandardSecondary).Render("--")
		stepRow = fmt.Sprintf("%s %s", baseBoldUnderlinedStyle.Render(" step "), dimmed)
	} else if m.currentFocus() == "step" {
		stepRow = fmt.Sprintf("%s %s", baseBoldUnderlinedStyle.Render(" step "), m.stepInput.View())
	} else {
		stepRow = fmt.Sprintf("%s %s", baseBoldUnderlinedStyle.Render(" step "), m.step)
	}
	stepModePane := lipgloss.JoinVertical(lipgloss.Left,
		stepRow,
		fmt.Sprintf("%s %s", baseBoldUnderlinedStyle.Render(" mode "), modeLabel),
	)

	timePane := lipgloss.JoinVertical(lipgloss.Left,
		fmt.Sprintf("%s %s ", baseBoldUnderlinedStyle.Render(" start "), m.timeRange.start.Value()),
		fmt.Sprintf("%s %s ", baseBoldUnderlinedStyle.Render("   end "), m.timeRange.end.Value()),
	)

	// pick border styles based on focused panel
	dsOuter, queryOuter, timeOuter, stepOuter := &borderedStyle, &borderedStyle, &borderedStyle, &borderedStyle
	tableOuter := lipgloss.NewStyle()
	switch m.currentFocus() {
	case "dataset":
		dsOuter = &borderedFocusStyle
	case "query":
		queryOuter = &borderedFocusStyle
	case "time":
		timeOuter = &borderedFocusStyle
	case "step":
		stepOuter = &borderedFocusStyle
	case "table":
		tableOuter = tableOuter.Border(lipgloss.DoubleBorder(), false, false, false, true).
			BorderForeground(FocusPrimary)
	}

	// render fixed panels first so we can measure their real widths
	dsRendered := dsOuter.Render(datasetPane)
	timeRendered := timeOuter.Render(timePane)
	stepRendered := stepOuter.Render(stepModePane)
	fixedW := lipgloss.Width(dsRendered) + lipgloss.Width(timeRendered) + lipgloss.Width(stepRendered)
	queryW := m.width - fixedW
	if queryW < 30 {
		queryW = 30
	}
	m.query.SetWidth(queryW - 2) // -2 for query panel border
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		dsRendered,
		queryOuter.Render(m.query.View()),
		timeRendered,
		stepRendered,
	)
	headerHeight := lipgloss.Height(header)

	if m.loading {
		m.status.Info = ""
		m.status.Error = ""
	}
	statusView := m.status.View()
	statusHeight := lipgloss.Height(statusView)

	// ── help ─────────────────────────────────────────────────────────────────
	var helpKeys [][]key.Binding
	switch m.overlay {
	case overlayNone:
		switch m.currentFocus() {
		case "dataset":
			helpKeys = [][]key.Binding{
				{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "pick dataset"))},
				{promqlAdditionalKeyBinds[0]},
			}
		case "query":
			helpKeys = TextAreaHelpKeys{}.FullHelp()
		case "time":
			timeHint := "edit time range"
			if m.instant {
				timeHint = "set evaluation time (instant)"
			}
			helpKeys = [][]key.Binding{
				{key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", timeHint))},
				{promqlAdditionalKeyBinds[0]},
			}
		case "step":
			helpKeys = [][]key.Binding{
				{
					key.NewBinding(key.WithKeys("type"), key.WithHelp("type", "edit step (e.g. 15s, 5m, 1h)")),
					key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "toggle range/instant")),
				},
				{
					promqlAdditionalKeyBinds[0],
				},
			}
		case "table":
			helpKeys = tableHelpBinds.FullHelp()
			helpKeys = append(helpKeys, promqlAdditionalKeyBinds)
		}
	case overlayInputs:
		helpKeys = m.timeRange.FullHelp()
		helpKeys = append(helpKeys, promqlAdditionalKeyBinds)
	case overlayDataset:
		helpKeys = [][]key.Binding{{
			key.NewBinding(key.WithKeys("↑/↓"), key.WithHelp("↑/↓", "navigate")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}}
	}
	helpView := m.help.FullHelpView(helpKeys)
	helpHeight := lipgloss.Height(helpView)

	// ── result area ──────────────────────────────────────────────────────────
	tableAvail := m.height - headerHeight - helpHeight - statusHeight
	pageSize := tableAvail - 6
	if pageSize < 1 {
		pageSize = 1
	}

	displayRows := make([]table.Row, pageSize)
	copy(displayRows, m.dataRows)
	m.table = m.table.WithPageSize(pageSize).WithRows(displayRows).WithTargetWidth(m.width)

	availW := m.width
	if availW < 0 {
		availW = 0
	}
	availH := tableAvail - 2
	if availH < 0 {
		availH = 0
	}
	tableOuter = tableOuter.Width(m.width)

	var resultPane string
	if !m.hasQueried {
		logoStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(FocusPrimary).
			Border(lipgloss.DoubleBorder()).
			BorderForeground(FocusSecondary).
			Padding(0, 2)
		hintStyle := lipgloss.NewStyle().
			Foreground(StandardSecondary).
			MarginTop(1)
		keyStyle := lipgloss.NewStyle().Foreground(FocusPrimary).Bold(true)
		logo := logoStyle.Render("P A R S E A B L E")
		hint := hintStyle.Render("write a PromQL expression above and press " + keyStyle.Render("ctrl+r") + " to run")
		content := lipgloss.JoinVertical(lipgloss.Center, logo, hint)
		placed := lipgloss.Place(availW, availH, lipgloss.Center, lipgloss.Center, content)
		resultPane = tableOuter.Render(placed)
	} else if m.loading {
		spinStyle := lipgloss.NewStyle().Foreground(FocusPrimary)
		content := spinStyle.Render(m.spinner.View() + " fetching...")
		placed := lipgloss.Place(availW, availH, lipgloss.Center, lipgloss.Center, content)
		resultPane = tableOuter.Render(placed)
	} else if m.fetchErrMsg != "" {
		errStyle := lipgloss.NewStyle().
			Padding(1, 2).
			Foreground(lipgloss.AdaptiveColor{Light: "#9B2226", Dark: "#FF6B6B"}).
			Width(m.width)
		rendered := errStyle.Render(m.fetchErrMsg)
		lines := strings.Split(rendered, "\n")
		maxLines := tableAvail - 2
		if maxLines < 1 {
			maxLines = 1
		}
		if len(lines) > maxLines {
			lines = lines[:maxLines]
		}
		resultPane = tableOuter.Render(strings.Join(lines, "\n"))
	} else {
		resultPane = tableOuter.Render(m.table.View())
	}

	// ── compose main or overlay view ─────────────────────────────────────────
	var mainView string
	switch m.overlay {
	case overlayNone:
		mainView = lipgloss.JoinVertical(lipgloss.Left, header, resultPane)
	case overlayInputs:
		timeView := m.timeRange.View()
		mainView = lipgloss.Place(m.width, m.height-helpHeight-statusHeight,
			lipgloss.Center, lipgloss.Center, timeView,
			lipgloss.WithWhitespaceChars(" "),
			lipgloss.WithWhitespaceForeground(StandardSecondary),
		)
	case overlayDataset:
		// render the normal content behind, then paint spotlight on top
		behind := lipgloss.JoinVertical(lipgloss.Left, header, resultPane)
		spotlight := m.renderSpotlight()
		mainView = lipgloss.Place(m.width, m.height-helpHeight-statusHeight,
			lipgloss.Center, lipgloss.Center, spotlight,
			lipgloss.WithWhitespaceChars(" "),
			lipgloss.WithWhitespaceForeground(StandardSecondary),
		)
		_ = behind
	}

	mainHeight := lipgloss.Height(mainView)
	bottomHeight := helpHeight + statusHeight
	padLines := m.height - mainHeight - bottomHeight
	if padLines > 0 {
		mainView = mainView + strings.Repeat("\n", padLines)
	}

	render := lipgloss.JoinVertical(lipgloss.Left, mainView, helpView, statusView)
	return lipgloss.NewStyle().Width(m.width).Render(render)
}

// renderSpotlight builds the dataset picker modal.
func (m PromqlModel) renderSpotlight() string {
	innerW := spotlightWidth - 2

	titleStyle := lipgloss.NewStyle().
		Foreground(FocusPrimary).
		Bold(true).
		Width(innerW).
		Align(lipgloss.Center)
	title := titleStyle.Render("Select Dataset")

	searchStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(FocusSecondary).
		Width(innerW-2).
		Padding(0, 1)
	searchBar := searchStyle.Render(m.spotlightFilter.View())

	var listLines []string
	if m.datasetsLoading {
		loadStyle := lipgloss.NewStyle().
			Foreground(StandardSecondary).
			Width(innerW).
			Align(lipgloss.Center).
			Padding(1, 0)
		listLines = append(listLines, loadStyle.Render(m.spinner.View()+" loading…"))
	} else if len(m.filteredDatasets) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(StandardSecondary).
			Width(innerW).
			Align(lipgloss.Center).
			Padding(1, 0)
		listLines = append(listLines, emptyStyle.Render("no datasets found"))
	} else {
		limit := len(m.filteredDatasets)
		if limit > spotlightMaxItems {
			limit = spotlightMaxItems
		}
		// scroll window around selected index
		start := 0
		if m.datasetSelectedIdx >= spotlightMaxItems {
			start = m.datasetSelectedIdx - spotlightMaxItems + 1
		}
		for i := start; i < start+limit && i < len(m.filteredDatasets); i++ {
			ds := m.filteredDatasets[i]
			if i == m.datasetSelectedIdx {
				row := lipgloss.NewStyle().
					Background(FocusPrimary).
					Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#000000"}).
					Width(innerW).
					Padding(0, 1).
					Bold(true).
					Render("▸ " + ds)
				listLines = append(listLines, row)
			} else {
				row := lipgloss.NewStyle().
					Width(innerW).
					Padding(0, 1).
					Render("  " + ds)
				listLines = append(listLines, row)
			}
		}
		if len(m.filteredDatasets) > spotlightMaxItems {
			more := lipgloss.NewStyle().
				Foreground(StandardSecondary).
				Width(innerW).
				Align(lipgloss.Right).
				Render(fmt.Sprintf("  +%d more", len(m.filteredDatasets)-spotlightMaxItems))
			listLines = append(listLines, more)
		}
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		title,
		searchBar,
		strings.Join(listLines, "\n"),
	)

	modal := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(FocusPrimary).
		Padding(0, 1).
		Width(spotlightWidth).
		Render(body)

	return modal
}

// updateTableColumns rebuilds table columns. valueWidth is inferred from data;
// the metric column is a flex column that fills all remaining width automatically.
func (m *PromqlModel) updateTableColumns(_, valueWidth int) {
	if valueWidth < len(promqlValueKey) {
		valueWidth = len(promqlValueKey)
	}
	columns := []table.Column{
		table.NewColumn(promqlTimestampKey, "timestamp", promqlTimestampWidth),
		table.NewFlexColumn(promqlMetricKey, "metric", 1).WithFiltered(true),
		table.NewColumn(promqlValueKey, "value", valueWidth).WithFiltered(true),
	}
	m.table = m.table.WithColumns(columns).WithTargetWidth(m.width).WithRows(m.dataRows)
}

// ─── async commands ───────────────────────────────────────────────────────────

// fetchDatasetList loads all streams from the server for the spotlight picker.
func fetchDatasetList(profile config.Profile) tea.Cmd {
	return func() tea.Msg {
		reqURL, err := url.JoinPath(profile.URL, "api/v1/logstream")
		if err != nil {
			return datasetListMsg{errMsg: err.Error()}
		}
		client := &http.Client{Timeout: 10 * time.Second}
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
			Name string `json:"name"`
		}
		if err := json.Unmarshal(body, &items); err != nil {
			return datasetListMsg{errMsg: err.Error()}
		}
		datasets := make([]string, len(items))
		for i, item := range items {
			datasets[i] = item.Name
		}
		sort.Strings(datasets)
		return datasetListMsg{datasets: datasets}
	}
}

// NewPromqlFetchTask returns a Bubble Tea command that fetches PromQL data asynchronously.
func NewPromqlFetchTask(profile config.Profile, expr, dataset, step, startTime, endTime string, instant bool) tea.Cmd {
	return func() (msg tea.Msg) {
		res := PromqlFetchData{status: fetchErr}
		defer func() {
			if r := recover(); r != nil {
				res.errMsg = fmt.Sprintf("panic: %v", r)
				msg = res
			}
		}()

		params := url.Values{}
		params.Set("query", expr)
		params.Set("stream", dataset)

		var apiPath string
		if instant {
			apiPath = "prometheus/api/v1/query"
			params.Set("time", endTime)
		} else {
			apiPath = "prometheus/api/v1/query_range"
			params.Set("start", startTime)
			params.Set("end", endTime)
			params.Set("step", step)
		}

		body, err := promqlModelFetch(profile, apiPath, params)
		if err != nil {
			res.errMsg = err.Error()
			return res
		}

		var result promqlRespModel
		if err := json.Unmarshal(body, &result); err != nil {
			res.errMsg = fmt.Sprintf("failed to parse response: %s", err)
			return res
		}
		if result.Status == "error" {
			res.errMsg = fmt.Sprintf("%s: %s", result.ErrorType, result.Error)
			return res
		}

		rows, seriesCount, metricWidth, valueWidth := promqlResultToRows(result)
		res.status = fetchOk
		res.resultType = result.Data.ResultType
		res.rows = rows
		res.seriesCount = seriesCount
		res.metricWidth = metricWidth
		res.valueWidth = valueWidth
		return res
	}
}

func promqlModelFetch(profile config.Profile, path string, params url.Values) ([]byte, error) {
	reqURL, err := url.JoinPath(profile.URL, path)
	if err != nil {
		return nil, err
	}
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	client := &http.Client{
		Timeout: 120 * time.Second,
		Transport: &http.Transport{
			TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),
		},
	}

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	if profile.Token != "" {
		req.Header.Set("Authorization", "Bearer "+profile.Token)
	} else {
		req.SetBasicAuth(profile.Username, profile.Password)
	}

	resp, err := client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "connection reset") {
			return nil, fmt.Errorf("server reset the connection — query timed out")
		}
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		errMsg := strings.TrimSpace(string(body))
		if errMsg == "" {
			errMsg = resp.Status
		}
		return nil, fmt.Errorf("HTTP %s: %s", resp.Status, errMsg)
	}
	return body, nil
}

// ─── data conversion ──────────────────────────────────────────────────────────

func promqlResultToRows(result promqlRespModel) (rows []table.Row, seriesCount, metricWidth, valueWidth int) {
	metricWidth = len(promqlMetricKey)
	valueWidth = len(promqlValueKey)

	for _, series := range result.Data.Result {
		metricStr := promqlModelFormatLabels(series.Metric)
		if len(metricStr) > metricWidth {
			metricWidth = len(metricStr)
		}

		switch result.Data.ResultType {
		case "vector":
			if len(series.Value) == 2 {
				ts := promqlModelFormatTS(series.Value[0])
				val := fmt.Sprintf("%v", series.Value[1])
				if len(val) > valueWidth {
					valueWidth = len(val)
				}
				rows = append(rows, table.NewRow(table.RowData{
					promqlTimestampKey: ts,
					promqlMetricKey:    metricStr,
					promqlValueKey:     val,
				}))
			}
		case "matrix":
			for _, pt := range series.Values {
				if len(pt) == 2 {
					ts := promqlModelFormatTS(pt[0])
					val := fmt.Sprintf("%v", pt[1])
					if len(val) > valueWidth {
						valueWidth = len(val)
					}
					rows = append(rows, table.NewRow(table.RowData{
						promqlTimestampKey: ts,
						promqlMetricKey:    metricStr,
						promqlValueKey:     val,
					}))
				}
			}
		}
	}

	seriesCount = len(result.Data.Result)
	return
}

func promqlModelFormatLabels(m map[string]string) string {
	name := m["__name__"]
	var labels []string
	for k, v := range m {
		if k != "__name__" {
			labels = append(labels, k+`="`+v+`"`)
		}
	}
	sort.Strings(labels)
	if len(labels) == 0 {
		return name
	}
	if name == "" {
		return "{" + strings.Join(labels, ", ") + "}"
	}
	return fmt.Sprintf("%s{%s}", name, strings.Join(labels, ", "))
}

func promqlModelFormatTS(v any) string {
	if f, ok := v.(float64); ok {
		return time.Unix(int64(f), 0).UTC().Format("2006-01-02T15:04:05Z")
	}
	return fmt.Sprintf("%v", v)
}

// filterDatasets returns entries in all that contain query (case-insensitive).
func filterDatasets(all []string, query string) []string {
	if query == "" {
		return all
	}
	q := strings.ToLower(query)
	var out []string
	for _, ds := range all {
		if strings.Contains(strings.ToLower(ds), q) {
			out = append(out, ds)
		}
	}
	return out
}
