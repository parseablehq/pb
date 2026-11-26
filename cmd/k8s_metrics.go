package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	internalHTTP "github.com/parseablehq/pb/pkg/http"
)

// k8sMetricCatalog maps available metric names to their label sets.
type k8sMetricCatalog map[string][]map[string]string

type k8sPodExtraMetrics struct {
	cpuRequest, cpuLimit        k8sPodMetric
	memoryRequest, memoryLimit  k8sPodMetric
	restarts                    k8sPodMetric
	networkReceive, networkSend k8sPodMetric
	requests, errors, latency   k8sPodMetric
	latencyTitle                string
}

func k8sMetricWindow(from string) (time.Time, time.Time, error) {
	end := time.Now().UTC().Truncate(30 * time.Second)
	if duration, err := time.ParseDuration(from); err == nil {
		return end.Add(-duration), end, nil
	}
	start, err := parseTimeStr(from)
	return start.UTC(), end, err
}

func k8sMetricStep(start, end time.Time) int {
	// Match the usual pod sample cadence and keep longer ranges below 360 points.
	seconds := int(math.Ceil(end.Sub(start).Seconds()/360/15)) * 15
	return max(30, seconds)
}

// fetchK8sPodMetricCatalog checks which signals exist before querying them.
func fetchK8sPodMetricCatalog(request k8sPodRequest) (k8sMetricCatalog, error) {
	start, err := parseTimeStr(request.From)
	if err != nil {
		return nil, err
	}
	params := url.Values{}
	params.Set("stream", request.Mapping.MetricsDataset)
	params.Set("start", start.UTC().Format(time.RFC3339))
	params.Set("end", time.Now().UTC().Format(time.RFC3339))
	params.Add("match[]", k8sPodSelector(request, "", ""))
	endpoint, err := url.JoinPath(request.Profile.URL, "prometheus/api/v1/series")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if err := internalHTTP.AddAuthHeaders(req, &request.Profile); err != nil {
		return nil, err
	}
	client := internalHTTP.DefaultClient(&request.Profile)
	resp, err := client.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		preview, _ := readLimitedErrorPreview(resp.Body)
		return nil, fmt.Errorf("metric catalog failed: %s: %s", resp.Status, preview)
	}
	var result struct {
		Status string              `json:"status"`
		Data   []map[string]string `json:"data"`
		Error  string              `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode metric catalog: %w", err)
	}
	if result.Status == "error" {
		return nil, fmt.Errorf("metric catalog failed: %s", result.Error)
	}
	catalog := make(k8sMetricCatalog)
	for _, series := range result.Data {
		if name := series["__name__"]; name != "" {
			catalog[name] = append(catalog[name], series)
		}
	}
	return catalog, nil
}

func (catalog k8sMetricCatalog) first(names ...string) string {
	for _, name := range names {
		if len(catalog[name]) > 0 {
			return name
		}
	}
	return ""
}

// fetchK8sPodExtras loads optional resource, network, and HTTP metrics.
func fetchK8sPodExtras(request k8sPodRequest) (k8sPodExtraMetrics, error) {
	var extra k8sPodExtraMetrics
	catalog, err := fetchK8sPodMetricCatalog(request)
	if err != nil {
		return extra, err
	}
	var group sync.WaitGroup
	for _, task := range []struct {
		name string
		into *k8sPodMetric
	}{
		{catalog.first("k8s.container.cpu_request"), &extra.cpuRequest},
		{catalog.first("k8s.container.cpu_limit"), &extra.cpuLimit},
		{catalog.first("k8s.container.memory_request"), &extra.memoryRequest},
		{catalog.first("k8s.container.memory_limit"), &extra.memoryLimit},
		{catalog.first("k8s.container.restarts"), &extra.restarts},
	} {
		if task.name == "" {
			continue
		}
		group.Add(1)
		go func() {
			defer group.Done()
			*task.into, _ = fetchK8sPodMetric(request, task.name)
		}()
	}
	if name := catalog.first("k8s.pod.network.io"); name != "" {
		group.Add(2)
		go func() {
			defer group.Done()
			extra.networkReceive = fetchK8sRate(request, name, `direction="receive"`)
		}()
		go func() {
			defer group.Done()
			extra.networkSend = fetchK8sRate(request, name, `direction="transmit"`)
		}()
	}
	group.Wait()

	countName := catalog.first(
		"http.server.request.duration.count", "http.server.request.duration_count",
		"http_server_request_duration_seconds_count", "http.server.duration.count",
		"http_server_requests_total", "http_requests_total",
	)
	if countName == "" {
		return extra, nil
	}
	extra.requests = fetchK8sRate(request, countName, "")
	if extra.requests.at.IsZero() {
		return extra, nil
	}
	for _, label := range []string{"http.response.status_code", "http.status_code", "status_code", "http_status_code"} {
		for _, series := range catalog[countName] {
			if _, ok := series[label]; ok {
				extra.errors = fetchK8sRate(request, countName, label+`=~"5.."`)
				if extra.errors.at.IsZero() {
					extra.errors = zeroK8sMetric(extra.requests)
				}
				break
			}
		}
		if !extra.errors.at.IsZero() {
			break
		}
	}
	bucketName := catalog.first(
		"http.server.request.duration.bucket", "http.server.request.duration_bucket",
		"http_server_request_duration_seconds_bucket", "http.server.duration.bucket",
	)
	if bucketName != "" {
		expression := "histogram_quantile(0.95, sum by (le) (rate(" + k8sPodSelector(request, bucketName, "") + "[5m])))"
		extra.latency, _ = fetchK8sPodMetricExpression(request, expression)
		if !extra.latency.at.IsZero() {
			extra.latencyTitle = "Latency p95"
			if !strings.Contains(bucketName, "http.server.duration") {
				extra.latency = scaleK8sMetric(extra.latency, 1000)
			}
		}
	}
	if extra.latency.at.IsZero() {
		sumName := catalog.first(
			"http.server.request.duration.sum", "http.server.request.duration_sum",
			"http_server_request_duration_seconds_sum", "http.server.duration.sum",
		)
		if sumName != "" {
			expression := "sum(rate(" + k8sPodSelector(request, sumName, "") + "[5m])) / sum(rate(" + k8sPodSelector(request, countName, "") + "[5m]))"
			extra.latency, _ = fetchK8sPodMetricExpression(request, expression)
			if !extra.latency.at.IsZero() {
				extra.latencyTitle = "Latency avg"
				if !strings.Contains(sumName, "http.server.duration") {
					extra.latency = scaleK8sMetric(extra.latency, 1000)
				}
			}
		}
	}
	return extra, nil
}

func fetchK8sRate(request k8sPodRequest, name, matcher string) k8sPodMetric {
	expression := "sum(rate(" + k8sPodSelector(request, name, matcher) + "[5m]))"
	metric, _ := fetchK8sPodMetricExpression(request, expression)
	return metric
}

func zeroK8sMetric(source k8sPodMetric) k8sPodMetric {
	return k8sPodMetric{values: make([]float64, len(source.values)), latest: 0, at: source.at}
}

func scaleK8sMetric(source k8sPodMetric, multiplier float64) k8sPodMetric {
	source.latest *= multiplier
	for i := range source.values {
		source.values[i] *= multiplier
	}
	return source
}
