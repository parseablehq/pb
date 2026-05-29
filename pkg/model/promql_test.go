package model

import (
	"reflect"
	"testing"
	"time"

	table "github.com/evertras/bubble-table/table"
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

func TestAggregateChartSeriesAveragesValuesByTimestamp(t *testing.T) {
	series := []promqlChartSeries{
		{
			times:  []string{"2026-01-02T04:00:00+05:30", "2026-01-02T02:00:00+05:30"},
			values: []float64{2, 1},
		},
		{
			times:  []string{"2026-01-02T02:00:00+05:30", "2026-01-02T04:00:00+05:30"},
			values: []float64{100, 200},
		},
	}

	got := aggregateChartSeries(series)
	if !reflect.DeepEqual(got.values, []float64{50.5, 101}) {
		t.Fatalf("values = %#v, want %#v", got.values, []float64{50.5, 101})
	}
	if len(got.times) != 2 {
		t.Fatalf("times length = %d, want 2", len(got.times))
	}
	first, ok := parsePromqlChartTime(got.times[0])
	if !ok {
		t.Fatalf("failed to parse first aggregated timestamp %q", got.times[0])
	}
	second, ok := parsePromqlChartTime(got.times[1])
	if !ok {
		t.Fatalf("failed to parse second aggregated timestamp %q", got.times[1])
	}
	if !first.Before(second) {
		t.Fatalf("aggregated timestamps not sorted: %q then %q", got.times[0], got.times[1])
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

func TestPromqlModelFormatTSUsesLocalTime(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.FixedZone("IST", 5*60*60+30*60)
	defer func() { time.Local = oldLocal }()

	utc := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	got := promqlModelFormatTS(float64(utc.Unix()))
	want := "2026-01-02T15:30:00+05:30"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
