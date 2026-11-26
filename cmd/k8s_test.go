package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/parseablehq/pb/pkg/config"
	"github.com/spf13/cobra"
)

func TestK8sSetupMapsMetrics(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/prism/v1/home":
			_, _ = w.Write([]byte(`{"datasets":[{"title":"pai-logs","datasetType":"logs"},{"title":"pai-metrics","datasetType":"metrics"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	conf := &config.Config{
		DefaultProfile: "prod",
		Profiles: map[string]config.Profile{
			"prod": {URL: server.URL, Username: "admin", Password: "admin"},
		},
	}
	if err := config.WriteConfigToFile(conf); err != nil {
		t.Fatal(err)
	}
	command := &cobra.Command{}
	command.Flags().String("context", "production", "")
	command.Flags().String("profile", "", "")
	command.Flags().String("metrics-dataset", "pai-metrics", "")
	command.Flags().String("cluster-name", "", "")
	command.SetOut(&bytes.Buffer{})
	if err := runK8sSetup(command, nil); err != nil {
		t.Fatal(err)
	}
	saved, err := config.ReadConfigFromFile()
	if err != nil {
		t.Fatal(err)
	}
	mapping := saved.K8sContexts["production"]
	if mapping.Profile != "prod" || mapping.MetricsDataset != "pai-metrics" || len(saved.K8sContexts) != 1 {
		t.Fatalf("unexpected context mapping: %+v", saved.K8sContexts)
	}
	if err := command.Flags().Set("metrics-dataset", "pai-logs"); err != nil {
		t.Fatal(err)
	}
	if err := runK8sSetup(command, nil); err == nil || errorDetails(err).Code != ErrorNotFound {
		t.Fatalf("expected wrong-type dataset error, got %v", err)
	}
	for flag, value := range map[string]string{"context": "staging", "metrics-dataset": "pai-metrics"} {
		if err := command.Flags().Set(flag, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := runK8sSetup(command, nil); err != nil {
		t.Fatalf("setup metrics dataset: %v", err)
	}
	staging, err := config.ReadConfigFromFile()
	if err != nil || staging.K8sContexts["staging"].MetricsDataset != "pai-metrics" {
		t.Fatalf("unexpected staging mapping: %+v (%v)", staging.K8sContexts, err)
	}
	podCommand := &cobra.Command{}
	podCommand.Flags().String("context", "production", "")
	podCommand.Flags().String("namespace", "shop", "")
	podCommand.Flags().String("from", "15m", "")
	podCommand.Flags().Bool("interactive", false, "")
	if err := runK8sPod(podCommand, []string{"payment-123"}); err == nil || errorDetails(err).Code != ErrorInvalidInput {
		t.Fatalf("pod dashboard should require -i and a terminal, got %v", err)
	}
}

func TestK8sPodMetricsQueryAndViewWithoutLogs(t *testing.T) {
	var receivedQuery, receivedStream string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prometheus/api/v1/query_range" {
			http.NotFound(w, r)
			return
		}
		receivedQuery = r.URL.Query().Get("query")
		receivedStream = r.URL.Query().Get("stream")
		if got := r.URL.Query().Get("step"); got != "30" {
			t.Errorf("1h metric step = %q, want 30s sample resolution", got)
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"k8s.pod.name":"pod"},"values":[[1795080000,"104857600"],[1795080060,"209715200"]]}]}}`))
	}))
	defer server.Close()
	request := k8sPodRequest{
		Profile: config.Profile{URL: server.URL, Username: "admin", Password: "admin"},
		Mapping: config.K8sContext{Profile: "prod", MetricsDataset: "metrics", ClusterName: "cluster-a"},
		Context: "production", Namespace: "shop", Pod: "payment-123", From: "1h",
	}
	metric, err := fetchK8sPodMetric(request, "k8s.pod.memory.usage")
	if err != nil {
		t.Fatal(err)
	}
	if receivedStream != "metrics" || !strings.Contains(receivedQuery, `k8s.pod.name="payment-123"`) || !strings.Contains(receivedQuery, `k8s.namespace.name="shop"`) || !strings.Contains(receivedQuery, `k8s.cluster.name="cluster-a"`) {
		t.Fatalf("unexpected metrics request: stream=%q query=%q", receivedStream, receivedQuery)
	}
	if metric.latest != 209715200 || len(metric.values) != 2 {
		t.Fatalf("unexpected metric: %+v", metric)
	}
	model := newK8sPodModel(request)
	model.loading, model.memory = false, metric
	model.width = 80
	view := model.View()
	for _, fragment := range []string{"MEMORY", "MiB", "POD MONITOR"} {
		if !strings.Contains(view, fragment) {
			t.Errorf("view missing %q: %q", fragment, view)
		}
	}
	model.scroll = 100
	if strings.Contains(model.View(), "Selected event:") || strings.Contains(model.View(), "Requests/s") {
		t.Errorf("empty sections should be hidden: %q", model.View())
	}
}

