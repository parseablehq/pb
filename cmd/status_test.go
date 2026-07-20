package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/parseablehq/pb/pkg/config"
)

func TestStatusErrorMessageTreatsAuthFailuresAsCredentialErrors(t *testing.T) {
	input := errors.New("Request Failed\nStatus Code: 403 Forbidden\nResponse: You don't have permission")
	got := statusErrorMessage(input)
	want := "Authentication failed: invalid username/password or API key"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStatusErrorMessageKeepsOtherErrors(t *testing.T) {
	input := errors.New("dial tcp: connection refused")
	got := statusErrorMessage(input)
	if got != input.Error() {
		t.Fatalf("got %q, want %q", got, input.Error())
	}
}

func TestStatusJSONWhenConfigIsMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	result, err := runStatusJSON(t)
	if err == nil {
		t.Fatal("expected nonzero status error")
	}
	if result.Status != "error" || result.Healthy || result.Error != "no profile configured. run: pb login" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestStatusJSONWhenDefaultProfileIsMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := config.WriteConfigToFile(&config.Config{
		Profiles: map[string]config.Profile{
			"available": {URL: "https://example.com"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := runStatusJSON(t)
	if err == nil {
		t.Fatal("expected nonzero status error")
	}
	if result.Status != "error" || result.Healthy || result.Error != "no active profile. run: pb login" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func runStatusJSON(t *testing.T) (statusOutput, error) {
	t.Helper()
	if err := StatusCmd.Flags().Set("output", "json"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = StatusCmd.Flags().Set("output", "text") })

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = writer
	commandErr := StatusCmd.RunE(StatusCmd, nil)
	_ = writer.Close()
	os.Stdout = originalStdout

	data, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	var result statusOutput
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid JSON %q: %v", data, err)
	}
	return result, commandErr
}
