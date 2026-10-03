package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/parseablehq/pb/pkg/config"
	internalHTTP "github.com/parseablehq/pb/pkg/http"
)

func TestFetchInfoUsesTelemetryType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v1/logstream/fly_logs/info"; got != want {
			t.Fatalf("path mismatch: want %s got %s", want, got)
		}
		fmt.Fprint(w, `{"streamType":"UserDefined","telemetryType":"logs"}`)
	}))
	defer server.Close()

	client := internalHTTP.DefaultClient(&config.Profile{URL: server.URL, APIKey: "test-api-key"})
	got, err := fetchInfo(&client, "fly_logs")
	if err != nil {
		t.Fatalf("fetchInfo returned error: %v", err)
	}
	if got != "logs" {
		t.Fatalf("dataset type mismatch: want logs got %s", got)
	}
}

func TestValidateDatasetTypeAcceptsSupportedTypes(t *testing.T) {
	for _, datasetType := range []string{"logs", "metrics", "traces"} {
		got, err := validateDatasetType(datasetType)
		if err != nil {
			t.Fatalf("expected %q to be valid: %v", datasetType, err)
		}
		if got != datasetType {
			t.Fatalf("got %q, want %q", got, datasetType)
		}
	}
}

func TestValidateDatasetTypeRejectsUnsupportedType(t *testing.T) {
	if _, err := validateDatasetType("events"); err == nil {
		t.Fatal("expected unsupported dataset type to fail")
	}
}

func TestEnsureDatasetTypeAcceptsMatchingType(t *testing.T) {
	if err := ensureDatasetType("server_logs", "logs", "logs"); err != nil {
		t.Fatalf("expected matching dataset type to pass: %v", err)
	}
}

func TestEnsureDatasetTypeRejectsMismatchedType(t *testing.T) {
	if err := ensureDatasetType("server_logs", "metrics", "logs"); err == nil {
		t.Fatal("expected mismatched dataset type to fail")
	}
}

func TestFetchInfoReturnsNotFoundError(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	client := internalHTTP.DefaultClient(&config.Profile{URL: server.URL, APIKey: "test-api-key"})
	_, err := fetchInfo(&client, "missing")
	if err == nil {
		t.Fatal("expected not found error")
	}
}

func TestDatasetInfoCompressionRatioJSON(t *testing.T) {
	for _, test := range []struct {
		name          string
		ingestionSize uint64
		storageSize   uint64
		wantIngestion string
		wantStorage   string
		want          string
	}{
		{name: "storage not reported", ingestionSize: 1400000, wantIngestion: "1.3 MiB", wantStorage: "0 B", want: "n/a"},
		{name: "empty dataset", wantIngestion: "0 B", wantStorage: "0 B", want: "n/a"},
		{name: "stored data available", ingestionSize: 1000, storageSize: 250, wantIngestion: "1000 B", wantStorage: "250 B", want: "75.00%"},
		{name: "GiB sizes", ingestionSize: 2684354560, storageSize: 1610612736, wantIngestion: "2.5 GiB", wantStorage: "1.5 GiB", want: "40.00%"},
	} {
		t.Run(test.name, func(t *testing.T) {
			useDatasetInfoStatsResponse(t, test.ingestionSize, test.storageSize)
			if err := StatDatasetCmd.Flags().Set("output", "json"); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = StatDatasetCmd.Flags().Set("output", "") })

			var output bytes.Buffer
			StatDatasetCmd.SetOut(&output)
			t.Cleanup(func() { StatDatasetCmd.SetOut(nil) })
			if err := StatDatasetCmd.RunE(StatDatasetCmd, []string{"events"}); err != nil {
				t.Fatal(err)
			}
			var result struct {
				Info struct {
					CompressionRatio string `json:"compression_ratio"`
					IngestionSize    string `json:"ingestion_size"`
					StorageSize      string `json:"storage_size"`
				} `json:"info"`
			}
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatalf("invalid JSON %q: %v", output.String(), err)
			}
			if result.Info.CompressionRatio != test.want {
				t.Fatalf("compression ratio = %q, want %q", result.Info.CompressionRatio, test.want)
			}
			if result.Info.IngestionSize != test.wantIngestion || result.Info.StorageSize != test.wantStorage {
				t.Fatalf("unexpected size display: ingestion=%q, storage=%q", result.Info.IngestionSize, result.Info.StorageSize)
			}
		})
	}
}

func TestDatasetInfoCompressionRatioText(t *testing.T) {
	useDatasetInfoStatsResponse(t, 1400000, 0)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = originalStdout })

	commandErr := StatDatasetCmd.RunE(StatDatasetCmd, []string{"events"})
	_ = writer.Close()
	os.Stdout = originalStdout
	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if commandErr != nil {
		t.Fatal(commandErr)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(output), "Compression Ratio: n/a") || !strings.Contains(string(output), "1.3 MiB") {
		t.Fatalf("unexpected text output: %q", output)
	}
}

func useDatasetInfoStatsResponse(t *testing.T, ingestionSize, storageSize uint64) {
	t.Helper()
	useCommandTransport(t, func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v1/logstream/events/info":
			return commandHTTPResponse(http.StatusOK, "200 OK", `{"telemetryType":"logs"}`), nil
		case "/api/v1/logstream/events/stats":
			body := fmt.Sprintf(`{"ingestion":{"count":3740,"size":%d},"storage":{"size":%d}}`, ingestionSize, storageSize)
			return commandHTTPResponse(http.StatusOK, "200 OK", body), nil
		case "/api/v1/logstream/events/retention", "/api/v1/logstream/events/alert":
			return commandHTTPResponse(http.StatusNotFound, "404 Not Found", ""), nil
		default:
			t.Fatalf("unexpected request path: %s", req.URL.Path)
			return nil, nil
		}
	})
}
