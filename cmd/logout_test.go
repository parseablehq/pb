package cmd

import (
	"strings"
	"testing"

	"github.com/parseablehq/pb/pkg/config"
)

func TestLogoutYesDoesNotPromptWhenDefaultIsAmbiguous(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fileConfig := &config.Config{
		Profiles: map[string]config.Profile{
			"first":  {URL: "https://first.example.com"},
			"second": {URL: "https://second.example.com"},
		},
	}
	if err := config.WriteConfigToFile(fileConfig); err != nil {
		t.Fatal(err)
	}

	if err := LogoutCmd.Flags().Set("yes", "true"); err != nil {
		t.Fatal(err)
	}
	if err := LogoutCmd.Flags().Set("output", "text"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = LogoutCmd.Flags().Set("yes", "false")
		_ = LogoutCmd.Flags().Set("output", "text")
	})

	err := LogoutCmd.RunE(LogoutCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "pb profile default <name>") {
		t.Fatalf("expected non-interactive ambiguity error, got %v", err)
	}

	saved, err := config.ReadConfigFromFile()
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Profiles) != 2 || saved.DefaultProfile != "" {
		t.Fatalf("config changed after ambiguous logout: %#v", saved)
	}
}
