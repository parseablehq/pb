package model

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	table "github.com/evertras/bubble-table/table"
	"github.com/parseablehq/pb/pkg/config"
)

func TestEscapePromQLValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain value unchanged",
			input: "production",
			want:  "production",
		},
		{
			name:  "double quote escaped",
			input: `foo"bar`,
			want:  `foo\"bar`,
		},
		{
			name:  "backslash escaped",
			input: `foo\bar`,
			want:  `foo\\bar`,
		},
		{
			name:  "newline escaped",
			input: "foo\nbar",
			want:  `foo\nbar`,
		},
		{
			name:  "tab escaped",
			input: "foo\tbar",
			want:  `foo\tbar`,
		},
		{
			name:  "backslash before quote escaped in order",
			input: `fo\"bar`,
			want:  `fo\\\"bar`,
		},
		{
			name:  "empty string unchanged",
			input: "",
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapePromQLValue(tt.input)
			if got != tt.want {
				t.Errorf("input %q\ngot  %q\nwant %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildPromqlExpr(t *testing.T) {
	tests := []struct {
		name   string
		metric string
		label  string
		value  string
		want   string
	}{
		{
			name:   "empty metric returns empty",
			metric: "",
			label:  "env",
			value:  "prod",
			want:   "",
		},
		{
			name:   "empty label returns bare metric",
			metric: "http_requests_total",
			label:  "",
			value:  "prod",
			want:   "http_requests_total",
		},
		{
			name:   "(any) label returns bare metric",
			metric: "http_requests_total",
			label:  "(any)",
			value:  "prod",
			want:   "http_requests_total",
		},
		{
			name:   "empty value returns not-empty matcher",
			metric: "http_requests_total",
			label:  "env",
			value:  "",
			want:   `http_requests_total{env!=""}`,
		},
		{
			name:   "(any) value returns not-empty matcher",
			metric: "http_requests_total",
			label:  "env",
			value:  "(any)",
			want:   `http_requests_total{env!=""}`,
		},
		{
			name:   "plain value builds eq matcher",
			metric: "http_requests_total",
			label:  "env",
			value:  "production",
			want:   `http_requests_total{env="production"}`,
		},
		{
			name:   "value with double quote is escaped",
			metric: "http_requests_total",
			label:  "env",
			value:  `prod"uction`,
			want:   `http_requests_total{env="prod\"uction"}`,
		},
		{
			name:   "value with backslash is escaped",
			metric: "http_requests_total",
			label:  "env",
			value:  `prod\uction`,
			want:   `http_requests_total{env="prod\\uction"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPromqlExpr(tt.metric, tt.label, tt.value)
			if got != tt.want {
				t.Errorf("metric=%q label=%q value=%q\ngot  %q\nwant %q",
					tt.metric, tt.label, tt.value, got, tt.want)
			}
		})
	}
}

func TestExtractChartDataGroupsSeriesByMetricLabelSet(t *testing.T) {
	m := PromqlModel{
		dataRows: []table.Row{
			table.NewRow(table.RowData{
				promqlTimestampFullKey: "2026-01-02T02:00:00+05:30",
				promqlMetricKey:        `cpu_usage{container_id="aaa", host.arch="amd64"}`,
				promqlValueKey:         "1",
			}),
			table.NewRow(table.RowData{
				promqlTimestampFullKey: "2026-01-02T04:00:00+05:30",
				promqlMetricKey:        `cpu_usage{container_id="aaa", host.arch="amd64"}`,
				promqlValueKey:         "2",
			}),
			table.NewRow(table.RowData{
				promqlTimestampFullKey: "2026-01-02T02:00:00+05:30",
				promqlMetricKey:        `cpu_usage{container_id="bbb", host.arch="arm64"}`,
				promqlValueKey:         "100",
			}),
			table.NewRow(table.RowData{
				promqlTimestampFullKey: "2026-01-02T04:00:00+05:30",
				promqlMetricKey:        `cpu_usage{container_id="bbb", host.arch="arm64"}`,
				promqlValueKey:         "200",
			}),
		},
	}

	series := m.extractChartData()
	if len(series) != 2 {
		t.Fatalf("series count = %d, want 2", len(series))
	}
	if !reflect.DeepEqual(series[0].values, []float64{1, 2}) {
		t.Fatalf("series[0] values = %#v, want %#v", series[0].values, []float64{1, 2})
	}
	if !reflect.DeepEqual(series[1].values, []float64{100, 200}) {
		t.Fatalf("series[1] values = %#v, want %#v", series[1].values, []float64{100, 200})
	}
	if series[0].label == series[1].label {
		t.Fatalf("series labels should be distinct, got %q", series[0].label)
	}
}

func TestChartSeriesPointsPreservesRawSeries(t *testing.T) {
	m := PromqlModel{
		dataRows: []table.Row{
			table.NewRow(table.RowData{
				promqlTimestampFullKey: "2026-01-02T04:00:00+05:30",
				promqlMetricKey:        `cpu_usage{host.arch="amd64"}`,
				promqlValueKey:         "2",
			}),
			table.NewRow(table.RowData{
				promqlTimestampFullKey: "2026-01-02T02:00:00+05:30",
				promqlMetricKey:        `cpu_usage{host.arch="amd64"}`,
				promqlValueKey:         "1",
			}),
			table.NewRow(table.RowData{
				promqlTimestampFullKey: "2026-01-02T02:00:00+05:30",
				promqlMetricKey:        `cpu_usage{host.arch="arm64"}`,
				promqlValueKey:         "100",
			}),
			table.NewRow(table.RowData{
				promqlTimestampFullKey: "2026-01-02T04:00:00+05:30",
				promqlMetricKey:        `cpu_usage{host.arch="arm64"}`,
				promqlValueKey:         "200",
			}),
		},
	}

	got := m.chartSeriesPoints()
	if len(got) != 2 {
		t.Fatalf("series count = %d, want 2", len(got))
	}
	if got[0].points[0].Value != 100 || got[0].points[1].Value != 200 {
		t.Fatalf("first series values = %#v, want highest latest-value series 100,200", got[0].points)
	}
	if got[1].points[0].Value != 1 || got[1].points[1].Value != 2 {
		t.Fatalf("second series values = %#v, want lower latest-value series 1,2", got[1].points)
	}
	if !got[0].points[0].Time.Before(got[0].points[1].Time) {
		t.Fatalf("first series timestamps not sorted: %#v", got[0].points)
	}
	if got[0].color == got[1].color {
		t.Fatalf("series colors should be unique, got %q", got[0].color)
	}
}

func TestChartSeriesColorDoesNotRepeatForManySeries(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		color := string(chartSeriesColor(i))
		if seen[color] {
			t.Fatalf("chartSeriesColor(%d) repeated color %s", i, color)
		}
		seen[color] = true
	}
}

func TestFormatCompactChartValue(t *testing.T) {
	tests := []struct {
		value float64
		want  string
	}{
		{value: 8, want: "8"},
		{value: 8.5, want: "8.5"},
		{value: 580000, want: "580k"},
		{value: 580500, want: "580.5k"},
		{value: 1722407, want: "1.7M"},
		{value: -1250, want: "-1.2k"},
	}

	for _, tt := range tests {
		if got := formatCompactChartValue(tt.value); got != tt.want {
			t.Fatalf("formatCompactChartValue(%v) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestFormatChartTime12h(t *testing.T) {
	tests := []struct {
		value time.Time
		want  string
	}{
		{value: time.Date(2026, 1, 2, 14, 10, 0, 0, time.UTC), want: "2:10pm"},
		{value: time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC), want: "3:00pm"},
		{value: time.Date(2026, 1, 2, 9, 30, 0, 0, time.UTC), want: "9:30am"},
	}

	for _, tt := range tests {
		if got := formatChartTime12h(tt.value); got != tt.want {
			t.Fatalf("formatChartTime12h(%v) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestChartTimeRangeTitleUsesDisplayMode(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.FixedZone("IST", 5*60*60+30*60)
	defer func() { time.Local = oldLocal }()

	firstUTC := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	lastUTC := time.Date(2026, 1, 2, 9, 10, 0, 0, time.UTC)
	rows := []table.Row{
		table.NewRow(table.RowData{
			promqlTimestampFullKey: firstUTC.Format(time.RFC3339),
			promqlMetricKey:        `cpu_usage{host.arch="amd64"}`,
			promqlValueKey:         "1",
		}),
		table.NewRow(table.RowData{
			promqlTimestampFullKey: lastUTC.Format(time.RFC3339),
			promqlMetricKey:        `cpu_usage{host.arch="amd64"}`,
			promqlValueKey:         "2",
		}),
	}

	localModel := PromqlModel{dataRows: rows}
	localModel.timeRange.SetDisplayMode(TimeDisplayLocal)
	if got, want := localModel.chartTimeRangeTitle(), "📊 2:30pm → 2:40pm"; got != want {
		t.Fatalf("local title = %q, want %q", got, want)
	}

	utcModel := PromqlModel{dataRows: rows}
	utcModel.timeRange.SetDisplayMode(TimeDisplayUTC)
	if got, want := utcModel.chartTimeRangeTitle(), "📊 9:00am → 9:10am"; got != want {
		t.Fatalf("utc title = %q, want %q", got, want)
	}
}

func TestPromqlModeToggleUsesCtrlSpace(t *testing.T) {
	if !isPromqlModeToggleKey(tea.KeyMsg{Type: tea.KeyCtrlAt}) {
		t.Fatal("ctrl+space should toggle PromQL mode")
	}
	if isPromqlModeToggleKey(tea.KeyMsg{Type: tea.KeySpace}) {
		t.Fatal("plain space should not toggle PromQL mode")
	}
}

func TestPromqlInstantToBothPreservesEndTime(t *testing.T) {
	start := time.Date(2026, 1, 2, 8, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	m := NewPromqlModel(config.Profile{}, "", start, end, "1m", "metrics", true)
	m.mode = promqlModeInstant
	m.focused = 3 // step panel

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlAt})
	got := next.(PromqlModel)
	if got.mode != promqlModeBoth {
		t.Fatalf("mode = %q, want %q", got.mode, promqlModeBoth)
	}
	if !got.timeRange.end.Time().Equal(end) {
		t.Fatalf("end time changed to %s, want %s", got.timeRange.end.Time(), end)
	}
	if wantStart := end.Add(OneHour); !got.timeRange.start.Time().Equal(wantStart) {
		t.Fatalf("start time = %s, want %s", got.timeRange.start.Time(), wantStart)
	}
}

func TestNormalizePromqlRangeWindowFixesInvalidRange(t *testing.T) {
	end := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	start := end.Add(time.Hour)

	gotStart, gotEnd := normalizePromqlRangeWindow(start.Format(time.RFC3339), end.Format(time.RFC3339))
	if gotEnd != end.Format(time.RFC3339) {
		t.Fatalf("end = %q, want %q", gotEnd, end.Format(time.RFC3339))
	}
	if gotStart != end.Add(OneHour).Format(time.RFC3339) {
		t.Fatalf("start = %q, want %q", gotStart, end.Add(OneHour).Format(time.RFC3339))
	}
}

func TestPromqlBothModeRendersChartRowsWhenInstantRowsEmpty(t *testing.T) {
	start := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)
	m := NewPromqlModel(config.Profile{}, "cpu_usage", start, end, "1m", "metrics", false)
	m.width = 100
	m.height = 40
	m.hasQueried = true
	m.mode = promqlModeBoth
	m.chartMode = true
	m.dataRows = nil
	m.chartRows = []table.Row{
		table.NewRow(table.RowData{
			promqlTimestampFullKey: start.Format(time.RFC3339),
			promqlMetricKey:        `cpu_usage{host="a"}`,
			promqlValueKey:         "1",
		}),
		table.NewRow(table.RowData{
			promqlTimestampFullKey: end.Format(time.RFC3339),
			promqlMetricKey:        `cpu_usage{host="a"}`,
			promqlValueKey:         "2",
		}),
	}

	view := m.View()
	if strings.Contains(view, "no results for this query") {
		t.Fatalf("both mode should render chart rows when instant rows are empty:\n%s", view)
	}
	if !strings.Contains(view, "RESULTS | Chart View") {
		t.Fatalf("expected chart view, got:\n%s", view)
	}
}

func TestPromqlBothModeKeepsInstantRowsForTable(t *testing.T) {
	m := PromqlModel{mode: promqlModeBoth}
	instantRows := []table.Row{
		table.NewRow(table.RowData{
			promqlTimestampFullKey: "2026-01-02T09:00:00Z",
			promqlMetricKey:        `cpu_usage{host="instant"}`,
			promqlValueKey:         "9",
		}),
	}
	rangeRows := []table.Row{
		table.NewRow(table.RowData{
			promqlTimestampFullKey: "2026-01-02T08:55:00Z",
			promqlMetricKey:        `cpu_usage{host="range"}`,
			promqlValueKey:         "1",
		}),
		table.NewRow(table.RowData{
			promqlTimestampFullKey: "2026-01-02T09:00:00Z",
			promqlMetricKey:        `cpu_usage{host="range"}`,
			promqlValueKey:         "2",
		}),
	}
	msg := PromqlFetchData{
		status:      fetchOk,
		resultType:  "vector",
		chartResult: "matrix",
		rows:        instantRows,
		chartRows:   rangeRows,
	}

	next, _ := m.Update(msg)
	got := next.(PromqlModel)
	if !got.chartMode {
		t.Fatal("both mode should default to chart view")
	}
	if !reflect.DeepEqual(got.dataRows, instantRows) {
		t.Fatalf("table rows = %#v, want instant rows %#v", got.dataRows, instantRows)
	}
	if !reflect.DeepEqual(got.chartRows, rangeRows) {
		t.Fatalf("chart rows = %#v, want range rows %#v", got.chartRows, rangeRows)
	}
}

func TestPromqlModelFormatTSUsesLocalTime(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.FixedZone("IST", 5*60*60+30*60)
	defer func() { time.Local = oldLocal }()

	utc := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	got := promqlModelFormatTS(float64(utc.Unix()), TimeDisplayLocal)
	want := "2026-01-02T15:30:00+05:30"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPromqlModelFormatTSUsesUTC(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.FixedZone("IST", 5*60*60+30*60)
	defer func() { time.Local = oldLocal }()

	utc := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	got := promqlModelFormatTS(float64(utc.Unix()), TimeDisplayUTC)
	want := "2026-01-02T10:00:00Z"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolvePromqlStep(t *testing.T) {
	start := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		step     string
		duration time.Duration
		want     string
	}{
		{name: "manual unchanged", step: "5m", duration: time.Hour, want: "5m"},
		{name: "ten minute range", step: "auto", duration: 10 * time.Minute, want: "15s"},
		{name: "one hour range", step: "auto", duration: time.Hour, want: "1m"},
		{name: "one day range", step: "auto", duration: 24 * time.Hour, want: "15m"},
		{name: "empty uses auto", step: "", duration: 6 * time.Hour, want: "5m"},
		{name: "invalid uses auto", step: "now", duration: time.Hour, want: "1m"},
		{name: "compound manual unchanged", step: "1h30m", duration: 24 * time.Hour, want: "1h30m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolvePromqlStep(tt.step, start, start.Add(tt.duration))
			if got != tt.want {
				t.Fatalf("ResolvePromqlStep(%q, %s) = %q, want %q", tt.step, tt.duration, got, tt.want)
			}
		})
	}
}
