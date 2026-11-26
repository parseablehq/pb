package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/parseablehq/pb/pkg/config"
)

func TestK8sDeploymentDashboardUsesSeparatePodSeries(t *testing.T) {
	now := time.Now().Unix()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prometheus/api/v1/query_range" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("step"); got != "30" {
			t.Errorf("1h deployment metric step = %q, want 30s", got)
		}
		expression := r.URL.Query().Get("query")
		if !strings.Contains(expression, `sum by (k8s.pod.name)`) || !strings.Contains(expression, `k8s.pod.name=~"demo-.+"`) || r.URL.Query().Get("stream") != "metrics" {
			t.Errorf("unexpected deployment query: %s", r.URL.String())
		}
		if !strings.Contains(expression, "k8s.pod.cpu.usage") && !strings.Contains(expression, "k8s.pod.memory.usage") {
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
			return
		}
		multiplier := 1.0
		if strings.Contains(expression, "k8s.pod.memory.usage") {
			multiplier = 1e6
		}
		var series []string
		for i := 1; i <= 3; i++ {
			series = append(series, fmt.Sprintf(`{"metric":{"k8s.pod.name":"demo-%d"},"values":[[%d,"%.0f"],[%d,"%.0f"]]}`, i, now-60, float64(i)*multiplier, now, float64(i)*multiplier))
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[` + strings.Join(series, ",") + `]}}`))
	}))
	defer server.Close()
	request := k8sDeploymentRequest{
		Profile: config.Profile{URL: server.URL, Username: "admin", Password: "admin"},
		Mapping: config.K8sContext{MetricsDataset: "metrics", Profile: "pb-k8s-test"},
		Context: "production", Namespace: "shop", Deployment: "demo", From: "1h",
	}
	data := loadK8sDeployment(request)
	if data.err != nil || len(k8sDeploymentPods(data)) != 3 {
		t.Fatalf("unexpected deployment data: %+v", data)
	}
	pods := k8sDeploymentPods(data)
	if total := aggregateK8sMetric(data.cpu, pods, "total").latest; total != 6 {
		t.Fatalf("total CPU = %v, want 6", total)
	}
	if avg := aggregateK8sMetric(data.cpu, pods, "avg").latest; avg != 2 {
		t.Fatalf("average CPU = %v, want 2", avg)
	}
	model := newK8sDeploymentModel(request)
	model.loading, model.data, model.width, model.height = false, data, 180, 70
	view := model.View()
	for _, fragment := range []string{"pods 3", "CPU  /  cores  /  BY POD", "MEMORY  /  MiB  /  BY POD", "demo-1", "demo-2", "demo-3", "auto refresh 15s"} {
		if !strings.Contains(view, fragment) {
			t.Errorf("deployment view missing %q", fragment)
		}
	}
	if strings.Contains(view, "LOGS") || strings.Contains(view, "METRICS") || strings.Count(deploymentControls(model, model.width), "╭") < 2 {
		t.Fatal("workload controls should have bordered range and aggregate groups without corner labels")
	}
	metadata := deploymentMetadata(model, "deployment", len(pods), model.width)
	if len(metadata) != 3 || !strings.Contains(metadata[0], "namespace") || !strings.Contains(metadata[0], "profile") || !strings.Contains(metadata[1], "deployment") || !strings.Contains(metadata[1], "context") || !strings.Contains(metadata[2], "pods") {
		t.Fatalf("workload header should show namespace/deployment/pods on the left and profile/context on the right: %q", metadata)
	}
	viewLines := strings.Split(view, "\n")
	if len(viewLines) != model.height || !strings.Contains(viewLines[len(viewLines)-1], "q back") {
		t.Fatalf("dashboard footer should stay on the terminal's bottom row: height=%d, last=%q", len(viewLines), viewLines[len(viewLines)-1])
	}
	statusLine := ""
	for _, line := range viewLines {
		if strings.Contains(line, "auto refresh 15s") {
			statusLine = line
			break
		}
	}
	if !strings.Contains(statusLine, "UTC") || !strings.HasPrefix(statusLine, " ") || lipgloss.Width(statusLine) != model.width {
		t.Fatalf("updated time should be UTC and right aligned above controls: %q", statusLine)
	}
	cpuWidth := (model.width - 2) / 2
	memoryWidth := model.width - cpuWidth - 2
	cpuSummary := deploymentSummary("CPU", aggregateK8sMetric(data.cpu, pods, "total"), k8sPodMetric{}, k8sPodMetric{}, true, cpuWidth)
	memorySummary := deploymentSummary("Memory", aggregateK8sMetric(data.memory, pods, "total"), k8sPodMetric{}, k8sPodMetric{}, false, memoryWidth)
	if got := lipgloss.Width(lipgloss.JoinHorizontal(lipgloss.Top, cpuSummary, "  ", memorySummary)); got != model.width {
		t.Fatalf("summary panels use %d columns, want terminal width %d", got, model.width)
	}
	cpuPanel := deploymentChart("CPU", data.cpu, pods, model.aggregate, model.perPod, model.request.From, data.updatedAt, cpuWidth, true)
	memoryPanel := deploymentChart("Memory", data.memory, pods, model.aggregate, model.perPod, model.request.From, data.updatedAt, memoryWidth, false)
	if got := lipgloss.Width(lipgloss.JoinHorizontal(lipgloss.Top, cpuPanel, "  ", memoryPanel)); got != model.width {
		t.Fatalf("chart panels use %d columns, want terminal width %d", got, model.width)
	}
	chart := deploymentChart("CPU", data.cpu, pods, model.aggregate, model.perPod, model.request.From, data.updatedAt, 90, true)
	chartLines := strings.Split(chart, "\n")
	if len(chartLines) < 2 || !strings.Contains(chartLines[1], "CPU  /  cores  /  BY POD") || !strings.Contains(chartLines[1], "(1h) UTC") {
		t.Fatalf("chart time range should appear beside the title in UTC: %q", chartLines)
	}
	balancedLegend := false
	for _, line := range strings.Split(chart, "\n") {
		if strings.Contains(line, "demo-1") && strings.Contains(line, "demo-2") {
			balancedLegend = true
		}
	}
	if !balancedLegend {
		t.Fatal("pod legend should place two pods on the same row")
	}
	model.request.Kind = "statefulset"
	if statefulSetView := model.View(); !strings.Contains(statefulSetView, "STATEFULSET") || !strings.Contains(statefulSetView, "statefulset") {
		t.Fatal("StatefulSet dashboard should identify its workload type")
	}
	if command, _, err := K8sCmd.Find([]string{"sts"}); err != nil || command != k8sStatefulSetCmd {
		t.Fatalf("sts alias did not resolve to statefulset command: %v", err)
	}
	if !regexp.MustCompile("[\u2801-\u28ff]").MatchString(view) {
		t.Fatal("resource charts did not render braille lines")
	}
	if !strings.Contains(view, "CPU USAGE") || !strings.Contains(view, "MEMORY USAGE") {
		t.Fatal("resource summaries did not render")
	}
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > model.width {
			t.Fatalf("dashboard line is %d columns wide, want at most %d: %q", got, model.width, line)
		}
	}
	for _, width := range []int{80, 181} {
		resized := model
		resized.width = width
		for _, line := range strings.Split(resized.View(), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("dashboard at width %d has a %d-column line: %q", width, got, line)
			}
		}
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if changed := updated.(k8sDeploymentModel); changed.aggregate != "avg" || changed.perPod {
		t.Fatal("average control should show the aggregate chart")
	}
	updated, cmd := model.Update(k8sRefreshMsg(time.Now()))
	if !updated.(k8sDeploymentModel).loading || cmd == nil {
		t.Fatal("15-second tick did not schedule a refresh")
	}
	model.scroll = 10
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := updated.(k8sDeploymentModel).scroll; got != 9 {
		t.Fatalf("up key scrolled to %d, want 9", got)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := updated.(k8sDeploymentModel).scroll; got != 11 {
		t.Fatalf("down key scrolled to %d, want 11", got)
	}
	updated, _ = model.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if got := updated.(k8sDeploymentModel).scroll; got != 13 {
		t.Fatalf("mouse wheel scrolled to %d, want 13", got)
	}
}

func TestK8sDeploymentRequestsUseAvailableHTTPMetrics(t *testing.T) {
	now := time.Now().UTC().Truncate(30 * time.Second).Unix()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/prometheus/api/v1/series":
			_, _ = w.Write([]byte(`{"status":"success","data":[{"__name__":"http.server.request.duration.count","http.response.status_code":"200"},{"__name__":"http.server.request.duration.bucket"}]}`))
		case "/prometheus/api/v1/query_range":
			expression := r.URL.Query().Get("query")
			if !strings.Contains(expression, `k8s.pod.name=~"web-.+"`) {
				t.Errorf("HTTP metric query missing workload selector: %q", expression)
			}
			if strings.Contains(expression, "histogram_quantile") {
				value := "0.2"
				if strings.Contains(expression, "0.99") {
					value = "0.3"
				} else if strings.Contains(expression, "0.50") {
					value = "0.1"
				}
				_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[%d,"%s"]]}]}}`, now, value)
				return
			}
			if strings.Contains(expression, `=~"5.."`) {
				_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"k8s.pod.name":"web-0"},"values":[[%d,"1"]]}]}}`, now)
				return
			}
			_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"k8s.pod.name":"web-0"},"values":[[%d,"10"]]},{"metric":{"k8s.pod.name":"web-1"},"values":[[%d,"12"]]}]}}`, now, now)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	request := k8sDeploymentRequest{
		Profile:   config.Profile{URL: server.URL, Username: "admin", Password: "admin"},
		Mapping:   config.K8sContext{MetricsDataset: "metrics"},
		Namespace: "shop", Deployment: "web", From: "1h",
	}
	requests, errors, duration := fetchK8sDeploymentHTTP(request, "web-0")
	if len(requests) != 2 || len(errors) != 2 || errors["web-1"].latest != 0 || duration["p95"].latest != 200 {
		t.Fatalf("unexpected HTTP metrics: requests=%v errors=%v duration=%v", requests, errors, duration)
	}
	model := newK8sDeploymentModel(request)
	model.data = k8sDeploymentData{requests: requests, errors: errors, duration: duration, updatedAt: time.Unix(now, 0)}
	section := deploymentRequestSection(model, []string{"web-0", "web-1"}, 180)
	joined := strings.Join(section, "\n")
	for _, want := range []string{"REQUESTS", "Requests", "5xx errors", "Duration p95", "p99", "p50"} {
		if !strings.Contains(joined, want) {
			t.Errorf("request section missing %q", want)
		}
	}
	for _, line := range section {
		if got := lipgloss.Width(line); got > 180 {
			t.Fatalf("request section line is %d columns wide: %q", got, line)
		}
	}
}

