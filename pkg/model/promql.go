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

	builderMaxItems = 10
)

// overlay states (overlayNone and overlayInputs are defined in query.go)
const overlayDataset uint = 2
const overlayBuilder uint = 3

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
	builderMetric          string // currently highlighted metric (drives label/value fetch)
	builderLabel           string // currently selected label for preview
	builderValue           string // currently selected value for preview
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
	cancelLabels           context.CancelFunc // aborts in-flight labels request; nil when idle
	cancelValues           context.CancelFunc // aborts in-flight values request; nil when idle

	// query panel mode toggle: "code" (raw textarea) or "builder" (expression breadcrumb + overlay)
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
	q.SetHeight(1)
	q.SetWidth(qw)
	q.ShowLineNumbers = false
	q.SetValue(expr)
	q.Placeholder = "write your PromQL expression here..."
	q.KeyMap = textAreaKeyMap
	q.Focus()

	si := textinput.New()
	si.Prompt = ""
	si.SetValue(step)
	si.Width = stepModePanelOuter - 10
	si.Blur()

	sf := textinput.New()
	sf.Placeholder = "search datasets..."
	sf.Width = spotlightWidth - 6
	sf.Blur()

	bf := textinput.New()
	bf.Placeholder = "search..."
	bf.Width = 30
	bf.Blur()

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
		builderFilter:   bf,
		queryMode:       "code",
	}
}

// ─── bubbletea lifecycle ─────────────────────────────────────────────────────

func (m PromqlModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick}
	if strings.TrimSpace(m.query.Value()) != "" {
		cmds = append(cmds, NewPromqlFetchTask(m.profile, m.query.Value(), m.dataset, m.step,
			m.timeRange.StartValueUtc(), m.timeRange.EndValueUtc(), m.instant))
	}
	if m.dataset != "" {
		cmds = append(cmds, fetchCacheMetrics(m.profile, m.dataset))
	}
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
		m.stepInput.Width = stepModePanelOuter - 10
		m.spotlightFilter.Width = spotlightWidth - 6
		colW := builderColWidth(m.width)
		m.builderFilter.Width = colW*3 + 8
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
				case 0: // confirm metric → fetch labels → move to Labels column
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
					// cancel any previous in-flight labels request
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
						// no label filter — build expr and run
						expr := buildPromqlExpr(m.builderCurrentMetric(), "", "")
						newM, cmd := m.runQueryFromBuilder(expr)
						return newM, cmd
					}
					m.builderValues, m.builderValuesFiltered = nil, nil
					m.builderValuesIdx = 0
					m.builderCol = 2
					// cancel any previous in-flight values request
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

				case 2: // confirm value → build expression + run query + close
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
						// clear stale cache and warm fresh one in background
						m.cacheDataset = ""
						m.cacheMetrics = nil
						m.cacheLabels = nil
						m.cacheValues = nil
						m.overlay = overlayNone
						m.spotlightFilter.SetValue("")
						m.spotlightFilter.Blur()
						m.focusSelected()
						return m, fetchCacheMetrics(m.profile, newDS)
					}
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

