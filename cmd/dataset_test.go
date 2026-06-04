package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"pb/pkg/config"
	internalHTTP "pb/pkg/http"
	"testing"
)

func TestFetchInfoUsesTelemetryType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/v1/logstream/fly_logs/info"; got != want {
			t.Fatalf("path mismatch: want %s got %s", want, got)
		}
		fmt.Fprint(w, `{"streamType":"UserDefined","telemetryType":"logs"}`)
	}))
	defer server.Close()

	client := internalHTTP.DefaultClient(&config.Profile{URL: server.URL})
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

	client := internalHTTP.DefaultClient(&config.Profile{URL: server.URL})
	_, err := fetchInfo(&client, "missing")
	if err == nil {
		t.Fatal("expected not found error")
	}
}
