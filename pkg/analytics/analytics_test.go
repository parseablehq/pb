package analytics

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestPostRunAnalyticsDoesNotCollectPrivateCommandData(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := CheckAndCreateULID(nil, nil); err != nil {
		t.Fatal(err)
	}

	var got Event
	originalTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	cmd := &cobra.Command{Use: "profile"}
	cmd.Annotations = map[string]string{
		"error":         "secret-error",
		"executionTime": "1s",
	}
	cmd.Flags().String("api-key", "", "")
	if err := cmd.Flags().Set("api-key", "secret-api-key"); err != nil {
		t.Fatal(err)
	}

	PostRunAnalytics(cmd, "profile", []string{"prod", "https://example.com", "user", "secret-password"})

	if got.Command.Name != "profile" {
		t.Fatalf("command name mismatch: %q", got.Command.Name)
	}
	if len(got.Command.Arguments) != 0 || len(got.Command.Flags) != 0 || got.Errors != nil {
		t.Fatalf("private command data collected: %#v", got)
	}
}