// runQueryFromBuilder sets the expression, closes the builder overlay, and fires a query.
// The query panel stays in builder mode so the expression is shown as a breadcrumb.
func (m *PromqlModel) runQueryFromBuilder(expr string) (PromqlModel, tea.Cmd) {
	if expr != "" {
		m.query.SetValue(expr)
	}
	m.overlay = overlayNone
	m.builderFilter.SetValue("")
	m.builderFilter.Blur()
	// return focus to query panel, stay in builder mode
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

// openBuilderOverlay transitions to the builder overlay, seeding state from the cache.
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

	// ── header panels ────────────────────────────────────────────────────────
	dsName := m.dataset
	var dsNameRendered string
	if dsName == "" {
		dsNameRendered = lipgloss.NewStyle().Foreground(StandardSecondary).Render("select dataset")
	} else {
		if len(dsName) > datasetPanelOuter-4 {
			dsName = dsName[:datasetPanelOuter-7] + "..."
		}
		dsNameRendered = dsName
	}
	datasetPane := lipgloss.JoinVertical(lipgloss.Left,
		baseBoldUnderlinedStyle.Render(" dataset "),
		dsNameRendered,
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
	innerW := queryW - 2 // subtract border
	m.query.SetWidth(innerW)

	// ── query panel: toggle row + mode-aware content ──────────────────────────
	activeTabStyle := lipgloss.NewStyle().Foreground(FocusPrimary).Bold(true)
	inactiveTabStyle := lipgloss.NewStyle().Foreground(StandardSecondary)
	var codeLabel, builderLabel string
	if m.queryMode == "builder" {
		codeLabel = inactiveTabStyle.Render("Code")
		builderLabel = activeTabStyle.Render("Builder")
	} else {
		codeLabel = activeTabStyle.Render("Code")
		builderLabel = inactiveTabStyle.Render("Builder")
	}
	toggleRow := lipgloss.NewStyle().
		Width(innerW).
		Align(lipgloss.Right).
		Render(codeLabel + inactiveTabStyle.Render(" | ") + builderLabel)

	var queryPanelContent string
	if m.queryMode == "builder" {
		expr := m.query.Value()
		var exprDisplay string
		if expr == "" {
			exprDisplay = lipgloss.NewStyle().
				Foreground(StandardSecondary).Width(innerW).
				Render("press Enter to open builder...")
		} else {
			exprDisplay = lipgloss.NewStyle().
				Foreground(FocusPrimary).Bold(true).Width(innerW).
				Render(expr)
		}
		queryPanelContent = lipgloss.JoinVertical(lipgloss.Left, toggleRow, exprDisplay)
	} else {
		queryPanelContent = lipgloss.JoinVertical(lipgloss.Left, toggleRow, m.query.View())
	}

	header := lipgloss.JoinHorizontal(lipgloss.Top,
		dsRendered,
		queryOuter.Render(queryPanelContent),
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
			if m.queryMode == "builder" {
				helpKeys = [][]key.Binding{
					{
						key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open builder")),
						key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "switch to code mode")),
					},
					{promqlAdditionalKeyBinds[0]},
				}
			} else {
				helpKeys = append(TextAreaHelpKeys{}.FullHelp(),
					[]key.Binding{key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("ctrl+b", "switch to builder mode"))},
				)
			}
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
					key.NewBinding(key.WithKeys("type"), key.WithHelp("type", "edit step (e.g. 15s, 5m)")),
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
	case overlayBuilder:
		helpKeys = [][]key.Binding{
			{
				key.NewBinding(key.WithKeys("↑/↓"), key.WithHelp("↑/↓", "navigate")),
				key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select → next / run")),
			},
			{
				key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "run with current")),
				key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
			},
		}
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
		behind := lipgloss.JoinVertical(lipgloss.Left, header, resultPane)
		spotlight := m.renderSpotlight()
		mainView = lipgloss.Place(m.width, m.height-helpHeight-statusHeight,
			lipgloss.Center, lipgloss.Center, spotlight,
			lipgloss.WithWhitespaceChars(" "),
			lipgloss.WithWhitespaceForeground(StandardSecondary),
		)
		_ = behind
	case overlayBuilder:
		behind := lipgloss.JoinVertical(lipgloss.Left, header, resultPane)
		builder := m.renderBuilder()
		mainView = lipgloss.Place(m.width, m.height-helpHeight-statusHeight,
			lipgloss.Center, lipgloss.Center, builder,
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
	return fmt.Sprintf(`%s{%s="%s"}`, metric, label, value)
}

