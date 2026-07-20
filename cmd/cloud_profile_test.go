package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/parseablehq/pb/pkg/config"
)

func TestCloudProfileAddOutputDoesNotExposeCredentials(t *testing.T) {
	secret := "psk_secret"
	output := cloudProfileAddOutput{
		Status: "success",
		Name:   "agent",
		Profile: safeProfileOutput(config.Profile{
			URL:             "https://workspace.example.com",
			Cloud:           true,
			APIKey:          secret,
			TenantID:        "tenant-id",
			OrchestratorURL: "https://orchestrator.example.com",
		}),
	}

	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatal("cloud profile JSON output exposed the API key")
	}
}

func TestCloudProfileAddSupportsJSONOutput(t *testing.T) {
	flag := CloudProfileAddCmd.Flags().Lookup("output")
	if flag == nil || flag.Shorthand != "o" || flag.DefValue != "text" {
		t.Fatalf("unexpected output flag: %#v", flag)
	}
}

func TestCloudRequestsRequireConfiguredOrchestratorURL(t *testing.T) {
	if _, err := cloudProfileFromDeviceLogin(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "URL is not configured") {
		t.Fatalf("unexpected device login error: %v", err)
	}
	if _, err := validateCloudAPIKey("", "api-key"); err == nil || !strings.Contains(err.Error(), "URL is not configured") {
		t.Fatalf("unexpected API-key validation error: %v", err)
	}
}

func TestCloudProfileAddJSONFlow(t *testing.T) {
	const secret = "psk_agent_secret"
	oldAuthToken := config.CloudOrchestratorAuthToken
	config.CloudOrchestratorAuthToken = "orchestrator-token"
	t.Cleanup(func() { config.CloudOrchestratorAuthToken = oldAuthToken })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cli/apikey/validate" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer orchestrator-token" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		if got := r.Header.Get("x-api-key"); got != secret {
			t.Fatalf("unexpected x-api-key header %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"workspace_id":"workspace-id",
			"workspace_name":"Agent Workspace",
			"tenant_id":"tenant-id",
			"url":"https://workspace.example.com",
			"ingest_url":"https://ingest.example.com"
		}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var stdout bytes.Buffer
	CloudCmd.SetOut(&stdout)
	CloudCmd.SetErr(&stdout)
	CloudCmd.SetArgs([]string{
		"profile", "add",
		"--api-key", secret,
		"--name", "agent",
		"--orchestrator-url", server.URL,
		"--output", "json",
	})
	t.Cleanup(func() {
		CloudCmd.SetArgs(nil)
		CloudCmd.SetOut(nil)
		CloudCmd.SetErr(nil)
		_ = CloudProfileAddCmd.Flags().Set("api-key", "")
		_ = CloudProfileAddCmd.Flags().Set("name", "")
		_ = CloudProfileAddCmd.Flags().Set("orchestrator-url", config.CloudOrchestratorURL)
		_ = CloudProfileAddCmd.Flags().Set("output", "text")
	})

	if err := CloudCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), secret) {
		t.Fatal("command output exposed the API key")
	}

	var output cloudProfileAddOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("invalid JSON output %q: %v", stdout.String(), err)
	}
	if output.Status != "success" || output.Name != "agent" || output.Profile.WorkspaceID != "workspace-id" {
		t.Fatalf("unexpected output: %#v", output)
	}

	fileConfig, err := config.ReadConfigFromFile()
	if err != nil {
		t.Fatal(err)
	}
	profile := fileConfig.Profiles["agent"]
	if profile.APIKey != secret || profile.WorkspaceID != "workspace-id" {
		t.Fatalf("unexpected saved profile: %#v", profile)
	}
}

func TestCloudDeviceRequestsUseOrchestratorBearerToken(t *testing.T) {
	oldAuthToken := config.CloudOrchestratorAuthToken
	config.CloudOrchestratorAuthToken = "orchestrator-token"
	t.Cleanup(func() { config.CloudOrchestratorAuthToken = oldAuthToken })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer orchestrator-token" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/cli/device/code":
			_, _ = w.Write([]byte(`{"device_code":"device-code","user_code":"user-code"}`))
		case "/api/v1/cli/oauth/token":
			_, _ = w.Write([]byte(`{"access_token":"session","refresh_token":"refresh"}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := server.Client()
	if _, err := requestCloudDeviceCode(context.Background(), client, server.URL); err != nil {
		t.Fatal(err)
	}
	var tokens cloudDeviceTokenResponse
	oauthErr, err := doCloudOAuthTokenRequest(
		context.Background(),
		client,
		server.URL+"/api/v1/cli/oauth/token",
		[]byte(`{"grant_type":"refresh_token","refresh_token":"refresh"}`),
		&tokens,
	)
	if err != nil {
		t.Fatal(err)
	}
	if oauthErr != nil {
		t.Fatalf("unexpected OAuth error: %#v", oauthErr)
	}
}

func TestCloudCommandsDoNotExposeDefaultFlag(t *testing.T) {
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
