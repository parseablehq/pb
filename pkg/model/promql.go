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
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"pb/pkg/config"
	"pb/pkg/ui"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/canvas"
	"github.com/NimbleMarkets/ntcharts/canvas/runes"
	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/bubbles/help"
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
	promqlTimestampKey     = "timestamp"
	promqlTimestampFullKey = "_timestamp_full"
	promqlMetricKey        = "metric"
	promqlValueKey         = "value"
	promqlTimestampWidth   = 10 // matches SQL dateTimeWidth (HH:MM:SS + slack)

	// spotlight modal width
	spotlightWidth    = 58
	spotlightMaxItems = 12

	builderMaxItems = 10
)

// overlay states (overlayNone and overlayInputs are defined in query.go)
const overlayDataset uint = 2
const overlayBuilder uint = 3

var PromqlNavigationMap = []string{"query", "dataset", "step", "time", "table"}

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

// builder message types — one per column so Update() can route them unambiguously.
type builderMetricsMsg struct {
	items  []string
	errMsg string
}
type builderLabelsMsg struct {
	metric string // which metric these labels belong to (for cache keying)
	items  []string
	errMsg string
}
type builderValuesMsg struct {
	metric string // context for cache keying
	label  string // context for cache keying
	items  []string
	errMsg string
}

// cacheMetricsMsg is returned by the background metrics pre-fetch (not the builder fetch).
type cacheMetricsMsg struct {
	dataset string
	items   []string
	errMsg  string
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
	chartMode      bool // toggle between table and chart view
	tsChart        timeserieslinechart.Model
	tsChartReady   bool
	chartCursor    int
	chartHover     bool

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

	// pre-fetch cache: warmed in background after dataset selection
	cacheDataset string
	cacheMetrics []string
	cacheLabels  map[string][]string            // metric → label names
	cacheValues  map[string]map[string][]string // metric → label → values

	// query builder — 3-column panel (metrics | labels | values)
	builderCol             int
	builderMetric          string
	builderLabel           string
	builderValue           string
	builderMetrics         []string
	builderLabels          []string
	builderValues          []string
	builderMetricsFiltered []string
	builderLabelsFiltered  []string
	builderValuesFiltered  []string
	builderMetricsIdx      int
	builderLabelsIdx       int
	builderValuesIdx       int
	builderMetricsLoading  bool
	builderLabelsLoading   bool
	builderValuesLoading   bool
	builderFilter          textinput.Model
	cancelLabels           context.CancelFunc
	cancelValues           context.CancelFunc

	queryMode string
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
	lw, rw := promqlPanelWidths(m.width)
	editorW := m.width - lw - rw - 2
	if editorW < 30 {
		editorW = 30
	}
	w := editorW - 6
	if w < 24 {
		w = 24
	}
	return w
}

// promqlPageSize computes the table page size for the given terminal height and
// bottom-bar height. bottomH=3 is the normal value (1 content line + 2 border
// lines from NormalBorder). Both View() and the WindowSizeMsg handler use this
// so navigation and rendering always agree on the page size.
//
// The table's built-in footer is disabled; a custom footer line (pages left,
// rows right) is rendered as the last line inside the results pane body.
// Table overhead = header(3) + last-row-bottom(1) + inside-footer(1) = 5 lines.
func promqlPageSize(totalH, bottomH int) int {
	const topH = 13
	av := totalH - topH - bottomH
	if av < 6 {
		av = 6
	}
	rih := av - 3 // results pane outer border (2) + title row (1)
	if rih < 3 {
		rih = 3
	}
	ps := rih - 5 // table overhead: header(3) + last-row-bottom(1) + inside-footer(1)
	if ps < 1 {
		ps = 1
	}
	return ps
}

func promqlPanelWidths(totalW int) (leftW, rightW int) {
	leftW, rightW = 20, 28
	if totalW >= 140 {
		leftW, rightW = 22, 30
	}
	if totalW < 100 {
		leftW, rightW = 18, 26
	}
	return
}

// ─── constructor ─────────────────────────────────────────────────────────────

func NewPromqlModel(profile config.Profile, expr string, startTime, endTime time.Time, step, dataset string, instant bool) PromqlModel {
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))

	inputs := NewTimeInputModel(startTime, endTime)
	inputs.SetInstant(instant)

	columns := []table.Column{
		table.NewColumn(promqlTimestampKey, "TIMESTAMP", promqlTimestampWidth),
		table.NewFlexColumn(promqlMetricKey, "METRIC", 1),
		table.NewColumn(promqlValueKey, "VALUE", 14),
	}

	pageSize := promqlPageSize(h, 3) // 3 = bottom bar height (1 content + 2 border lines)
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
		HighlightStyle(highlightStyle).
		WithMissingDataIndicatorStyled(table.StyledCell{
			Style: ui.Type().Mute,
			Data:  "╌",
		}).WithTargetWidth(w).
		WithFooterVisibility(false) // custom page-info line is pinned to results pane bottom

	lw, rw := promqlPanelWidths(w)
	editorInitW := w - lw - rw - 2
	if editorInitW < 30 {
		editorInitW = 30
	}
	qw := editorInitW - 6
	if qw < 24 {
		qw = 24
	}
	q := textarea.New()
	q.MaxHeight = 0
	q.MaxWidth = 0
	q.SetHeight(1)
	q.ShowLineNumbers = false
	q.EndOfBufferCharacter = ' '
	q.SetValue(expr)
	q.Placeholder = "Write your queries here"
	q.KeyMap = textAreaKeyMap
	applyEditorStyles(&q)
	q.SetWidth(qw)
	q.SetValue(expr)
	q.Focus()

	si := textinput.New()
	si.Prompt = ""
	si.SetValue(step)
	si.Width = 4
	si.Blur()

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

	bf := textinput.New()
	bf.Placeholder = "filter"
	bf.Prompt = "> "
	bf.PromptStyle = lipgloss.NewStyle().
		Foreground(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Accent })).
		Bold(true)
	bf.PlaceholderStyle = lipgloss.NewStyle().
		Foreground(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Ghost })).
		Italic(true)
	bf.Width = 30
	bf.Blur()

	hlp := help.New()
	hlp.Styles.FullDesc = ui.Type().Dim

	stat := NewStatusBar(profile.URL, w)

	sp := spinner.New()
	sp.Spinner = spinner.Line
	sp.Style = ui.Type().Accent

	// Start with focus on the query editor so the cursor is ready to type.
	queryFocusIdx := 0
	for i, name := range PromqlNavigationMap {
		if name == "query" {
			queryFocusIdx = i
			break
		}
	}

	hasQuery := strings.TrimSpace(expr) != "" && dataset != ""
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
		focused:    queryFocusIdx,
		chartMode:  true,

		stepInput:       si,
		spotlightFilter: sf,
		builderFilter:   bf,
		queryMode:       "code",
	}
}

// ─── bubbletea lifecycle ─────────────────────────────────────────────────────