func TestK8sChartUsesSampleTimesWithoutFillingOutages(t *testing.T) {
	start := time.Unix(0, 0)
	metric := k8sPodMetric{
		times:  []time.Time{start, start.Add(2 * time.Minute), start.Add(4 * time.Minute), start.Add(20 * time.Minute)},
		values: []float64{1, 3, 5, 9},
	}
	segments := k8sChartSegments(metric, start, start.Add(20*time.Minute), 1)
	if len(segments) != 2 || len(segments[0]) != 3 || len(segments[1]) != 1 {
		t.Fatalf("outage should split real sample points: %+v", segments)
	}
	if !segments[0][1].Time.Equal(start.Add(2*time.Minute)) || segments[0][1].Value != 3 || segments[1][0].Value != 9 {
		t.Fatalf("chart changed sample times or values: %+v", segments)
	}
}

func TestK8sChartKeepsFetchedWindowAcrossRedraws(t *testing.T) {
	end := time.Now().Truncate(15 * time.Second)
	metric := k8sPodMetric{
		times:     []time.Time{end.Add(-time.Minute), end},
		values:    []float64{0.002, 0.003},
		latest:    0.003,
		at:        end,
		windowEnd: end,
	}
	first := podChart("CPU", metric, "payment", "1h", end.Add(2*time.Second), 80, true)
	second := podChart("CPU", metric, "payment", "1h", end.Add(12*time.Second), 80, true)
	if first != second {
		t.Fatal("chart shifted while no new metric response arrived")
	}
}

