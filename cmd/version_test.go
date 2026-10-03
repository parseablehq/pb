package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/parseablehq/pb/pkg/config"
	"github.com/spf13/cobra"
)

func TestPrintVersionUsesCallingCommandOutputFormat(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := config.WriteConfigToFile(&config.Config{
		Profiles: map[string]config.Profile{
			"test": {URL: "http://127.0.0.1:1", APIKey: "test-key"},
		},
		DefaultProfile: "test",
	}); err != nil {
		t.Fatal(err)
	}

	command := &cobra.Command{Use: "pb"}
	command.Flags().StringP("output", "o", "text", "output format")
	if err := command.Flags().Set("output", "json"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command.SetOut(&output)

	if err := PrintVersion(command, "v1", "client-commit"); err != nil {
		t.Fatal(err)
	}
	var result map[string]string
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("invalid version JSON %q: %v", output.String(), err)
	}
	if len(result) != 1 || result["version"] != "1" {
		t.Fatalf("unexpected version output: %+v", result)
	}
}

func TestPrintVersionTextWithProfile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := config.WriteConfigToFile(&config.Config{
		Profiles: map[string]config.Profile{
			"test": {URL: "http://127.0.0.1:1", APIKey: "test-key"},
		},
		DefaultProfile: "test",
	}); err != nil {
		t.Fatal(err)
	}

	command := &cobra.Command{Use: "pb"}
	command.Flags().StringP("output", "o", "text", "output format")
	var output bytes.Buffer
	command.SetOut(&output)
	if err := PrintVersion(command, "v1.0.0", "client-commit"); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "pb version 1.0.0\n" {
		t.Fatalf("unexpected version output: %q", got)
	}
}

func TestPrintVersionWithoutProfile(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		config *config.Config
	}{
		{name: "no config file"},
		{name: "no default profile", config: &config.Config{Profiles: map[string]config.Profile{}}},
		{name: "missing default profile", config: &config.Config{DefaultProfile: "missing"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			if scenario.config != nil {
				if err := config.WriteConfigToFile(scenario.config); err != nil {
					t.Fatal(err)
				}
			}

			for _, format := range []string{"text", "json"} {
				t.Run(format, func(t *testing.T) {
					command := &cobra.Command{Use: "pb"}
					command.Flags().StringP("output", "o", format, "output format")
					var output bytes.Buffer
					command.SetOut(&output)
					if err := PrintVersion(command, "v1", "client-commit"); err != nil {
						t.Fatal(err)
					}
					if format == "json" {
						var result map[string]string
						if err := json.Unmarshal(output.Bytes(), &result); err != nil {
							t.Fatalf("invalid version JSON %q: %v", output.String(), err)
						}
						if len(result) != 1 || result["version"] != "1" {
							t.Fatalf("unexpected version information: %v", result)
						}
					} else if got := output.String(); got != "pb version 1\n" {
						t.Fatalf("unexpected version output: %q", got)
					}
				})
			}
		})
	}
}
