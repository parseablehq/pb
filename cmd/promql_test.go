package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/parseablehq/pb/pkg/config"
	"github.com/spf13/cobra"
)

func TestPromqlInteractiveFromDefaultUsesTenMinutes(t *testing.T) {
	got := promqlInteractiveFromDefault("5m", true, false)
	if got != "10m" {
		t.Fatalf("got %q, want 10m", got)
	}
}

func TestPromqlInteractiveFromDefaultKeepsExplicitFrom(t *testing.T) {
	got := promqlInteractiveFromDefault("1h", true, true)
	if got != "1h" {
		t.Fatalf("got %q, want 1h", got)
	}
}

func TestPromqlInteractiveFromDefaultKeepsNonInteractiveDefault(t *testing.T) {
	got := promqlInteractiveFromDefault("5m", false, false)
	if got != "5m" {
		t.Fatalf("got %q, want 5m", got)
	}
}

func TestPromqlPositionalArgumentsOverrideFlags(t *testing.T) {
	requests := make(chan *url.URL, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURL := *r.URL
		requests <- &requestURL
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"success","data":[]}`)
	}))
	defer server.Close()

	originalProfile := DefaultProfile
	DefaultProfile = config.Profile{URL: server.URL, APIKey: "test-key"}
	t.Cleanup(func() { DefaultProfile = originalProfile })

	tests := []struct {
		name      string
		cmd       *cobra.Command
		args      []string
		flagSetup map[string]string
		wantPath  string
		wantQuery map[string]string
	}{
		{
			name:      "labels stream",
			cmd:       promqlLabelsCmd,
			args:      []string{"positional-labels"},
			wantPath:  "/prometheus/api/v1/labels",
			wantQuery: map[string]string{"stream": "positional-labels"},
		},
		{
			name:      "label values label and stream",
			cmd:       promqlLabelValuesCmd,
			args:      []string{"job", "positional-label-values"},
			wantPath:  "/prometheus/api/v1/label/job/values",
			wantQuery: map[string]string{"stream": "positional-label-values"},
		},
		{
			name: "series stream",
			cmd:  promqlSeriesCmd,
			args: []string{"positional-series"},
			flagSetup: map[string]string{
				"match": "metric_name",
			},
			wantPath: "/prometheus/api/v1/series",
			wantQuery: map[string]string{
				"stream":  "positional-series",
				"match[]": "metric_name",
			},
		},
		{
			name: "cardinality label values stream and label",
			cmd:  promqlCardinalityLabelValuesCmd,
			args: []string{"positional-cardinality", "service.name"},
			flagSetup: map[string]string{
				"label": "flag-label",
			},
			wantPath:  "/prometheus/api/v1/cardinality/label_values",
			wantQuery: map[string]string{"stream": "positional-cardinality", "label_name": "service.name"},
		},
		{
			name: "active series stream and selector",
			cmd:  promqlCardinalityActiveSeriesCmd,
			args: []string{"positional-active", `{job="api"}`},
			flagSetup: map[string]string{
				"selector": `{job="flag-value"}`,
			},
			wantPath:  "/prometheus/api/v1/cardinality/active_series",
			wantQuery: map[string]string{"stream": "positional-active", "selector": `{job="api"}`},
		},
		{
			name:      "tsdb stream",
			cmd:       promqlTSDBCmd,
			args:      []string{"positional-tsdb"},
			wantPath:  "/prometheus/api/v1/status/tsdb",
			wantQuery: map[string]string{"stream": "positional-tsdb"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.cmd.Flags().Set("dataset", "flag-dataset"); err != nil {
				t.Fatal(err)
			}
			if err := test.cmd.Flags().Set("output", "json"); err != nil {
				t.Fatal(err)
			}
			for name, value := range test.flagSetup {
				if err := test.cmd.Flags().Set(name, value); err != nil {
					t.Fatal(err)
				}
			}
			if err := test.cmd.Args(test.cmd, test.args); err != nil {
				t.Fatal(err)
			}
			if err := test.cmd.RunE(test.cmd, test.args); err != nil {
				t.Fatal(err)
			}

			requestURL := <-requests
			if requestURL.Path != test.wantPath {
				t.Fatalf("path=%q want=%q", requestURL.Path, test.wantPath)
			}
			for key, want := range test.wantQuery {
				if got := requestURL.Query().Get(key); got != want {
					t.Fatalf("query %s=%q want=%q", key, got, want)
				}
			}
		})
	}
}
