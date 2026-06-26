package datasets

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/parseablehq/pb/pkg/config"
	internalHTTP "github.com/parseablehq/pb/pkg/http"
)

const (
	// TypeLogs identifies log/event datasets.
	TypeLogs = "logs"
	// TypeMetrics identifies metrics datasets.
	TypeMetrics = "metrics"
	// TypeTraces identifies traces datasets.
	TypeTraces = "traces"
)

type Dataset struct {
	Title         string `json:"title"`
	DatasetType   string `json:"datasetType"`
	DatasetFormat string `json:"datasetFormat"`
	Ingestion     bool   `json:"ingestion"`
}

type homeResponse struct {
	Datasets []Dataset `json:"datasets"`
}

func FetchHomeDatasets(profile config.Profile) ([]Dataset, error) {
	reqURL, err := url.JoinPath(profile.URL, "api/prism/v1/home")
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	authenticate(req, profile)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result homeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	sort.Slice(result.Datasets, func(i, j int) bool {
		return result.Datasets[i].Title < result.Datasets[j].Title
	})
	return result.Datasets, nil
}

func NamesByType(items []Dataset, datasetType string) []string {
	var names []string
	for _, item := range items {
		if item.DatasetType == datasetType {
			names = append(names, item.Title)
		}
	}
	sort.Strings(names)
	return names
}

func authenticate(req *http.Request, profile config.Profile) {
	internalHTTP.AddAuthHeaders(req, &profile)
	req.Header.Set("Content-Type", "application/json")
}