// renderBuilderCol renders a single column (Metrics / Labels / Values) for the builder overlay.
func renderBuilderCol(title string, items []string, selectedIdx int, loading, focused bool, colW int) string {
	innerW := colW - 2

	titleStyle := lipgloss.NewStyle().Bold(true).Width(innerW)
	if focused {
		titleStyle = titleStyle.Foreground(FocusPrimary)
	} else {
		titleStyle = titleStyle.Foreground(StandardSecondary)
	}

	var rows []string
	switch {
	case loading:
		rows = append(rows, lipgloss.NewStyle().
			Foreground(StandardSecondary).Width(innerW).
			Render("loading..."))
	case len(items) == 0:
		rows = append(rows, lipgloss.NewStyle().
			Foreground(StandardSecondary).Width(innerW).
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
		for i := start; i < end; i++ {
			item := items[i]
			maxLen := innerW - 4
			if maxLen > 3 && len(item) > maxLen {
				item = item[:maxLen-3] + "..."
			}
			if i == selectedIdx {
				rows = append(rows, lipgloss.NewStyle().
					Background(FocusPrimary).
					Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#000000"}).
					Width(innerW).Padding(0, 1).Bold(true).
					Render("▸ "+item))
			} else {
				rows = append(rows, lipgloss.NewStyle().
					Width(innerW).Padding(0, 1).
					Render("  "+item))
			}
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(title),
		strings.Join(rows, "\n"),
	)

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Width(colW)
	if focused {
		borderStyle = borderStyle.BorderForeground(FocusPrimary)
	} else {
		borderStyle = borderStyle.BorderForeground(StandardSecondary)
	}
	return borderStyle.Render(content)
}

// renderBuilder builds the 3-column query builder overlay.
func (m PromqlModel) renderBuilder() string {
	colW := builderColWidth(m.width)

	metricsItems := m.builderMetricsFiltered
	if m.dataset == "" {
		metricsItems = []string{"── select a dataset first ──"}
	}
	col0 := renderBuilderCol("Metrics", metricsItems, m.builderMetricsIdx,
		m.builderMetricsLoading, m.builderCol == 0, colW)
	col1 := renderBuilderCol("Labels", m.builderLabelsFiltered, m.builderLabelsIdx,
		m.builderLabelsLoading, m.builderCol == 1, colW)
	col2 := renderBuilderCol("Values", m.builderValuesFiltered, m.builderValuesIdx,
		m.builderValuesLoading, m.builderCol == 2, colW)

	columns := lipgloss.JoinHorizontal(lipgloss.Top, col0, col1, col2)
	colsW := lipgloss.Width(columns)

	expr := buildPromqlExpr(m.builderCurrentMetric(), m.builderCurrentLabel(), m.builderCurrentValue())
	dimStyle := lipgloss.NewStyle().Foreground(StandardSecondary)
	exprStyle := lipgloss.NewStyle().Foreground(FocusPrimary).Bold(true)
	exprLine := dimStyle.Render("Built: ") + exprStyle.Render(expr)

	searchStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(FocusSecondary).
		Width(colsW-4).
		Padding(0, 1)
	searchBar := searchStyle.Render(m.builderFilter.View())

	titleStyle := lipgloss.NewStyle().
		Foreground(FocusPrimary).Bold(true).
		Width(colsW).Align(lipgloss.Center)
	title := titleStyle.Render("PromQL Query Builder")

	body := lipgloss.JoinVertical(lipgloss.Left, title, columns, exprLine, searchBar)

	modal := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(FocusPrimary).
		Padding(0, 1).
		Render(body)

	return modal
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
			Name string `json:"name"`
		}
		if err := json.Unmarshal(body, &items); err != nil {
			return datasetListMsg{errMsg: err.Error()}
		}

		var all, matched []string
		for _, item := range items {
			all = append(all, item.Name)
			if strings.Contains(strings.ToLower(item.Name), "metrics") {
				matched = append(matched, item.Name)
			}
		}
		datasets := matched
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

// builderHTTPGetCtx performs an authenticated GET with context for cancellation.
// URLs are built manually so that match[] stays as literal brackets —
// url.Values.Encode percent-encodes them to match%5B%5D, which Parseable ignores.
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
