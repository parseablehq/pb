package model

import "testing"

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
