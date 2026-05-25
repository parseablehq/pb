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
	"net/http"
	"net/url"
	"os"
	"pb/pkg/config"
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
	"golang.org/x/term"
)

// ─── constants ───────────────────────────────────────────────────────────────

const (
	promqlTimestampKey   = "timestamp"
	promqlMetricKey      = "metric"
	promqlValueKey       = "value"
	promqlTimestampWidth = 10 // matches SQL dateTimeWidth (HH:MM:SS + slack)

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
			m.updateTableColumns(msg.metricWidth, msg.valueWidth)
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

	pageSize := resultsInnerH - 5
	if pageSize < 1 {
		pageSize = 1
	}
	m.table = m.table.WithPageSize(pageSize).WithRows(m.dataRows).WithTargetWidth(resultsInnerW)

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
	{
		lines := strings.Split(inner, "\n")
		if len(lines) > resultsInnerH {
			lines = lines[:resultsInnerH]
		}
		inner = strings.Join(lines, "\n")
	}
	resultsPane := renderResultsPane(inner, m.width, availH, 0, m.currentFocus() == "table")

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
	lines := []string{
		prefix(datasetHi) + dLabel.Render("DATASET"),
		prefix(datasetHi) + val.Render(dataset),
		"",
		prefix(stepHi && !instant) + sLabel.Render("STEP  ") + stepVal,
		prefix(stepHi && !instant) + sLabel.Render("MODE  ") + val.Render(mode),
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
		return append([]ui.KeyHint{
			{Key: "<↑/↓>", Label: "Row"},
			{Key: "</>", Label: "Filter"},
		}, common...)
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
				ts := trimTimestampToHMS(promqlModelFormatTS(series.Value[0]))
				val := fmt.Sprintf("%v", series.Value[1])
				if len(val) > valueWidth {
					valueWidth = len(val)
				}
				addRow(table.RowData{
					promqlTimestampKey: ts,
					promqlMetricKey:    metricStr,
					promqlValueKey:     val,
				})
			}
		case "matrix":
			for _, pt := range series.Values {
				if len(pt) == 2 {
					ts := trimTimestampToHMS(promqlModelFormatTS(pt[0]))
					val := fmt.Sprintf("%v", pt[1])
					if len(val) > valueWidth {
						valueWidth = len(val)
					}
					addRow(table.RowData{
						promqlTimestampKey: ts,
						promqlMetricKey:    metricStr,
						promqlValueKey:     val,
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