func (m PromqlModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick}
	if strings.TrimSpace(m.query.Value()) != "" && m.dataset != "" {
		cmds = append(cmds, NewPromqlFetchTask(m.profile, m.query.Value(), m.dataset, m.step,
			m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc(), m.instant))
	}
	if m.dataset != "" {
		cmds = append(cmds, fetchCacheMetrics(m.profile, m.dataset))
	}
	// Fetch dataset list on init so the first dataset is auto-selected if none was provided.
	cmds = append(cmds, fetchMetricDatasets(m.profile))
	return tea.Batch(cmds...)
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
		m.query.SetHeight(13 - 4) // topH(13) - border(2)+title(1)+spacer(1) = 9 editable lines
		m.stepInput.Width = 4
		m.spotlightFilter.Width = spotlightWidth - 6
		colW := builderColWidth(m.width)
		m.builderFilter.Width = colW*3 + 8
		m.updateTableColumns(0, 0)
		bh := lipgloss.Height(buildPromqlBottomBar(m, m.width))
		m.table = m.table.WithPageSize(promqlPageSize(m.height, bh))
		m.rebuildChart()
		return m, nil

	case datasetListMsg:
		m.datasetsLoading = false
		if msg.errMsg != "" {
			m.status.Error = "could not load datasets: " + msg.errMsg
		} else {
			m.allDatasets = msg.datasets
			m.filteredDatasets = msg.datasets
			m.datasetSelectedIdx = 0
			// No dataset chosen yet: pick the first available so the
			// sidebar shows a real value out of the box instead of
			// the "select-dataset" placeholder. Kick off the metrics
			// cache fetch the same way an explicit selection does.
			if (m.dataset == "" || m.dataset == "select-dataset") && len(msg.datasets) > 0 {
				m.dataset = msg.datasets[0]
				m.cacheDataset = ""
				m.cacheMetrics = nil
				if strings.TrimSpace(m.query.Value()) != "" {
					m.loading = true
					m.hasQueried = true
					return m, tea.Batch(
						m.spinner.Tick,
						fetchCacheMetrics(m.profile, m.dataset),
						NewPromqlFetchTask(m.profile, m.query.Value(), m.dataset, m.step,
							m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc(), m.instant),
					)
				}
				return m, fetchCacheMetrics(m.profile, m.dataset)
			}
			for i, ds := range m.filteredDatasets {
				if ds == m.dataset {
					m.datasetSelectedIdx = i
					break
				}
			}
		}
		return m, nil

	case cacheMetricsMsg:
		if msg.errMsg == "" && len(msg.items) > 0 && msg.dataset == m.dataset {
			m.cacheDataset = msg.dataset
			m.cacheMetrics = msg.items
			if m.overlay == overlayBuilder && m.builderMetricsLoading {
				// builder is open and waiting — feed it; labels wait for user navigation
				m.builderMetricsLoading = false
				m.builderMetrics = msg.items
				m.builderMetricsFiltered = msg.items
				m.builderMetricsIdx = 0
				m.builderMetric = msg.items[0]
			}
		}
		return m, nil

	case builderMetricsMsg:
		m.builderMetricsLoading = false
		if msg.errMsg != "" {
			m.status.Error = "could not load metrics: " + msg.errMsg
			return m, nil
		}
		m.cacheDataset = m.dataset
		m.cacheMetrics = msg.items
		m.builderMetrics = msg.items
		m.builderMetricsFiltered = msg.items
		m.builderMetricsIdx = 0
		if len(m.builderMetrics) > 0 {
			m.builderMetric = m.builderMetrics[0]
		}
		return m, nil

	case builderLabelsMsg:
		m.builderLabelsLoading = false
		m.cancelLabels = nil
		// always cache, even if builder has moved on
		if msg.metric != "" && msg.errMsg == "" {
			if m.cacheLabels == nil {
				m.cacheLabels = make(map[string][]string)
			}
			m.cacheLabels[msg.metric] = msg.items
		}
		// discard if user already navigated to a different metric
		if msg.metric != m.builderCurrentMetric() {
			return m, nil
		}
		if msg.errMsg != "" || len(msg.items) == 0 {
			m.builderLabels = []string{"(any)"}
			m.builderLabelsFiltered = []string{"(any)"}
			m.builderLabelsIdx = 0
			m.builderValues = []string{"(any)"}
			m.builderValuesFiltered = []string{"(any)"}
			return m, nil
		}
		labels := append([]string{"(any)"}, msg.items...)
		m.builderLabels = labels
		m.builderLabelsFiltered = labels
		m.builderLabelsIdx = 1
		// Values are fetched on Enter in col 1 — not auto-triggered here
		return m, nil

	case builderValuesMsg:
		m.builderValuesLoading = false
		m.cancelValues = nil
		// cache non-sentinel results (sentinel = "(any)" label short-circuit returns empty metric/label)
		if msg.metric != "" && msg.label != "" && msg.errMsg == "" {
			if m.cacheValues == nil {
				m.cacheValues = make(map[string]map[string][]string)
			}
			if m.cacheValues[msg.metric] == nil {
				m.cacheValues[msg.metric] = make(map[string][]string)
			}
			m.cacheValues[msg.metric][msg.label] = msg.items
		}
		// update display only when the arrival still matches what the user is viewing
		curMetric := m.builderCurrentMetric()
		curLabel := m.builderCurrentLabel()
		if msg.metric != "" && (msg.metric != curMetric || msg.label != curLabel) {
			return m, nil
		}
		values := append([]string{"(any)"}, msg.items...)
		if msg.errMsg != "" || len(msg.items) == 0 {
			values = []string{"(any)"}
		}
		m.builderValues = values
		m.builderValuesFiltered = values
		m.builderValuesIdx = 0
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
			m.status.Info = ""
			m.chartCursor = 0
			m.chartHover = false
			m.updateTableColumns(msg.metricWidth, msg.valueWidth)
			m.rebuildChart()
			// Auto-focus results table after successful query.
			for i, p := range PromqlNavigationMap {
				if p == "table" {
					m.focused = i
					break
				}
			}
			m.focusSelected()
		} else {
			m.dataRows = []table.Row{}
			m.table = m.table.WithRows([]table.Row{})
			m.tsChart = timeserieslinechart.Model{}
			m.tsChartReady = false
			m.chartCursor = 0
			m.chartHover = false
			m.fetchErrMsg = msg.errMsg
			if m.fetchErrMsg == "" {
				m.fetchErrMsg = "query failed"
			}
			m.status.Error = "query failed"
		}
		return m, nil

	case tea.KeyMsg:
		// ── global shortcuts (work from any state when no overlay is open) ───
		if m.overlay == overlayNone {
			switch msg.Type {
			case tea.KeyCtrlD:
				m.overlay = overlayDataset
				m.spotlightFilter.Focus()
				m.datasetsLoading = true
				return m, fetchMetricDatasets(m.profile)
			case tea.KeyCtrlB:
				// Toggle query panel between Code and Builder mode, focusing the query panel.
				if m.queryMode == "builder" {
					m.queryMode = "code"
				} else {
					m.queryMode = "builder"
				}
				for i, p := range PromqlNavigationMap {
					if p == "query" {
						m.focused = i
						break
					}
				}
				m.focusSelected()
				return m, nil
			case tea.KeyCtrlQ:
				for i, p := range PromqlNavigationMap {
					if p == "query" {
						m.focused = i
						break
					}
				}
				m.focusSelected()
				return m, nil
			}
		}

		// ── builder overlay ──────────────────────────────────────────────────
		if m.overlay == overlayBuilder {
			switch msg.Type {
			case tea.KeyEsc:
				m.overlay = overlayNone
				m.builderFilter.SetValue("")
				m.builderFilter.Blur()
				m.focusSelected()
				return m, nil

			// Ctrl+R inside builder: build expression with current selections and run immediately.
			case tea.KeyCtrlR:
				expr := buildPromqlExpr(m.builderCurrentMetric(), m.builderCurrentLabel(), m.builderCurrentValue())
				newM, cmd := m.runQueryFromBuilder(expr)
				return newM, cmd

			// Enter: wizard progression — each column confirms the selection and moves to the next.
			// On the final column (Values) it also runs the query.
			case tea.KeyEnter:
				switch m.builderCol {
				case 0:
					metric := m.builderCurrentMetric()
					if metric == "" {
						return m, nil
					}
					m.builderMetric = metric
					m.builderLabels, m.builderLabelsFiltered = nil, nil
					m.builderValues, m.builderValuesFiltered = nil, nil
					m.builderLabelsIdx, m.builderValuesIdx = 0, 0
					m.builderCol = 1
					m.builderFilter.SetValue("")
					if m.cancelLabels != nil {
						m.cancelLabels()
					}
					// cache hit — show instantly
					if labels, ok := m.cacheLabels[metric]; ok {
						full := append([]string{"(any)"}, labels...)
						m.builderLabels = full
						m.builderLabelsFiltered = full
						m.builderLabelsIdx = 1
						m.builderLabelsLoading = false
						m.cancelLabels = nil
						return m, nil
					}
					m.builderLabelsLoading = true
					ctx, cancel := context.WithCancel(context.Background())
					m.cancelLabels = cancel
					return m, fetchBuilderLabelsCtx(ctx, m.profile, m.dataset, metric, m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc())

				case 1: // confirm label → fetch values → move to Values column (or run if "(any)")
					label := m.builderCurrentLabel()
					m.builderLabel = label
					m.builderFilter.SetValue("")
					if label == "" || label == "(any)" {
						expr := buildPromqlExpr(m.builderCurrentMetric(), "", "")
						newM, cmd := m.runQueryFromBuilder(expr)
						return newM, cmd
					}
					m.builderValues, m.builderValuesFiltered = nil, nil
					m.builderValuesIdx = 0
					m.builderCol = 2
					if m.cancelValues != nil {
						m.cancelValues()
					}
					// cache hit
					if m.cacheValues != nil {
						if metricVals, ok := m.cacheValues[m.builderCurrentMetric()]; ok {
							if vals, ok2 := metricVals[label]; ok2 {
								full := append([]string{"(any)"}, vals...)
								m.builderValues = full
								m.builderValuesFiltered = full
								m.builderValuesIdx = 1
								m.builderValuesLoading = false
								m.cancelValues = nil
								return m, nil
							}
						}
					}
					m.builderValuesLoading = true
					ctx2, cancel2 := context.WithCancel(context.Background())
					m.cancelValues = cancel2
					return m, fetchBuilderValuesCtx(ctx2, m.profile, m.dataset, m.builderCurrentMetric(), label, m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc())

				case 2:
					expr := buildPromqlExpr(m.builderCurrentMetric(), m.builderCurrentLabel(), m.builderCurrentValue())
					newM, cmd := m.runQueryFromBuilder(expr)
					return newM, cmd
				}
				return m, nil

			case tea.KeyTab:
				m.builderCol = (m.builderCol + 1) % 3
				m.builderFilter.SetValue("")
				return m, nil

			case tea.KeyShiftTab:
				m.builderCol = (m.builderCol + 2) % 3
				m.builderFilter.SetValue("")
				return m, nil

			case tea.KeyUp:
				switch m.builderCol {
				case 0:
					if m.builderMetricsIdx > 0 {
						m.builderMetricsIdx--
					}
				case 1:
					if m.builderLabelsIdx > 0 {
						m.builderLabelsIdx--
					}
				case 2:
					if m.builderValuesIdx > 0 {
						m.builderValuesIdx--
					}
				}
				return m, nil

			case tea.KeyDown:
				switch m.builderCol {
				case 0:
					if m.builderMetricsIdx < len(m.builderMetricsFiltered)-1 {
						m.builderMetricsIdx++
					}
				case 1:
					if m.builderLabelsIdx < len(m.builderLabelsFiltered)-1 {
						m.builderLabelsIdx++
					}
				case 2:
					if m.builderValuesIdx < len(m.builderValuesFiltered)-1 {
						m.builderValuesIdx++
					}
				}
				return m, nil

			default:
				prev := m.builderFilter.Value()
				m.builderFilter, cmd = m.builderFilter.Update(msg)
				cmds = append(cmds, cmd)
				if m.builderFilter.Value() != prev {
					filter := m.builderFilter.Value()
					switch m.builderCol {
					case 0:
						m.builderMetricsFiltered = filterDatasets(m.builderMetrics, filter)
						m.builderMetricsIdx = 0
					case 1:
						m.builderLabelsFiltered = filterBuilderList(m.builderLabels, filter)
						m.builderLabelsIdx = 0
					case 2:
						m.builderValuesFiltered = filterBuilderList(m.builderValues, filter)
						m.builderValuesIdx = 0
					}
				}
				return m, tea.Batch(cmds...)
			}
		}

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
					newDS := m.filteredDatasets[m.datasetSelectedIdx]
					if newDS != m.dataset {
						m.dataset = newDS
						m.query.SetValue("")
						// clear stale cache and warm fresh one in background
						m.cacheDataset = ""
						m.cacheMetrics = nil
						m.cacheLabels = nil
						m.cacheValues = nil
						m.overlay = overlayNone
						m.spotlightFilter.SetValue("")
						m.spotlightFilter.Blur()
						m.focused = 0 // focus query editor
						m.focusSelected()
						return m, fetchCacheMetrics(m.profile, m.dataset)
					}
				}
				// same dataset or empty list — close picker, preserve editor state
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

		// Up/Down navigate within the sidebar (dataset ↔ step) as an
		// alternative to Tab so the user can stay in the sidebar area.
		if msg.Type == tea.KeyDown && m.currentFocus() == "dataset" {
			for i, p := range PromqlNavigationMap {
				if p == "step" {
					m.focused = i
					break
				}
			}
			m.focusSelected()
			return m, nil
		}
		if msg.Type == tea.KeyUp && m.currentFocus() == "step" {
			for i, p := range PromqlNavigationMap {
				if p == "dataset" {
					m.focused = i
					break
				}
			}
			m.focusSelected()
			return m, nil
		}

		// Enter on dataset → open spotlight
		if msg.Type == tea.KeyEnter && m.currentFocus() == "dataset" {
			m.overlay = overlayDataset
			m.spotlightFilter.Focus()
			m.datasetsLoading = true
			return m, fetchMetricDatasets(m.profile)
		}

		// Enter on time → open time overlay
		if msg.Type == tea.KeyEnter && m.currentFocus() == "time" {
			m.overlay = overlayInputs
			return m, nil
		}

		// Enter on query panel in builder mode → open builder overlay
		if msg.Type == tea.KeyEnter && m.currentFocus() == "query" && m.queryMode == "builder" {
			return m, m.openBuilderOverlay()
		}

		// Ctrl+R or Alt+Enter (≈ Cmd+Enter with meta config) → run query
		isAltEnter := msg.Alt && msg.Type == tea.KeyEnter
		if msg.Type == tea.KeyCtrlR || isAltEnter {
			if m.dataset == "" {
				m.overlay = overlayDataset
				m.spotlightFilter.Focus()
				m.datasetsLoading = true
				m.status.Error = "select a dataset first"
				return m, fetchMetricDatasets(m.profile)
			}
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

		// 't' on results panel toggles chart/table view
		if msg.String() == "t" && m.currentFocus() == "table" && m.overlay == overlayNone {
			m.chartMode = !m.chartMode
			m.rebuildChart()
			return m, nil
		}
		if m.chartMode && m.currentFocus() == "table" && m.overlay == overlayNone {
			switch {
			case msg.Type == tea.KeyLeft || msg.String() == "[":
				m.moveChartCursor(-1)
				m.rebuildChart()
				return m, nil
			case msg.Type == tea.KeyRight || msg.String() == "]":
				m.moveChartCursor(1)
				m.rebuildChart()
				return m, nil
			}
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		default:
			switch m.currentFocus() {
			case "query":
				if m.queryMode == "code" {
					m.query, cmd = m.query.Update(msg)
					cmds = append(cmds, cmd)
				}
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

// runQueryFromBuilder sets the expression, switches to code mode, closes the
// builder overlay, and fires the query.
func (m *PromqlModel) runQueryFromBuilder(expr string) (PromqlModel, tea.Cmd) {
	if expr != "" {
		m.query.SetValue(expr)
		m.query.CursorEnd()
	}
	m.queryMode = "code"
	m.overlay = overlayNone
	m.builderFilter.SetValue("")
	m.builderFilter.Blur()
	// return focus to query panel
	for i, p := range PromqlNavigationMap {
		if p == "query" {
			m.focused = i
			break
		}
	}
	m.focusSelected()
	if m.query.Value() == "" {
		return *m, nil
	}
	m.status.Error = ""
	m.status.Info = ""
	m.loading = true
	m.hasQueried = true
	return *m, tea.Batch(m.spinner.Tick,
		NewPromqlFetchTask(m.profile, m.query.Value(), m.dataset, m.step,
			m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc(), m.instant))
}

func (m *PromqlModel) openBuilderOverlay() tea.Cmd {
	m.overlay = overlayBuilder
	m.builderCol = 0
	m.builderMetric, m.builderLabel, m.builderValue = "", "", ""
	m.builderMetricsIdx = 0
	m.builderLabelsIdx, m.builderValuesIdx = 0, 0
	m.builderLabels, m.builderLabelsFiltered = nil, nil
	m.builderValues, m.builderValuesFiltered = nil, nil
	m.builderLabelsLoading = false
	m.builderValuesLoading = false
	m.builderFilter.SetValue("")
	m.builderFilter.Focus()

	if m.dataset == "" {
		m.builderMetrics, m.builderMetricsFiltered = nil, nil
		m.builderMetricsLoading = false
		return nil
	}
	if m.cacheDataset == m.dataset && len(m.cacheMetrics) > 0 {
		m.builderMetrics = m.cacheMetrics
		m.builderMetricsFiltered = m.cacheMetrics
		m.builderMetricsLoading = false
		m.builderMetric = m.cacheMetrics[0]
		return nil
	}
	m.builderMetrics, m.builderMetricsFiltered = nil, nil
	m.builderMetricsLoading = true
	return fetchCacheMetrics(m.profile, m.dataset)
}

// ─── view ────────────────────────────────────────────────────────────────────

func (m PromqlModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	p := ui.Active

	// ── Status + help (precompute heights) ──────────────────────────
	if m.loading {
		m.status.Info = ""
		m.status.Error = ""
	}
	m.status.SetMode("PromQL")
	bottomView := buildPromqlBottomBar(m, m.width)
	bottomHeight := lipgloss.Height(bottomView)

	topH := 13
	sidebarW := 30
	if m.width >= 140 {
		sidebarW = 34
	}
	if m.width < 100 {
		sidebarW = 26
	}
	// editorW reserves 1 col for the horizontal gap between editor
	// and sidebar so the two `│` borders aren't flush against each
	// other.
	editorW := m.width - sidebarW - 1
	if editorW < 30 {
		editorW = 30
	}
	m.query.SetWidth(editorW - 6)
	editorBodyH := topH - 4 // border(2) + title(1) + spacer(1)
	if editorBodyH < 1 {
		editorBodyH = 1
	}
	m.query.SetHeight(editorBodyH)

	var editorBody string
	if m.queryMode == "builder" {
		expr := m.query.Value()
		if expr == "" {
			editorBody = lipgloss.NewStyle().
				Foreground(p.Faint).
				Italic(true).
				Render("Press Enter to open builder…")
		} else {
			editorBody = lipgloss.NewStyle().
				Foreground(p.Accent).
				Bold(true).
				Render(expr)
		}
	} else {
		editorBody = m.query.View()
	}

	editorFocused := m.currentFocus() == "query"
	editorPane := renderPromqlEditorPane(editorBody, editorW, topH, editorFocused, m.queryMode == "builder")

	rangeMode := "range"
	if m.instant {
		rangeMode = "instant"
	}
	stepHi := m.currentFocus() == "step"
	dsHi := m.currentFocus() == "dataset"
	dataset := m.dataset
	if dataset == "" {
		dataset = "select-dataset"
	}
	timeHi := m.currentFocus() == "time"
	// Two stacked sidebar boxes. Borders touch — same zero-gap join
	// used between the top section and the results pane.
	// Controls (7 rows) + Date (6 rows) = 13 = topH.
	// When step is focused, pass the live textinput View() so its cursor is visible.
	stepDisplay := m.step
	if stepHi {
		stepDisplay = m.stepInput.View()
	}
	controlsBox := renderPromqlControlsBox(
		dataset, stepDisplay, rangeMode,
		sidebarW, 7,
		dsHi, stepHi, m.instant,
	)
	dateBox := renderPromqlDateBox(
		m.timeRange.start.Value(), m.timeRange.end.Value(),
		sidebarW, 6,
		timeHi, m.instant,
	)
	sidebarPane := lipgloss.JoinVertical(lipgloss.Left, controlsBox, dateBox)

	gap := lipgloss.NewStyle().Width(1).Height(topH).Render("")
	topSection := lipgloss.JoinHorizontal(lipgloss.Top, editorPane, gap, sidebarPane)

	// ── Results pane ─────────────────────────────────────────────────
	availH := m.height - topH - bottomHeight
	if availH < 6 {
		availH = 6
	}
	resultsInnerH := availH - 3
	if resultsInnerH < 3 {
		resultsInnerH = 3
	}
	resultsInnerW := m.width - 4
	if resultsInnerW < 10 {
		resultsInnerW = 10
	}

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
		if m.chartMode {
			// Render chart view
			inner = m.renderChart(resultsInnerW, resultsInnerH)
		} else {
			// Render table view
			pageSize := resultsInnerH - 5
			if pageSize < 1 {
				pageSize = 1
			}
			m.table = m.table.WithPageSize(pageSize).WithRows(m.dataRows).WithTargetWidth(resultsInnerW)
			tableStr := m.table.View()
			tableLines := strings.Split(tableStr, "\n")
			// strip any trailing blank line the table renderer may emit
			for len(tableLines) > 0 && tableLines[len(tableLines)-1] == "" {
				tableLines = tableLines[:len(tableLines)-1]
			}

			var bottomRule string
			if len(tableLines) > 0 {
				bottomRule = tableLines[len(tableLines)-1]
				tableLines = tableLines[:len(tableLines)-1]
			}
			tableBodyH := len(tableLines) + 1 // +1 for the bottom rule line
			paddingH := resultsInnerH - 1 - tableBodyH
			if paddingH < 0 {
				paddingH = 0
			}
			// placeholder row — matches table row indent (no left border in customBorder)
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
			gap := resultsInnerW - 2 - lipgloss.Width(leftR) - lipgloss.Width(rightR)
			if gap < 1 {
				gap = 1
			}
			footerLine := lipgloss.NewStyle().Width(resultsInnerW).Padding(0, 1).
				Render(leftR + strings.Repeat(" ", gap) + rightR)
			// assemble: body rows → "--" placeholders → closing rule → footer
			parts := make([]string, 0, len(tableLines)+paddingH+3)
			parts = append(parts, strings.Join(tableLines, "\n"))
			for i := 0; i < paddingH; i++ {
				parts = append(parts, dashRow)
			}
			parts = append(parts, bottomRule)
			parts = append(parts, footerLine)
			inner = strings.Join(parts, "\n")
		}
	}
	{
		lines := strings.Split(inner, "\n")
		if len(lines) > resultsInnerH {
			lines = lines[:resultsInnerH]
		}
		inner = strings.Join(lines, "\n")
	}
	var resultsTitleRight string
	resultsTitle := "RESULTS | Table View"
	if m.chartMode {
		resultsTitle = "RESULTS | Chart View"
		resultsTitleRight = lipgloss.NewStyle().
			Foreground(p.Accent).
			Bold(true).
			Render(m.chartTimeRangeTitle())
	}
	resultsPane := renderPromqlResultsPane(inner, m.width, availH, m.currentFocus() == "table", resultsTitle, resultsTitleRight)

	// ── Compose body or overlay ──────────────────────────────────────
	body := lipgloss.JoinVertical(lipgloss.Left, topSection, resultsPane)
	var mainView string
	switch m.overlay {
	case overlayNone:
		mainView = body
	case overlayInputs:
		timeView := m.timeRange.View()
		mainView = lipgloss.Place(m.width, m.height-bottomHeight,
			lipgloss.Center, lipgloss.Center, timeView,
			lipgloss.WithWhitespaceChars(" "),
		)
	case overlayDataset:
		spotlight := m.renderSpotlight()
		mainView = lipgloss.Place(m.width, m.height-bottomHeight,
			lipgloss.Center, lipgloss.Center, spotlight,
			lipgloss.WithWhitespaceChars(" "),
		)
	case overlayBuilder:
		builder := m.renderBuilder()
		mainView = lipgloss.Place(m.width, m.height-bottomHeight,
			lipgloss.Center, lipgloss.Center, builder,
			lipgloss.WithWhitespaceChars(" "),
		)
	}

	render := lipgloss.JoinVertical(lipgloss.Left,
		mainView,
		bottomView,
	)
	return lipgloss.NewStyle().Width(m.width).Render(render)
}

func renderPromqlResultsPane(body string, width, height int, focused bool, title string, right string) string {
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

	left := lipgloss.NewStyle().Foreground(titleFg).Bold(focused).Render(title)
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

func renderPromqlEditorPane(body string, width, height int, focused, builder bool) string {
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

	left := lipgloss.NewStyle().Foreground(titleFg).Bold(focused).Render("EDITOR")

	activeStyle := lipgloss.NewStyle().Foreground(p.Active).Bold(true)
	idle := lipgloss.NewStyle().Foreground(p.Faint)
	sepStyle := lipgloss.NewStyle().Foreground(p.Faint)
	sep := sepStyle.Render(" | ")
	var right string
	if builder {
		right = idle.Render("Code") + sep + activeStyle.Render("Builder")
	} else {
		right = activeStyle.Render("Code") + sep + idle.Render("Builder")
	}

	gap := innerW - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	titleRow := lipgloss.NewStyle().Width(innerW).Padding(0, 1).Render(
		left + strings.Repeat(" ", gap) + right,
	)
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

// sidebarStyles returns the shared label/value/rail styles used by
// the controls and date sidebar boxes.
func sidebarStyles() (dim, val, hi lipgloss.Style, rail string) {
	p := ui.Active
	dim = lipgloss.NewStyle().Foreground(p.Faint)
	val = lipgloss.NewStyle().Foreground(p.Body)
	hi = lipgloss.NewStyle().Foreground(p.Active).Bold(true)
	rail = lipgloss.NewStyle().Background(p.Active).Render(" ")
	return
}

func renderPromqlControlsBox(dataset, step, mode string, width, height int, datasetHi, stepHi, instant bool) string {
	p := ui.Active
	innerW := width - 2
	if innerW < 4 {
		innerW = 4
	}
	innerH := height - 2
	if innerH < 1 {
		innerH = 1
	}
	dim, val, hi, rail := sidebarStyles()
	prefix := func(active bool) string {
		if active {
			return rail + " "
		}
		return "  "
	}
	dLabel := dim
	if datasetHi {
		dLabel = hi
	}
	sLabel := dim
	if stepHi {
		sLabel = hi
	}
	// When step is focused, `step` is already the textinput View() string
	// (cursor included); render it directly to avoid stripping the cursor.
	stepVal := val.Render(step)
	if stepHi {
		stepVal = step
	}
	// In instant mode step is irrelevant — show it greyed out with a dash.
	if instant {
		sLabel = lipgloss.NewStyle().Foreground(p.Ghost)
		stepVal = lipgloss.NewStyle().Foreground(p.Ghost).Render("—")
	}
	modeLabel := sLabel
	if stepHi && instant {
		modeLabel = hi
	}
	lines := []string{
		prefix(datasetHi) + dLabel.Render("DATASET"),
		prefix(datasetHi) + val.Render(dataset),
		"",
		prefix(stepHi) + sLabel.Render("STEP  ") + stepVal,
		prefix(stepHi) + modeLabel.Render("MODE  ") + val.Render(mode),
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

func renderPromqlDateBox(start, end string, width, height int, timeHi, instant bool) string {
	p := ui.Active
	innerW := width - 2
	if innerW < 4 {
		innerW = 4
	}
	innerH := height - 2
	if innerH < 1 {
		innerH = 1
	}
	dim, val, hi, rail := sidebarStyles()
	prefix := "  "
	if timeHi {
		prefix = rail + " "
	}
	label := dim
	if timeHi {
		label = hi
	}
	lines := []string{}
	if !instant {
		lines = append(lines,
			prefix+label.Render("FROM"),
			prefix+val.Render(start),
		)
	}
	lines = append(lines,
		prefix+label.Render("TO"),
		prefix+val.Render(end),
	)
	body := lipgloss.NewStyle().
		Width(innerW).
		Height(innerH).
		Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(p.Border).
		Render(body)
}

// buildPromqlBottomBar — two-line footer matching SQL view design.
// Line 1: shortcuts. Line 2: hairline. Line 3: Parseable <url> left · MODE right.
func buildPromqlBottomBar(m PromqlModel, width int) string {
	p := ui.Active

	keyStyle := lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(p.Faint)
	sepStyle := lipgloss.NewStyle().Foreground(p.BorderSoft)

	const pad = 2
	innerW := width - pad*2
	if innerW < 1 {
		innerW = 1
	}
	padding := strings.Repeat(" ", pad)

	// ── Line 1: shortcuts ─────────────────────────────────────────
	hints := promqlKeysForFocus(m)
	var keyParts []string
	for _, h := range hints {
		k := strings.TrimSuffix(strings.TrimPrefix(h.Key, "<"), ">")
		keyParts = append(keyParts,
			keyStyle.Render("<"+k+">")+labelStyle.Render(" "+strings.ToLower(h.Label)),
		)
	}
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

// promqlKeysForFocus returns context-aware keybind hints for the
// HeaderStrip. Each focused pane / overlay surfaces its real keys.
func promqlKeysForFocus(m PromqlModel) []ui.KeyHint {
	common := []ui.KeyHint{
		{Key: "<tab>", Label: "Next pane"},
		{Key: "<shift+tab>", Label: "Prev pane"},
		{Key: "<ctrl-r>", Label: "Run"},
		{Key: "<ctrl-c>", Label: "Quit"},
	}
	switch m.overlay {
	case overlayDataset:
		return []ui.KeyHint{
			{Key: "<↑/↓>", Label: "Navigate"},
			{Key: "<enter>", Label: "Select"},
			{Key: "<esc>", Label: "Cancel"},
			{Key: "type", Label: "Filter"},
		}
	case overlayBuilder:
		return []ui.KeyHint{
			{Key: "<↑/↓>", Label: "Navigate"},
			{Key: "<enter>", Label: "Next col"},
			{Key: "<tab>", Label: "Cycle col"},
			{Key: "<ctrl-r>", Label: "Run"},
			{Key: "<esc>", Label: "Cancel"},
		}
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
			{Key: "<enter>", Label: "Open picker"},
			{Key: "<ctrl-d>", Label: "Datasets"},
		}, common...)
	case "query":
		hints := []ui.KeyHint{
			{Key: "<ctrl-b>", Label: "Toggle builder"},
		}
		if m.queryMode == "builder" {
			hints = append(hints, ui.KeyHint{Key: "<enter>", Label: "Open builder"})
		}
		return append(hints, common...)
	case "time":
		return append([]ui.KeyHint{
			{Key: "<enter>", Label: "Open picker"},
		}, common...)
	case "step":
		return append([]ui.KeyHint{
			{Key: "type", Label: "Edit (15s, 5m, 1h)"},
			{Key: "<space>", Label: "Toggle range/instant"},
		}, common...)
	case "table":
		if m.chartMode {
			return append([]ui.KeyHint{
				{Key: "<←/→>", Label: "Inspect"},
				{Key: "<t>", Label: "Toggle chart/table"},
			}, common...)
		}
		hints := []ui.KeyHint{
			{Key: "<↑/↓>", Label: "Row"},
			{Key: "</>", Label: "Filter"},
			{Key: "<t>", Label: "Toggle chart/table"},
		}
		return append(hints, common...)
	}
	return common
}

// renderSpotlight builds the dataset picker modal — flat NormalBorder
// frame, UPPERCASE title + count on top row, inline prompt (no inner
// box), and clean list rows. Matches the main view's chrome.
func (m PromqlModel) renderSpotlight() string {
	p := ui.Active
	// content area inside border(2) + h-padding(4)
	innerW := spotlightWidth - 6
	if innerW < 20 {
		innerW = 20
	}

	// Header: title left, count right
	titleLeft := lipgloss.NewStyle().
		Foreground(p.Accent).
		Bold(true).
		Render("SELECT DATASET")
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

	// Inline filter prompt — no inner border. The textinput renders
	// its own prompt + cursor; we just place the row inside the body.
	searchRow := lipgloss.NewStyle().Width(innerW).Render(m.spotlightFilter.View())

	// List
	var listLines []string
	switch {
	case m.datasetsLoading:
		listLines = append(listLines, lipgloss.NewStyle().
			Foreground(p.Faint).
			Width(innerW).
			Padding(1, 0).
			Render("  "+m.spinner.View()+" loading…"))
	case len(m.filteredDatasets) == 0:
		listLines = append(listLines, lipgloss.NewStyle().
			Foreground(p.Faint).
			Width(innerW).
			Padding(1, 0).
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
				name := lipgloss.NewStyle().
					Foreground(p.Active).
					Bold(true).
					Render(ds)
				listLines = append(listLines, rail+" "+name)
			} else {
				name := lipgloss.NewStyle().Foreground(p.Body).Render(ds)
				listLines = append(listLines, "  "+name)
			}
		}
		if len(m.filteredDatasets) > spotlightMaxItems {
			more := lipgloss.NewStyle().
				Foreground(p.Faint).
				Width(innerW).
				Align(lipgloss.Right).
				Render(fmt.Sprintf("+%d more", len(m.filteredDatasets)-spotlightMaxItems))
			listLines = append(listLines, more)
		}
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		header,
		rule,
		"",
		searchRow,
		"",
		strings.Join(listLines, "\n"),
	)

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(p.Border).
		Padding(1, 2).
		Width(spotlightWidth).
		Render(body)
}

// updateTableColumns rebuilds table columns. valueWidth is inferred from data;
// the metric column is a flex column that fills all remaining width automatically.
func (m *PromqlModel) updateTableColumns(_, valueWidth int) {
	if valueWidth < len(promqlValueKey) {
		valueWidth = len(promqlValueKey)
	}
	columns := []table.Column{
		table.NewColumn(promqlTimestampKey, "TIMESTAMP", promqlTimestampWidth),
		table.NewFlexColumn(promqlMetricKey, "METRIC", 1).WithFiltered(true),
		table.NewColumn(promqlValueKey, "VALUE", valueWidth).WithFiltered(true),
	}
	m.table = m.table.WithColumns(columns).WithTargetWidth(m.width).WithRows(m.dataRows)
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
	valueWidth = 14

	addRow := func(rowData table.RowData) {
		rows = append(rows, table.NewRow(rowData))
	}

	for _, series := range result.Data.Result {
		metricStr := promqlModelFormatLabels(series.Metric)
		if len(metricStr) > metricWidth {
			metricWidth = len(metricStr)
		}

		switch result.Data.ResultType {
		case "vector":
			if len(series.Value) == 2 {
				tsFull := promqlModelFormatTS(series.Value[0])
				ts := trimTimestampToHMS(tsFull)
				val := fmt.Sprintf("%v", series.Value[1])
				if len(val) > valueWidth {
					valueWidth = len(val)
				}
				addRow(table.RowData{
					promqlTimestampKey:     ts,
					promqlTimestampFullKey: tsFull,
					promqlMetricKey:        metricStr,
					promqlValueKey:         val,
				})
			}
		case "matrix":
			for _, pt := range series.Values {
				if len(pt) == 2 {
					tsFull := promqlModelFormatTS(pt[0])
					ts := trimTimestampToHMS(tsFull)
					val := fmt.Sprintf("%v", pt[1])
					if len(val) > valueWidth {
						valueWidth = len(val)
					}
					addRow(table.RowData{
						promqlTimestampKey:     ts,
						promqlTimestampFullKey: tsFull,
						promqlMetricKey:        metricStr,
						promqlValueKey:         val,
					})
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
		sec, frac := math.Modf(f)
		nsec := int64(frac * float64(time.Second))
		return time.Unix(int64(sec), nsec).In(time.Local).Format("2006-01-02T15:04:05-07:00")
	}
	if s, ok := v.(string); ok {
		if t, parsed := parsePromqlChartTime(s); parsed {
			return t.Format("2006-01-02T15:04:05-07:00")
		}
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

// filterBuilderList filters a column list, always keeping "(any)" at index 0.
func filterBuilderList(all []string, query string) []string {
	if query == "" {
		return all
	}
	q := strings.ToLower(query)
	var out []string
	for _, item := range all {
		if item == "(any)" {
			continue
		}
		if strings.Contains(strings.ToLower(item), q) {
			out = append(out, item)
		}
	}
	if len(all) > 0 && all[0] == "(any)" {
		return append([]string{"(any)"}, out...)
	}
	return out
}

// ─── builder helpers ──────────────────────────────────────────────────────────

func builderColWidth(w int) int {
	cw := (w - 14) / 3
	if cw < 18 {
		cw = 18
	}
	return cw
}

func (m PromqlModel) builderCurrentMetric() string {
	if len(m.builderMetricsFiltered) == 0 {
		return ""
	}
	idx := m.builderMetricsIdx
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.builderMetricsFiltered) {
		idx = len(m.builderMetricsFiltered) - 1
	}
	return m.builderMetricsFiltered[idx]
}

func (m PromqlModel) builderCurrentLabel() string {
	if len(m.builderLabelsFiltered) == 0 {
		return ""
	}
	idx := m.builderLabelsIdx
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.builderLabelsFiltered) {
		idx = len(m.builderLabelsFiltered) - 1
	}
	return m.builderLabelsFiltered[idx]
}

func (m PromqlModel) builderCurrentValue() string {
	if len(m.builderValuesFiltered) == 0 {
		return ""
	}
	idx := m.builderValuesIdx
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.builderValuesFiltered) {
		idx = len(m.builderValuesFiltered) - 1
	}
	return m.builderValuesFiltered[idx]
}

func escapePromQLValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

func buildPromqlExpr(metric, label, value string) string {
	if metric == "" {
		return ""
	}
	if label == "" || label == "(any)" {
		return metric
	}
	if value == "" || value == "(any)" {
		return fmt.Sprintf(`%s{%s!=""}`, metric, label)
	}
	return fmt.Sprintf(`%s{%s="%s"}`, metric, label, escapePromQLValue(value))
}

func renderBuilderCol(title string, items []string, selectedIdx int, loading, focused bool, colW int) string {
	p := ui.Active
	innerW := colW - 2

	borderColor := p.Border
	titleFg := p.Faint
	if focused {
		borderColor = p.BorderHi
		titleFg = p.Accent
	}
	titleRow := lipgloss.NewStyle().
		Foreground(titleFg).
		Bold(true).
		Width(innerW).
		Padding(0, 1).
		Render(strings.ToUpper(title))

	var rows []string
	switch {
	case loading:
		rows = append(rows, lipgloss.NewStyle().
			Foreground(p.Faint).Width(innerW).Padding(0, 1).
			Render("loading..."))
	case len(items) == 0:
		rows = append(rows, lipgloss.NewStyle().
			Foreground(p.Faint).Width(innerW).Padding(0, 1).
			Render("(empty)"))
	default:
		start := 0
		if selectedIdx >= builderMaxItems {
			start = selectedIdx - builderMaxItems + 1
		}
		end := start + builderMaxItems
		if end > len(items) {
			end = len(items)
		}
		rail := lipgloss.NewStyle().Background(p.Active).Render(" ")
		for i := start; i < end; i++ {
			item := items[i]
			maxLen := innerW - 4
			if maxLen > 3 && len(item) > maxLen {
				item = item[:maxLen-3] + "..."
			}
			if i == selectedIdx {
				name := lipgloss.NewStyle().
					Foreground(p.Active).
					Bold(true).
					Render(item)
				rows = append(rows, " "+rail+" "+name)
			} else {
				name := lipgloss.NewStyle().Foreground(p.Body).Render(item)
				rows = append(rows, "   "+name)
			}
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		titleRow,
		strings.Join(rows, "\n"),
	)

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Width(colW).
		Render(content)
}

// renderBuilder builds the 3-column query builder overlay — flat
// NormalBorder, UPPERCASE title, plain bg. Matches the main view.
func (m PromqlModel) renderBuilder() string {
	p := ui.Active
	colW := builderColWidth(m.width)

	metricsItems := m.builderMetricsFiltered
	if m.dataset == "" {
		metricsItems = []string{"── select a dataset first ──"}
	}
	col0 := renderBuilderCol("metrics", metricsItems, m.builderMetricsIdx,
		m.builderMetricsLoading, m.builderCol == 0, colW)
	col1 := renderBuilderCol("labels", m.builderLabelsFiltered, m.builderLabelsIdx,
		m.builderLabelsLoading, m.builderCol == 1, colW)
	col2 := renderBuilderCol("values", m.builderValuesFiltered, m.builderValuesIdx,
		m.builderValuesLoading, m.builderCol == 2, colW)

	columns := lipgloss.JoinHorizontal(lipgloss.Top, col0, col1, col2)
	colsW := lipgloss.Width(columns)

	expr := buildPromqlExpr(m.builderCurrentMetric(), m.builderCurrentLabel(), m.builderCurrentValue())
	exprLine := lipgloss.NewStyle().Foreground(p.Faint).Render("built  ") +
		lipgloss.NewStyle().Foreground(p.Accent).Bold(true).Render(expr)

	searchBar := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(p.Border).
		Width(colsW-4).
		Padding(0, 1).
		Render(m.builderFilter.View())

	title := lipgloss.NewStyle().
		Foreground(p.Accent).
		Bold(true).
		Width(colsW).
		Align(lipgloss.Center).
		Render("PROMQL QUERY BUILDER")

	body := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		columns,
		"",
		exprLine,
		"",
		searchBar,
	)

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(p.Border).
		Padding(1, 2).
		Render(body)
}

// ─── builder async commands ───────────────────────────────────────────────────

// fetchMetricDatasets fetches all streams and keeps those whose name contains "metrics"
// (case-insensitive). Falls back to all datasets when none match.
func fetchMetricDatasets(profile config.Profile) tea.Cmd {
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
		// Fall back to name-contains-"metrics" only for servers that don't
		// return stream_type — never return everything unfiltered.
		hasType := false
		for _, item := range items {
			if item.StreamType != "" {
				hasType = true
				break
			}
		}
		var all, typed []string
		for _, item := range items {
			all = append(all, item.Name)
			if hasType {
				if item.StreamType == "Metrics" {
					typed = append(typed, item.Name)
				}
			} else {
				if strings.Contains(strings.ToLower(item.Name), "metrics") {
					typed = append(typed, item.Name)
				}
			}
		}
		datasets := typed
		if len(datasets) == 0 {
			datasets = all
		}
		sort.Strings(datasets)
		return datasetListMsg{datasets: datasets}
	}
}

type promqlLabelListResp struct {
	Status string   `json:"status"`
	Data   []string `json:"data"`
	Error  string   `json:"error,omitempty"`
}

func builderHTTPGetCtx(ctx context.Context, profile config.Profile, rawURL string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
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
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("HTTP %s: %s", resp.Status, msg)
	}
	return body, nil
}

func fetchBuilderLabelsCtx(ctx context.Context, profile config.Profile, dataset, metric, startTime, endTime string) tea.Cmd {
	return func() tea.Msg {
		base, err := url.JoinPath(profile.URL, "prometheus/api/v1/labels")
		if err != nil {
			return builderLabelsMsg{metric: metric, errMsg: err.Error()}
		}
		rawURL := base + "?stream=" + url.QueryEscape(dataset)
		if startTime != "" {
			rawURL += "&start=" + url.QueryEscape(startTime)
		}
		if endTime != "" {
			rawURL += "&end=" + url.QueryEscape(endTime)
		}
		if metric != "" {
			rawURL += "&match[]=" + url.QueryEscape(metric)
		}
		body, err := builderHTTPGetCtx(ctx, profile, rawURL)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return builderLabelsMsg{metric: metric, errMsg: err.Error()}
		}
		var resp promqlLabelListResp
		if err := json.Unmarshal(body, &resp); err != nil {
			return builderLabelsMsg{metric: metric, errMsg: err.Error()}
		}
		if resp.Status == "error" {
			return builderLabelsMsg{metric: metric, errMsg: resp.Error}
		}
		var labels []string
		for _, l := range resp.Data {
			if l != "__name__" {
				labels = append(labels, l)
			}
		}
		return builderLabelsMsg{metric: metric, items: labels}
	}
}

func fetchBuilderValuesCtx(ctx context.Context, profile config.Profile, dataset, metric, label, startTime, endTime string) tea.Cmd {
	if label == "" || label == "(any)" {
		return func() tea.Msg { return builderValuesMsg{} } // sentinel: clear values to [(any)]
	}
	return func() tea.Msg {
		base, err := url.JoinPath(profile.URL, "prometheus/api/v1/label/"+url.PathEscape(label)+"/values")
		if err != nil {
			return builderValuesMsg{metric: metric, label: label, errMsg: err.Error()}
		}
		rawURL := base + "?stream=" + url.QueryEscape(dataset)
		if startTime != "" {
			rawURL += "&start=" + url.QueryEscape(startTime)
		}
		if endTime != "" {
			rawURL += "&end=" + url.QueryEscape(endTime)
		}
		if metric != "" {
			rawURL += "&match[]=" + url.QueryEscape(metric)
		}
		body, err := builderHTTPGetCtx(ctx, profile, rawURL)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return builderValuesMsg{metric: metric, label: label, errMsg: err.Error()}
		}
		var resp promqlLabelListResp
		if err := json.Unmarshal(body, &resp); err != nil {
			return builderValuesMsg{metric: metric, label: label, errMsg: err.Error()}
		}
		if resp.Status == "error" {
			return builderValuesMsg{metric: metric, label: label, errMsg: resp.Error}
		}
		return builderValuesMsg{metric: metric, label: label, items: resp.Data}
	}
}

// fetchCacheMetrics is the background pre-fetch fired on dataset selection.
func fetchCacheMetrics(profile config.Profile, dataset string) tea.Cmd {
	return func() tea.Msg {
		params := url.Values{}
		params.Set("stream", dataset)
		body, err := promqlModelFetch(profile, "prometheus/api/v1/label/__name__/values", params)
		if err != nil {
			return cacheMetricsMsg{dataset: dataset, errMsg: err.Error()}
		}
		var resp promqlLabelListResp
		if err := json.Unmarshal(body, &resp); err != nil {
			return cacheMetricsMsg{dataset: dataset, errMsg: err.Error()}
		}
		if resp.Status == "error" {
			return cacheMetricsMsg{dataset: dataset, errMsg: resp.Error}
		}
		return cacheMetricsMsg{dataset: dataset, items: resp.Data}
	}
}

type promqlChartSeries struct {
	label  string    // metric label (for legend)
	times  []string  // timestamps (X-axis)
	values []float64 // numeric values (Y-axis)
}

// extractChartData extracts value data from PromQL results for charting.
// PromQL results have exactly: timestamp, metric, and value columns.
func (m PromqlModel) extractChartData() []promqlChartSeries {
	if len(m.dataRows) < 2 {
		return nil
	}

	seriesByKey := make(map[string]*promqlChartSeries)
	var order []string
	for _, row := range m.dataRows {
		timeStr, ok := chartRowTime(row)
		if !ok {
			continue
		}

		value, ok := chartRowValue(row)
		if !ok {
			return nil
		}

		metricLabel := "value"
		if met, ok := row.Data[promqlMetricKey]; ok {
			if metStr, ok := met.(string); ok && metStr != "" {
				metricLabel = metStr
			}
		}

		key := promqlChartLabelSet(metricLabel)
		if _, ok := seriesByKey[key]; !ok {
			order = append(order, key)
			seriesByKey[key] = &promqlChartSeries{
				label: metricLabel,
			}
		}
		seriesByKey[key].times = append(seriesByKey[key].times, timeStr)
		seriesByKey[key].values = append(seriesByKey[key].values, value)
	}

	var out []promqlChartSeries
	for _, key := range order {
		series := seriesByKey[key]
		if len(series.values) >= 2 {
			out = append(out, *series)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func chartRowTime(row table.Row) (string, bool) {
	if timeVal, ok := row.Data[promqlTimestampFullKey]; ok {
		return fmt.Sprintf("%v", timeVal), true
	}
	if timeVal, ok := row.Data[promqlTimestampKey]; ok {
		return fmt.Sprintf("%v", timeVal), true
	}
	return "", false
}

func chartRowValue(row table.Row) (float64, bool) {
	val, ok := row.Data[promqlValueKey]
	if !ok {
		return 0, false
	}
	switch v := val.(type) {
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func promqlChartLabelSet(metricLabel string) string {
	open := strings.Index(metricLabel, "{")
	close := strings.LastIndex(metricLabel, "}")
	if open >= 0 && close > open {
		return metricLabel[open+1 : close]
	}
	return metricLabel
}

func aggregateChartSeries(series []promqlChartSeries) promqlChartSeries {
	type aggregatePoint struct {
		t     time.Time
		value float64
		count int
	}

	pointsByTime := make(map[int64]aggregatePoint)
	for _, s := range series {
		for i, timeStr := range s.times {
			if i >= len(s.values) {
				break
			}
			t, ok := parsePromqlChartTime(timeStr)
			if !ok {
				continue
			}

			key := t.UnixNano()
			point := pointsByTime[key]
			point.t = t
			point.value += s.values[i]
			point.count++
			pointsByTime[key] = point
		}
	}

	points := make([]aggregatePoint, 0, len(pointsByTime))
	for _, point := range pointsByTime {
		points = append(points, point)
	}
	sort.Slice(points, func(i, j int) bool {
		return points[i].t.Before(points[j].t)
	})

	out := promqlChartSeries{label: "average"}
	for _, point := range points {
		out.times = append(out.times, point.t.Format("2006-01-02T15:04:05-07:00"))
		out.values = append(out.values, point.value/float64(point.count))
	}
	return out
}

func parsePromqlChartTimes(values []string) []time.Time {
	times := make([]time.Time, 0, len(values))
	for _, value := range values {
		if t, ok := parsePromqlChartTime(value); ok {
			times = append(times, t)
		} else {
			return nil
		}
	}
	return times
}

func parsePromqlChartTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}

	zoneLayouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, layout := range zoneLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t.In(time.Local), true
		}
	}

	localLayouts := []string{
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"15:04:05",
		"15:04",
	}
	for _, layout := range localLayouts {
		if t, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return t, true
		}
	}

	return time.Time{}, false
}

func formatChartTime12h(t time.Time) string {
	return t.Format("3:04pm")
}

func formatChartRangeTime(t time.Time, duration time.Duration) string {
	if isDateChartRange(duration) {
		return t.Format("Jan 2, 2006")
	}
	return formatChartTime12h(t)
}

func isDateChartRange(duration time.Duration) bool {
	return duration >= 48*time.Hour
}

func formatCompactChartValue(value float64) string {
	absValue := math.Abs(value)
	if absValue >= 1000000 {
		formatted := fmt.Sprintf("%.1fM", value/1000000)
		return strings.Replace(formatted, ".0M", "M", 1)
	}
	if absValue >= 1000 {
		formatted := fmt.Sprintf("%.1fk", value/1000)
		return strings.Replace(formatted, ".0k", "k", 1)
	}

	formatted := fmt.Sprintf("%.1f", value)
	return strings.TrimSuffix(formatted, ".0")
}

func (m PromqlModel) chartTimeRangeTitle() string {
	series := m.extractChartData()
	if len(series) == 0 {
		return ""
	}
	aggregatedSeries := aggregateChartSeries(series)
	parsedTimes := parsePromqlChartTimes(aggregatedSeries.times)
	if len(parsedTimes) == 0 {
		return ""
	}
	duration := m.chartSelectedDuration(parsedTimes[0], parsedTimes[len(parsedTimes)-1])
	return fmt.Sprintf("📊 %s → %s",
		formatChartRangeTime(parsedTimes[0], duration),
		formatChartRangeTime(parsedTimes[len(parsedTimes)-1], duration))
}

func (m PromqlModel) chartSelectedDuration(fallbackStart, fallbackEnd time.Time) time.Duration {
	start := m.timeRange.start.Time()
	end := m.timeRange.end.Time()
	if end.After(start) {
		return end.Sub(start)
	}
	return fallbackEnd.Sub(fallbackStart)
}

func (m *PromqlModel) rebuildChart() {
	chart, ok := m.buildChart(m.width-2, m.height-8)
	m.tsChart = chart
	m.tsChartReady = ok
}

func (m PromqlModel) chartTimePoints() []timeserieslinechart.TimePoint {
	series := m.extractChartData()
	if len(series) == 0 {
		return nil
	}

	aggregatedSeries := aggregateChartSeries(series)
	parsedTimes := parsePromqlChartTimes(aggregatedSeries.times)
	if len(aggregatedSeries.values) < 2 || len(parsedTimes) != len(aggregatedSeries.values) {
		return nil
	}

	points := make([]timeserieslinechart.TimePoint, 0, len(aggregatedSeries.values))
	for i, value := range aggregatedSeries.values {
		points = append(points, timeserieslinechart.TimePoint{
			Time:  parsedTimes[i],
			Value: value,
		})
	}
	return points
}

func (m PromqlModel) buildChart(width, height int) (timeserieslinechart.Model, bool) {
	points := m.chartTimePoints()
	if len(points) < 2 {
		return timeserieslinechart.Model{}, false
	}

	if width < 20 {
		width = 20
	}
	if height < 8 {
		height = 8
	}

	p := ui.Active
	axisStyle := lipgloss.NewStyle().Foreground(p.Faint)
	labelStyle := lipgloss.NewStyle().Foreground(p.Faint)
	lineStyle := lipgloss.NewStyle().Foreground(p.Accent)
	yAxis := chartYAxisScale(points, height)
	timeRange := m.chartSelectedDuration(points[0].Time, points[len(points)-1].Time)

	chart := timeserieslinechart.New(
		width,
		height,
		timeserieslinechart.WithAxesStyles(axisStyle, labelStyle),
		timeserieslinechart.WithLineStyle(runes.ThinLineStyle),
		timeserieslinechart.WithStyle(lineStyle),
		timeserieslinechart.WithXYSteps(chartXAxisStep(width), yAxis.rowStep),
		timeserieslinechart.WithTimeRange(points[0].Time, points[len(points)-1].Time),
		timeserieslinechart.WithYRange(yAxis.min, yAxis.max),
		timeserieslinechart.WithXLabelFormatter(func(_ int, value float64) string {
			return ""
		}),
		timeserieslinechart.WithYLabelFormatter(func(_ int, value float64) string {
			return formatCompactChartValue(value)
		}),
	)
	for _, point := range points {
		chart.Push(point)
	}
	chart.Draw()
	m.drawChartXAxisLabels(&chart, points, timeRange)
	m.drawChartCursor(&chart, points)

	return chart, true
}

func chartXAxisStep(width int) int {
	step := width / 8
	if step < 8 {
		return 8
	}
	return step
}

const chartXAxisLabelCount = 8

type chartYAxisConfig struct {
	min     float64
	max     float64
	rowStep int
}

func chartYAxisScale(points []timeserieslinechart.TimePoint, height int) chartYAxisConfig {
	minValue := points[0].Value
	maxValue := points[0].Value
	for _, point := range points[1:] {
		if point.Value < minValue {
			minValue = point.Value
		}
		if point.Value > maxValue {
			maxValue = point.Value
		}
	}

	dataRange := maxValue - minValue
	if dataRange == 0 {
		dataRange = math.Abs(maxValue) * 0.05
		if dataRange == 0 {
			dataRange = 1
		}
		minValue -= dataRange / 2
		maxValue += dataRange / 2
	}

	graphHeight := height - 2
	if graphHeight < 1 {
		graphHeight = 1
	}
	rowStep := chartYAxisRowStep(graphHeight)
	targetIntervals := float64(graphHeight) / float64(rowStep)
	if targetIntervals < 1 {
		targetIntervals = 1
	}

	padding := dataRange * 0.05
	if padding == 0 {
		padding = 1
	}
	paddedMin := minValue - padding
	paddedMax := maxValue + padding
	step := niceChartValueStep((paddedMax - paddedMin) / targetIntervals)
	niceMin := math.Floor(paddedMin/step) * step
	niceMax := niceMin + step*float64(graphHeight)/float64(rowStep)

	for niceMax < paddedMax {
		step = nextNiceChartValueStep(step)
		niceMin = math.Floor(paddedMin/step) * step
		niceMax = niceMin + step*float64(graphHeight)/float64(rowStep)
	}

	return chartYAxisConfig{
		min:     niceMin,
		max:     niceMax,
		rowStep: rowStep,
	}
}

func chartYAxisRowStep(graphHeight int) int {
	switch {
	case graphHeight >= 22:
		return 2
	case graphHeight >= 14:
		return 3
	default:
		return 4
	}
}

func niceChartValueStep(step float64) float64 {
	if step <= 0 {
		return 1
	}

	magnitude := math.Pow(10, math.Floor(math.Log10(step)))
	normalized := step / magnitude

	switch {
	case normalized <= 1:
		return magnitude
	case normalized <= 2:
		return 2 * magnitude
	case normalized <= 2.5:
		return 2.5 * magnitude
	case normalized <= 5:
		return 5 * magnitude
	default:
		return 10 * magnitude
	}
}

func nextNiceChartValueStep(step float64) float64 {
	return niceChartValueStep(step * 1.01)
}

func (m PromqlModel) selectedChartPointIndex(points []timeserieslinechart.TimePoint) (int, bool) {
	if !m.chartHover || len(points) == 0 {
		return 0, false
	}
	idx := m.chartCursor
	if idx < 0 {
		idx = 0
	}
	if idx >= len(points) {
		idx = len(points) - 1
	}
	return idx, true
}

func (m *PromqlModel) moveChartCursor(delta int) {
	points := m.chartTimePoints()
	if len(points) == 0 {
		return
	}
	if !m.chartHover {
		m.chartCursor = 0
		if delta < 0 {
			m.chartCursor = len(points) - 1
		} else if delta > 0 && len(points) > 1 {
			m.chartCursor = 1
		}
		m.chartHover = true
		return
	}

	current := m.chartCursor
	if current < 0 {
		current = 0
	}
	if current >= len(points) {
		current = len(points) - 1
	}

	direction := 1
	if delta < 0 {
		direction = -1
	}
	graphWidth := m.chartCursorGraphWidth()
	first := points[0].Time
	last := points[len(points)-1].Time
	currentX := chartGraphX(points[current].Time, first, last, graphWidth)
	next := current
	for {
		next += direction
		if next < 0 {
			next = 0
			break
		}
		if next >= len(points) {
			next = len(points) - 1
			break
		}
		if chartGraphX(points[next].Time, first, last, graphWidth) != currentX {
			break
		}
	}
	m.chartCursor = next
}

func (m PromqlModel) chartCursorGraphWidth() int {
	if m.tsChartReady && m.tsChart.GraphWidth() > 0 {
		return m.tsChart.GraphWidth()
	}
	width := m.width - 4
	if width < 20 {
		width = 20
	}
	chart, ok := m.buildChart(width, m.height-8)
	if ok && chart.GraphWidth() > 0 {
		return chart.GraphWidth()
	}
	return width
}

func chartGraphX(current, first, last time.Time, graphWidth int) int {
	if graphWidth <= 0 {
		return 0
	}

	total := last.Sub(first)
	if total <= 0 {
		return 0
	}

	elapsed := current.Sub(first)
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed > total {
		elapsed = total
	}

	return int(math.Round(float64(elapsed) / float64(total) * float64(graphWidth)))
}

func (m PromqlModel) drawChartCursor(chart *timeserieslinechart.Model, points []timeserieslinechart.TimePoint) {
	idx, ok := m.selectedChartPointIndex(points)
	if !ok {
		return
	}

	p := ui.Active
	point := points[idx]
	x := chartCursorCanvasX(chart, point.Time)
	y := chartCursorCanvasY(chart, point.Value)
	cursorStyle := lipgloss.NewStyle().Foreground(p.Faint)
	pointStyle := lipgloss.NewStyle().Foreground(p.Accent)

	for row := 0; row < chart.Origin().Y; row++ {
		chart.Canvas.SetCell(canvas.Point{X: x, Y: row}, canvas.NewCellWithStyle(runes.LineVertical, cursorStyle))
	}
	chart.Canvas.SetCell(canvas.Point{X: x, Y: y}, canvas.NewCellWithStyle('●', pointStyle))
}

func chartCursorCanvasX(chart *timeserieslinechart.Model, t time.Time) int {
	x := chartGraphX(t, time.Unix(int64(chart.ViewMinX()), 0), time.Unix(int64(chart.ViewMaxX()), 0), chart.GraphWidth())
	return chart.Origin().X + x
}

func chartCursorCanvasY(chart *timeserieslinechart.Model, value float64) int {
	dy := chart.ViewMaxY() - chart.ViewMinY()
	if dy <= 0 {
		return chart.Origin().Y
	}

	y := int(math.Round((value - chart.ViewMinY()) * float64(chart.Origin().Y) / dy))
	if y < 0 {
		y = 0
	}
	if y > chart.Origin().Y {
		y = chart.Origin().Y
	}
	return chart.Origin().Y - y
}

func (m PromqlModel) drawChartXAxisLabels(chart *timeserieslinechart.Model, points []timeserieslinechart.TimePoint, duration time.Duration) {
	if len(points) < 2 || chart.GraphWidth() <= 0 || chart.Origin().Y+1 >= chart.Height() {
		return
	}

	p := ui.Active
	labelStyle := lipgloss.NewStyle().Foreground(p.Faint)
	labelY := chart.Origin().Y + 1
	for col := 0; col < chart.Width(); col++ {
		chart.Canvas.SetCell(canvas.Point{X: col, Y: labelY}, canvas.NewCellWithStyle(' ', labelStyle))
	}

	first := points[0].Time
	last := points[len(points)-1].Time
	total := last.Sub(first)
	if total <= 0 {
		return
	}

	for i := 0; i < chartXAxisLabelCount; i++ {
		ratio := float64(i) / float64(chartXAxisLabelCount-1)
		t := first.Add(time.Duration(ratio * float64(total)))
		label := formatChartAxisLabel(t, duration, chart.GraphWidth())
		x := chart.Origin().X + int(math.Round(ratio*float64(chart.GraphWidth())))
		placeChartCanvasLabel(chart, labelY, x, label, labelStyle)
	}
}

func formatChartAxisLabel(t time.Time, duration time.Duration, graphWidth int) string {
	if isDateChartRange(duration) && graphWidth < chartXAxisLabelCount*12 {
		return t.Format("Jan 2")
	}
	return formatChartRangeTime(t, duration)
}

func placeChartCanvasLabel(chart *timeserieslinechart.Model, y int, x int, label string, style lipgloss.Style) {
	if label == "" {
		return
	}
	start := x
	if start+len([]rune(label)) > chart.Width() {
		start = chart.Width() - len([]rune(label))
	}
	if start < chart.Origin().X {
		start = chart.Origin().X
	}
	chart.Canvas.SetStringWithStyle(canvas.Point{X: start, Y: y}, label, style)
}

func (m PromqlModel) renderChartInspector(width int, points []timeserieslinechart.TimePoint) string {
	idx, ok := m.selectedChartPointIndex(points)
	if !ok {
		return ""
	}

	p := ui.Active
	point := points[idx]
	dot := lipgloss.NewStyle().Foreground(p.Accent).Render("●")
	timeText := lipgloss.NewStyle().Foreground(p.Body).Render(point.Time.Format("3:04 PM, Jan 2, 2006"))
	separator := lipgloss.NewStyle().Foreground(p.Faint).Render("|")
	name := lipgloss.NewStyle().Foreground(p.Faint).Render("avg")
	value := lipgloss.NewStyle().Foreground(p.Body).Bold(true).Render(formatCompactChartValue(point.Value))
	inspector := fmt.Sprintf("%s  %s  %s %s %s", timeText, separator, dot, name, value)
	if lipgloss.Width(inspector) > width {
		return lipgloss.NewStyle().MaxWidth(width).Render(inspector)
	}
	return inspector
}

// renderChart renders the aggregated PromQL time series using ntcharts.
func (m PromqlModel) renderChart(width, height int) string {
	p := ui.Active

	points := m.chartTimePoints()
	inspector := m.renderChartInspector(width, points)
	chartHeight := height - 1
	if chartHeight < 1 {
		chartHeight = 1
	}
	chart := m.tsChart
	ok := m.tsChartReady
	if ok {
		chart.Resize(width, chartHeight)
		chart.Draw()
		if len(points) >= 2 {
			m.drawChartXAxisLabels(&chart, points, m.chartSelectedDuration(points[0].Time, points[len(points)-1].Time))
		}
		m.drawChartCursor(&chart, points)
	} else {
		chart, ok = m.buildChart(width, chartHeight)
	}
	if !ok {
		series := m.extractChartData()
		message := "not enough data points to plot"
		if len(series) == 0 {
			message = "no numeric data to plot"
		}
		msg := lipgloss.NewStyle().Foreground(p.Faint).Render(message)
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, msg,
			lipgloss.WithWhitespaceChars(" "))
	}

	chartOutput := strings.TrimRight(chart.View(), "\n")
	if chartOutput == "" {
		errMsg := lipgloss.NewStyle().Foreground(p.Err).Render("chart error: failed to generate chart")
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, errMsg,
			lipgloss.WithWhitespaceChars(" "))
	}

	legendTextStyle := lipgloss.NewStyle().Foreground(p.Faint)
	legendDotStyle := lipgloss.NewStyle().Foreground(p.Accent)
	legend := fmt.Sprintf("%s %s", legendDotStyle.Render("●"), legendTextStyle.Render("avg"))
	legendWidth := lipgloss.Width(legend)
	inspectorWidth := lipgloss.Width(inspector)
	gap := width - inspectorWidth - legendWidth
	if gap < 1 {
		gap = 1
	}
	legendContent := inspector + strings.Repeat(" ", gap) + legend
	if inspector == "" {
		legendContent = legend
	}
	legendRow := lipgloss.NewStyle().
		Width(width).
		Align(lipgloss.Right).
		Render(legendContent)

	parts := []string{legendRow, chartOutput}
	body := strings.TrimRight(lipgloss.JoinVertical(lipgloss.Left, parts...), "\n")

	styled := lipgloss.NewStyle().
		Foreground(p.Body).
		Width(width).
		Render(body)

	return strings.TrimRight(styled, "\n")
}
