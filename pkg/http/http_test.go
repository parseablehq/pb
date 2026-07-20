package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/parseablehq/pb/pkg/config"
)

type testRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn testRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func testHTTPResponse(req *http.Request, statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestNewRequestWithNilProfileReturnsError(t *testing.T) {
	client := DefaultClient(nil)
	req, err := client.NewRequest(http.MethodGet, "about", nil)
	if err == nil || err.Error() != "profile is nil" {
		t.Fatalf("request=%v error=%v", req, err)
	}
	if req != nil {
		t.Fatalf("expected nil request, got %v", req)
	}
}

func TestCloudSessionRefreshesAndRetriesWorkspaceRequest(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	profile := config.Profile{
		URL:             "https://workspace.example.com",
		Cloud:           true,
		SessionToken:    "old-session",
		RefreshToken:    "old-refresh",
		TenantID:        "tenant-id",
		WorkspaceID:     "workspace-id",
		OrchestratorURL: "https://orchestrator.example.com",
	}
	if err := config.WriteConfigToFile(&config.Config{
		Profiles:       map[string]config.Profile{"cloud": profile},
		DefaultProfile: "cloud",
	}); err != nil {
		t.Fatal(err)
	}

	workspaceCalls := 0
	refreshCalls := 0
	base := testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "orchestrator.example.com":
			refreshCalls++
			var payload cloudRefreshRequest
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.RefreshToken != "old-refresh" {
				t.Fatalf("refresh token=%q", payload.RefreshToken)
			}
			return testHTTPResponse(req, http.StatusOK, `{"access_token":"new-session","refresh_token":"new-refresh"}`), nil
		case "workspace.example.com":
			workspaceCalls++
			cookie, _ := req.Cookie("session")
			if cookie != nil && cookie.Value == "new-session" {
				return testHTTPResponse(req, http.StatusOK, `{}`), nil
			}
			return testHTTPResponse(req, http.StatusUnauthorized, ``), nil
		default:
			t.Fatalf("unexpected host: %s", req.URL.Host)
			return nil, nil
		}
	})

	client := DefaultClientWithTransport(&profile, base)
	req, err := client.NewRequest(http.MethodGet, "about", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK || workspaceCalls != 2 || refreshCalls != 1 {
		t.Fatalf("status=%d workspace_calls=%d refresh_calls=%d", resp.StatusCode, workspaceCalls, refreshCalls)
	}
	if profile.SessionToken != "new-session" || profile.RefreshToken != "new-refresh" {
		t.Fatalf("tokens not updated: %#v", profile)
	}
}

func TestCloudSessionDoesNotRefreshCrossOriginRequest(t *testing.T) {
	profile := config.Profile{
		URL:             "https://workspace.example.com",
		Cloud:           true,
		SessionToken:    "session-token",
		RefreshToken:    "refresh-token",
		TenantID:        "tenant-id",
		WorkspaceID:     "workspace-id",
		OrchestratorURL: "https://orchestrator.example.com",
	}
	externalCalls := 0
	refreshCalls := 0
	base := testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "orchestrator.example.com" {
			refreshCalls++
		} else {
			externalCalls++
		}
		if _, err := req.Cookie("session"); err == nil || req.Header.Get("X-P-Tenant") != "" {
			t.Fatal("cloud credentials sent cross-origin")
		}
		return testHTTPResponse(req, http.StatusUnauthorized, ``), nil
	})

	client := DefaultClientWithTransport(&profile, base)
	req, err := http.NewRequest(http.MethodGet, "https://analytics.example.com/event", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if externalCalls != 1 || refreshCalls != 0 {
		t.Fatalf("external_calls=%d refresh_calls=%d", externalCalls, refreshCalls)
	}
}

func TestCloudSessionReturnsRefreshPersistenceError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	profile := config.Profile{
		URL:             "https://workspace.example.com",
		Cloud:           true,
		SessionToken:    "old-session",
		RefreshToken:    "old-refresh",
		TenantID:        "tenant-id",
		WorkspaceID:     "workspace-id",
		OrchestratorURL: "https://orchestrator.example.com",
	}
	if err := config.WriteConfigToFile(&config.Config{
		Profiles: map[string]config.Profile{
			"different": {
				Cloud:        true,
				RefreshToken: "different-refresh",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	base := testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "orchestrator.example.com" {
			return testHTTPResponse(req, http.StatusOK, `{"access_token":"new-session","refresh_token":"new-refresh"}`), nil
		}
		return testHTTPResponse(req, http.StatusUnauthorized, ``), nil
	})

	client := DefaultClientWithTransport(&profile, base)
	req, err := client.NewRequest(http.MethodGet, "about", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Client.Do(req)
	if err == nil || !strings.Contains(err.Error(), "failed to persist refreshed cloud tokens") {
		t.Fatalf("response=%v error=%v", resp, err)
	}
	if resp != nil {
		t.Fatalf("expected nil response, got %v", resp)
	}
	if profile.SessionToken != "new-session" || profile.RefreshToken != "new-refresh" {
		t.Fatalf("rotated tokens were not retained in memory: %#v", profile)
	}
}
