package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
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

func TestK8sSetupPreservesRefreshedCloudTokens(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	oldAuthToken := config.CloudOrchestratorAuthToken
	config.CloudOrchestratorAuthToken = "orchestrator-auth-token"
	t.Cleanup(func() { config.CloudOrchestratorAuthToken = oldAuthToken })

	workspaceCalls, refreshCalls := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/prism/v1/home":
			workspaceCalls++
			cookie, err := r.Cookie("session")
			if err != nil || cookie.Value != "new-session" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"datasets":[{"title":"pai-metrics","datasetType":"metrics"}]}`))
		case "/api/v1/cli/oauth/token":
			refreshCalls++
			_, _ = w.Write([]byte(`{"access_token":"new-session","refresh_token":"new-refresh"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	profile := config.Profile{
		URL: server.URL, Cloud: true, SessionToken: "old-session", RefreshToken: "old-refresh",
		TenantID: "tenant", WorkspaceID: "workspace", OrchestratorURL: server.URL,
	}
	if err := config.WriteConfigToFile(&config.Config{
		DefaultProfile: "cloud", Profiles: map[string]config.Profile{"cloud": profile},
	}); err != nil {
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
	if got := saved.Profiles["cloud"]; got.SessionToken != "new-session" || got.RefreshToken != "new-refresh" {
		t.Fatalf("setup overwrote refreshed Cloud tokens: %+v", got)
	}
	if got := saved.K8sContexts["production"]; got.Profile != "cloud" || got.MetricsDataset != "pai-metrics" {
		t.Fatalf("unexpected context mapping: %+v", got)
	}
	if workspaceCalls != 2 || refreshCalls != 1 {
		t.Fatalf("workspace calls=%d, refresh calls=%d", workspaceCalls, refreshCalls)
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

func TestK8sPodDashboardIgnoresOlderLoad(t *testing.T) {
	model := newK8sPodModel(k8sPodRequest{From: "15m"})
	changed, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	model = changed.(k8sPodModel)
	if model.loadID != 2 || model.request.From != "1h" {
		t.Fatalf("range change did not start a new load: %+v", model)
	}
	changed, _ = model.Update(k8sPodLoadedMsg{loadID: 1, cpu: k8sPodMetric{latest: 1}})
	model = changed.(k8sPodModel)
	if !model.loading || model.cpu.latest != 0 {
		t.Fatal("older pod result was accepted while newer load was pending")
	}
	changed, _ = model.Update(k8sPodLoadedMsg{loadID: 2, cpu: k8sPodMetric{latest: 2}})
	model = changed.(k8sPodModel)
	if model.loading || model.cpu.latest != 2 {
		t.Fatal("current pod result was not accepted")
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

func TestK8sPodFailedErrorQueryDoesNotShowZero(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/prometheus/api/v1/series":
			_, _ = w.Write([]byte(`{"status":"success","data":[{"__name__":"http.server.request.duration.count","http.response.status_code":"200"}]}`))
		case "/prometheus/api/v1/query_range":
			if strings.Contains(r.URL.Query().Get("query"), `=~"5.."`) {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1795080000,"2"]]}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	request := k8sPodRequest{
		Profile: config.Profile{URL: server.URL, Username: "admin", Password: "admin"},
		Mapping: config.K8sContext{MetricsDataset: "metrics"}, Namespace: "shop", Pod: "pod", From: "1h",
	}
	extra, err := fetchK8sPodExtras(request)
	if err != nil {
		t.Fatal(err)
	}
	if extra.requests.latest != 2 || !extra.errors.at.IsZero() {
		t.Fatalf("failed 5xx query should be hidden, not shown as zero: %+v", extra)
	}
}

func TestZeroK8sMetricKeepsChartSamples(t *testing.T) {
	end := time.Now().UTC().Truncate(time.Minute)
	source := k8sPodMetric{
		values: []float64{3, 2, 1},
		times:  []time.Time{end.Add(-2 * time.Minute), end.Add(-time.Minute), end},
		latest: 1, at: end, windowEnd: end,
	}
	zero := zeroK8sMetric(source)
	if zero.latest != 0 || !zero.at.Equal(end) || !zero.windowEnd.Equal(end) || len(zero.times) != len(source.times) {
		t.Fatalf("zero metric lost its sample window: %+v", zero)
	}
	for i, value := range zero.values {
		if value != 0 || !zero.times[i].Equal(source.times[i]) {
			t.Fatalf("zero sample %d = %v at %s", i, value, zero.times[i])
		}
	}
	segments := k8sChartSegments(zero, end.Add(-5*time.Minute), end, 1)
	if len(segments) != 1 || len(segments[0]) != 3 {
		t.Fatalf("zero line has no chart points: %+v", segments)
	}
	card := k8sSignalCard("5xx errors/s", zero, 80, "5m", end, "err/s")
	if !regexp.MustCompile("[\u2801-\u28ff]").MatchString(card) {
		t.Fatal("zero error samples did not render a chart line")
	}
}

func TestK8sHTTPCardsFitTerminalWidth(t *testing.T) {
	for _, width := range []int{74, 80, 87, 88, 100} {
		cardWidth := width
		if width >= 88 {
			cardWidth = (width - 2) / 2
		}
		lines := appendK8sCards(nil, []string{strings.Repeat("a", cardWidth), strings.Repeat("b", cardWidth)}, width)
		wantLines := 2
		if width >= 88 {
			wantLines = 1
		}
		if len(lines) != wantLines {
			t.Errorf("width %d: got %d rows, want %d", width, len(lines), wantLines)
		}
		for _, line := range lines {
			if got := lipgloss.Width(line); got > width {
				t.Errorf("width %d: card row uses %d columns", width, got)
			}
		}
	}
}
