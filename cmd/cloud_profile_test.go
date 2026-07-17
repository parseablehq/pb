package cmd

import (
	"testing"

	"github.com/parseablehq/pb/pkg/config"
)

func TestCloudCommandsDoNotExposeDefaultFlag(t *testing.T) {
	if flag := CloudLoginCmd.Flags().Lookup("default"); flag != nil {
		t.Fatal("cloud login still exposes --default")
	}
	if flag := CloudProfileAddCmd.Flags().Lookup("default"); flag != nil {
		t.Fatal("cloud profile add still exposes --default")
	}
}

func TestSaveCloudProfilePreservesExistingDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := saveCloudProfile("first", config.Profile{URL: "https://first.example.com"}, false, false); err != nil {
		t.Fatal(err)
	}
	if err := saveCloudProfile("second", config.Profile{URL: "https://second.example.com"}, false, false); err != nil {
		t.Fatal(err)
	}

	fileConfig, err := config.ReadConfigFromFile()
	if err != nil {
		t.Fatal(err)
	}
	if fileConfig.DefaultProfile != "first" {
		t.Fatalf("default profile changed to %q", fileConfig.DefaultProfile)
	}
}

func TestSaveCloudProfileCanMakeAddedProfileDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := saveCloudProfile("first", config.Profile{URL: "https://first.example.com"}, false, false); err != nil {
		t.Fatal(err)
	}
	if err := saveCloudProfile("second", config.Profile{URL: "https://second.example.com"}, false, true); err != nil {
		t.Fatal(err)
	}

	fileConfig, err := config.ReadConfigFromFile()
	if err != nil {
		t.Fatal(err)
	}
	if fileConfig.DefaultProfile != "second" {
		t.Fatalf("added profile was not made default: %q", fileConfig.DefaultProfile)
	}
}