func TestK8sMetricWindowAndStep(t *testing.T) {
	for _, test := range []struct {
		span string
		step int
	}{{"15m", 30}, {"1h", 30}, {"6h", 60}, {"24h", 240}} {
		start, end, err := k8sMetricWindow(test.span)
		if err != nil {
			t.Fatal(err)
		}
		if got := k8sMetricStep(start, end); got != test.step {
			t.Errorf("%s step = %ds, want %ds", test.span, got, test.step)
		}
		if end.Unix()%30 != 0 || end.Sub(start) <= 0 {
			t.Errorf("%s window is not aligned: %s to %s", test.span, start, end)
		}
	}
}

func TestK8sPodDashboardRangesAndScrolling(t *testing.T) {
	model := newK8sPodModel(k8sPodRequest{From: "15m"})
	model.loading = false
	model.height = 18
	model.width = 80
	model.memory = k8sPodMetric{values: []float64{1, 2}, latest: 2, at: time.Now()}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	if cmd == nil || updated.(k8sPodModel).request.From != "24h" {
		t.Fatal("4 should load the 24h range")
	}
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(k8sPodModel)
	if model.scroll != 1 {
		t.Fatal("down should scroll the metrics dashboard")
	}
}

func TestK8sPodFooterStaysAtBottom(t *testing.T) {
	model := newK8sPodModel(k8sPodRequest{Namespace: "shop", Pod: "payment-123", From: "1h"})
	model.loading, model.width, model.height = false, 100, 20
	view := model.View()
	lines := strings.Split(view, "\n")
	if len(lines) != model.height || !strings.Contains(lines[len(lines)-1], "q back") {
		t.Fatalf("footer should be on the last terminal row: %q", view)
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got > model.width {
			t.Fatalf("pod dashboard line is %d columns wide: %q", got, line)
		}
	}
}

func TestK8sMetricErrorTextRemovesTerminalSequences(t *testing.T) {
	got := cleanK8sText("\x1b[2mhello\x1b[0m\n\x1b]11;?\x07world")
	if got != "hello world" {
		t.Fatalf("unexpected cleaned error: %q", got)
	}
}

func TestK8sPodOptionalMetricsOnlyRenderWhenAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/prometheus/api/v1/series":
			_, _ = w.Write([]byte(`{"status":"success","data":[{"__name__":"http.server.request.duration.count","k8s.pod.name":"pod","http.response.status_code":"200"},{"__name__":"http.server.request.duration.bucket","k8s.pod.name":"pod"},{"__name__":"k8s.container.cpu_limit","k8s.pod.name":"pod"}]}`))
		case "/prometheus/api/v1/query_range":
			expr := r.URL.Query().Get("query")
			if !strings.Contains(expr, `k8s.pod.name="pod"`) || r.URL.Query().Get("stream") != "metrics" {
				t.Errorf("unexpected optional query: %s", r.URL.String())
			}
			if strings.Contains(expr, `=~"5.."`) {
				_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
				return
			}
			value := "1.2"
			if strings.Contains(expr, "histogram_quantile") {
				value = "0.12"
			}
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1795080000,"` + value + `"],[1795080060,"` + value + `"]]}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	request := k8sPodRequest{Profile: config.Profile{URL: server.URL, Username: "admin", Password: "admin"}, Mapping: config.K8sContext{MetricsDataset: "metrics"}, Pod: "pod", Namespace: "shop", From: "1h"}
	extra, err := fetchK8sPodExtras(request)
	if err != nil {
		t.Fatal(err)
	}
	if extra.requests.latest != 1.2 || extra.errors.latest != 0 || extra.errors.at.IsZero() || extra.latency.latest != 120 || extra.latencyTitle != "Latency p95" || extra.cpuLimit.at.IsZero() {
		t.Fatalf("unexpected optional metrics: %+v", extra)
	}
	model := newK8sPodModel(request)
	model.loading, model.extra, model.height = false, extra, 80
	view := model.View()
	for _, fragment := range []string{"Requests", "REQUESTS/S", "5XX ERRORS/S", "LATENCY P95"} {
		if !strings.Contains(view, fragment) {
			t.Errorf("dashboard missing %q", fragment)
		}
	}
}
