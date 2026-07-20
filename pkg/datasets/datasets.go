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
	Name          string `json:"name,omitempty"`
	DatasetType   string `json:"datasetType"`
	DatasetFormat string `json:"datasetFormat"`
	Ingestion     bool   `json:"ingestion"`
}

type homeResponse struct {
	Datasets []Dataset `json:"datasets"`
}

type httpStatusError struct {
	statusCode int
	status     string
	body       string
}

func (err httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", err.statusCode, strings.TrimSpace(err.body))
}

func FetchHomeDatasets(profile config.Profile) ([]Dataset, error) {
	return fetchPrismHomeDatasets(profile)
}

func fetchPrismHomeDatasets(profile config.Profile) ([]Dataset, error) {
	reqURL, err := url.JoinPath(profile.URL, "api/prism/v1/home")
	if err != nil {
		return nil, err
	}

	client := internalHTTP.DefaultClient(&profile)
	client.Client.Timeout = 15 * time.Second
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	if err := authenticate(req, profile); err != nil {
		return nil, err
	}

	resp, err := client.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, httpStatusError{statusCode: resp.StatusCode, status: resp.Status, body: string(body)}
	}

	var result homeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	sortDatasets(result.Datasets)
	return result.Datasets, nil
}

func FetchAPIDatasets(profile config.Profile) ([]Dataset, error) {
	return fetchPrismHomeDatasets(profile)
}

func sortDatasets(items []Dataset) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Title < items[j].Title
	})
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

func authenticate(req *http.Request, profile config.Profile) error {
	if err := internalHTTP.AddAuthHeaders(req, &profile); err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return nil
}
