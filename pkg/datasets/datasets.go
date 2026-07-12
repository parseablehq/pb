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
	client := internalHTTP.DefaultClient(&profile)
	req, err := client.NewRequest(http.MethodGet, "logstream", nil)
	if err != nil {
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

	names, err := decodeDatasetNames(body)
	if err != nil {
		return nil, err
	}

	items := make([]Dataset, 0, len(names))
	for _, name := range names {
		item := Dataset{Title: name}
		if datasetType, err := fetchDatasetType(&client, name); err == nil {
			item.DatasetType = datasetType
		}
		items = append(items, item)
	}
	sortDatasets(items)
	return items, nil
}

func decodeDatasetNames(body []byte) ([]string, error) {
	var names []string
	if err := json.Unmarshal(body, &names); err == nil {
		sort.Strings(names)
		return names, nil
	}

	var datasets []Dataset
	if err := json.Unmarshal(body, &datasets); err == nil {
		names := make([]string, 0, len(datasets))
		for _, item := range datasets {
			name := item.Title
			if name == "" {
				name = item.Name
			}
			if name != "" {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		return names, nil
	}

	var wrapped struct {
		Datasets []string `json:"datasets"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Datasets != nil {
		sort.Strings(wrapped.Datasets)
		return wrapped.Datasets, nil
	}

	return nil, fmt.Errorf("failed to decode dataset list response")
}

func fetchDatasetType(client *internalHTTP.HTTPClient, name string) (string, error) {
	req, err := client.NewRequest(http.MethodGet, fmt.Sprintf("logstream/%s/info", name), nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", httpStatusError{statusCode: resp.StatusCode, status: resp.Status, body: string(body)}
	}

	var response struct {
		StreamType    string `json:"streamType"`
		TelemetryType string `json:"telemetryType"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}
	if response.TelemetryType != "" {
		return response.TelemetryType, nil
	}
	return response.StreamType, nil
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