func TestK8sResourceChartsRenderUsageOnly(t *testing.T) {
	at := time.Now().Truncate(15 * time.Second)
	times := []time.Time{at.Add(-45 * time.Minute), at.Add(-30 * time.Minute), at.Add(-15 * time.Minute), at}
	for _, test := range []struct {
		name   string
		values []float64
		cpu    bool
	}{{"CPU", []float64{0.002, 0.003, 0.0015, 0.0025}, true}, {"Memory", []float64{500 << 20, 600 << 20, 550 << 20, 700 << 20}, false}} {
		metric := k8sPodMetric{times: times, values: test.values, latest: test.values[3], at: at, windowEnd: at}
		view := podChart(test.name, metric, "payment", "1h", at, 80, test.cpu)
		if strings.Contains(view, "Request") || strings.Contains(view, "Limit") || strings.Contains(view, "No samples") {
			t.Fatalf("%s usage chart contains reference data or lacks samples: %q", test.name, view)
		}
		if got := len(regexp.MustCompile("[\u2801-\u28ff]").FindAllString(view, -1)); got < 10 {
			t.Fatalf("%s usage series barely rendered (%d cells): %q", test.name, got, view)
		}
	}
}

func TestK8sCPUSummaryShowsSmallCoreValues(t *testing.T) {
	at := time.Now()
	metric := k8sPodMetric{latest: 0.002462, at: at, values: []float64{0.001, 0.002462}, times: []time.Time{at.Add(-time.Minute), at}}
	request := k8sPodMetric{latest: 0.25, at: at}
	limit := k8sPodMetric{latest: 1, at: at}
	view := deploymentSummary("CPU", metric, request, limit, true, 80)
	for _, text := range []string{"CPU USAGE", "mCPU", "0.002462 cores", "Request: 250 mCPU", "Limit:   1000 mCPU", "0.25%", "of limit used", "▏"} {
		if !strings.Contains(view, text) {
			t.Fatalf("CPU summary missing %q: %q", text, view)
		}
	}
	if strings.Index(view, "Request:") > strings.Index(view, "0.25%") || strings.Index(view, "Limit:") > strings.Index(view, "0.25%") {
		t.Fatalf("request and limit should appear above usage percentage: %q", view)
	}
	if !strings.Contains(view, "▁") || !strings.Contains(view, "█") {
		t.Fatalf("small CPU usage is unreadable: %q", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > 80 {
			t.Fatalf("CPU card line is %d columns wide: %q", got, line)
		}
		if strings.Contains(line, "▏") && lipgloss.Width(strings.SplitN(line, "▏", 2)[0]) < 25 {
			t.Fatalf("usage bar should stay in the right column: %q", line)
		}
	}
	narrow := deploymentSummary("CPU", metric, request, limit, true, 30)
	for _, line := range strings.Split(narrow, "\n") {
		if got := lipgloss.Width(line); got > 30 {
			t.Fatalf("narrow CPU card line is %d columns wide: %q", got, line)
		}
	}
	noLimit := deploymentSummary("CPU", metric, request, k8sPodMetric{}, true, 80)
	if strings.Contains(noLimit, "%") || strings.Contains(noLimit, "of limit used") {
		t.Fatalf("CPU summary cannot show a percentage without a limit: %q", noLimit)
	}
	if !strings.Contains(noLimit, "Limit:   not set") {
		t.Fatalf("CPU summary should identify an unset limit: %q", noLimit)
	}
}

func TestK8sMemorySummaryShowsLimitUsage(t *testing.T) {
	metric := k8sPodMetric{latest: 512 << 20}
	request := k8sPodMetric{latest: 256 << 20}
	limit := k8sPodMetric{latest: 1024 << 20}
	view := deploymentSummary("Memory", metric, request, limit, false, 80)
	for _, text := range []string{"Request: 256.0 MiB", "Limit:   1024.0 MiB", "50.0%", "of limit used"} {
		if !strings.Contains(view, text) {
			t.Fatalf("memory summary missing %q: %q", text, view)
		}
	}
}
