// Copyright (c) 2024 Parseable, Inc
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package cmd

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/parseablehq/pb/pkg/config"
	"github.com/spf13/cobra"
)

const (
	cloudDefaultOrchestratorURL       = "https://orchestrator.cloud-staging.parseable.com"
	cloudDefaultOrchestratorAuthToken = "Add Authbearer token"
	cloudDefaultClerkPublishableKey   = "pk_test_bW92ZWQtcGlyYW5oYS02My5jbGVyay5hY2NvdW50cy5kZXYk"
	cloudDefaultCallbackAddr          = "127.0.0.1:9999"
)

const (
	cloudOAuthProfilePath     = "api/v1/organizations"
	cloudParseableSessionPath = "api/v1/o/code"
)

const cloudLoginTimeout = 5 * time.Minute

var (
	cloudAPIKey                string
	cloudCallbackAddr          string
	cloudOrchestratorAuthToken string
	cloudClerkPublishableKey   string
	cloudProfileName           string
	cloudOrchestratorURL       string
	cloudWorkspaceURL          string
	cloudTenantID              string
	cloudClerkSessionTokenFlag string
	cloudSetDefault            bool
	cloudForceOverwrite        bool
	cloudDirectCodeExchange    bool
)

type cloudAPIKeyValidationResponse struct {
	OrgID         string `json:"org_id"`
	OrgName       string `json:"org_name"`
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	TenantID      string `json:"tenant_id"`
	URL           string `json:"url"`
	IngestURL     string `json:"ingest_url"`
	State         string `json:"state"`
	MultiTenant   bool   `json:"multi_tenant"`
}

type cloudLoginCallbackResult struct {
	Token string
	Err   error
}

type cloudOAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

type cloudOAuthJWTClaims struct {
	Subject   string `json:"sub"`
	SessionID string `json:"sid"`
}

type cloudParseableSessionResponse struct {
	Session  string `json:"session"`
	Username string `json:"username"`
	UserID   string `json:"user_id"`
}

type cloudOrganizationResponse struct {
	OrgID      string                   `json:"org_id"`
	OrgName    string                   `json:"org_name"`
	Workspaces []cloudWorkspaceResponse `json:"workspaces"`
}

type cloudOrganizationsResponse struct {
	Organizations []cloudOrganizationResponse `json:"organizations"`
}

type cloudWorkspaceResponse struct {
	WorkspaceID      string `json:"workspace_id"`
	WorkspaceName    string `json:"workspace_name"`
	OrgName          string `json:"org_name"`
	PrismURL         string `json:"prism_url"`
	URL              string `json:"url"`
	MultiTenant      bool   `json:"multi_tenant"`
	State            string `json:"state"`
	InvitationStatus string `json:"invitation_status"`
}

var CloudCmd = &cobra.Command{
	Use:   "cloud",
	Short: "Manage Parseable Cloud profiles",
	Long:  "\ncloud command is used to configure Parseable Cloud access.",
}

var CloudLoginCmd = &cobra.Command{
	Use:     "login",
	Example: "  pb cloud login\n  pb cloud login --name prod",
	Short:   "Login to Parseable Cloud",
	Long:    "\nLogin to Parseable Cloud using Clerk browser login.",
	RunE: func(_ *cobra.Command, _ []string) error {
		orchestratorURL := strings.TrimSpace(cloudOrchestratorURL)
		if orchestratorURL == "" {
			orchestratorURL = cloudDefaultOrchestratorURL
		}

		publishableKey := strings.TrimSpace(cloudClerkPublishableKey)
		if publishableKey == "" {
			publishableKey = cloudDefaultClerkPublishableKey
		}

		callbackAddr := strings.TrimSpace(cloudCallbackAddr)
		if callbackAddr == "" {
			callbackAddr = cloudDefaultCallbackAddr
		}

		profile, err := cloudProfileFromBrowserLogin(orchestratorURL, publishableKey, callbackAddr)
		if err != nil {
			return err
		}

		profileName := strings.TrimSpace(cloudProfileName)
		if profileName == "" {
			profileName = cloudProfileNameFromSession(profile)
		}

		if err := saveCloudProfile(profileName, *profile, cloudSetDefault, cloudForceOverwrite); err != nil {
			return err
		}

		fmt.Printf("Cloud profile %s added successfully\n", profileName)
		if profile.WorkspaceName != "" {
			fmt.Printf("Workspace: %s (%s)\n", profile.WorkspaceName, profile.WorkspaceID)
		} else {
			fmt.Printf("Workspace: %s\n", profile.WorkspaceID)
		}
		fmt.Printf("URL: %s\n", profile.URL)
		return nil
	},
}

func cloudProfileFromBrowserLogin(orchestratorURL, publishableKey, callbackAddr string) (*config.Profile, error) {
	if strings.TrimSpace(publishableKey) == "" {
		return nil, errors.New("cloud clerk publishable key is required")
	}

	state, err := generateCloudLoginState()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cloudLoginTimeout)
	defer cancel()

	resultCh, stop, loginURL, err := startCloudClerkLoginServer(ctx, callbackAddr, state, publishableKey)
	if err != nil {
		return nil, err
	}
	defer stop()

	fmt.Printf("Opening browser for Parseable Cloud login...\n")
	if err := openExternalBrowser(loginURL); err != nil {
		fmt.Printf("Could not open browser automatically: %v\n", err)
	}
	fmt.Printf("Login URL:\n%s\n", loginURL)
	fmt.Printf("Waiting for Clerk session token on %s\n", loginURL)

	var result cloudLoginCallbackResult
	select {
	case result = <-resultCh:
	case <-ctx.Done():
		return nil, fmt.Errorf("timed out waiting for cloud login callback")
	}
	if result.Err != nil {
		return nil, result.Err
	}

	clerkSessionToken := strings.TrimSpace(result.Token)

	if cloudDirectCodeExchange {
		return cloudProfileFromDirectCodeExchange(cloudWorkspaceURL, cloudTenantID, clerkSessionToken, clerkSessionToken)
	}

	return cloudProfileFromOAuthTokens(orchestratorURL, &cloudOAuthTokenResponse{
		AccessToken: clerkSessionToken,
		IDToken:     clerkSessionToken,
	})
}

var CloudProfileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage Parseable Cloud profiles",
	Long:  "\ncloud profile command is used to add Parseable Cloud profiles.",
}

var CloudProfileAddCmd = &cobra.Command{
	Use:     "add",
	Example: "  pb cloud profile add --api-key psk_xxx\n  pb cloud profile add --api-key psk_xxx --name prod",
	Short:   "Add a Parseable Cloud profile",
	Long:    "\nAdd a Parseable Cloud profile using an API key created in Parseable Cloud.",
	RunE: func(_ *cobra.Command, _ []string) error {
		apiKey := strings.TrimSpace(cloudAPIKey)
		if apiKey == "" {
			return errors.New("api key is required. pass --api-key")
		}

		orchestratorURL := strings.TrimSpace(cloudOrchestratorURL)
		if orchestratorURL == "" {
			orchestratorURL = cloudDefaultOrchestratorURL
		}

		result, err := validateCloudAPIKey(orchestratorURL, apiKey)
		if err != nil {
			return err
		}

		profileName := strings.TrimSpace(cloudProfileName)
		if profileName == "" {
			profileName = cloudProfileNameFromWorkspace(result)
		}

		profile := config.Profile{
			URL:             result.URL,
			Cloud:           true,
			APIKey:          apiKey,
			TenantID:        result.TenantID,
			IngestURL:       result.IngestURL,
			WorkspaceID:     result.WorkspaceID,
			WorkspaceName:   result.WorkspaceName,
			OrchestratorURL: orchestratorURL,
		}

		if err := saveCloudProfile(profileName, profile, cloudSetDefault, cloudForceOverwrite); err != nil {
			return err
		}

		fmt.Printf("Cloud profile %s added successfully\n", profileName)
		if result.WorkspaceName != "" {
			fmt.Printf("Workspace: %s (%s)\n", result.WorkspaceName, result.WorkspaceID)
		} else {
			fmt.Printf("Workspace: %s\n", result.WorkspaceID)
		}
		fmt.Printf("URL: %s\n", result.URL)
		return nil
	},
}

func init() {
	CloudLoginCmd.Flags().StringVar(&cloudProfileName, "name", "", "profile name")
	CloudLoginCmd.Flags().StringVar(&cloudOrchestratorURL, "orchestrator-url", cloudDefaultOrchestratorURL, "Parseable Cloud orchestrator URL")
	CloudLoginCmd.Flags().StringVar(&cloudOrchestratorAuthToken, "orchestrator-auth-token", cloudDefaultOrchestratorAuthToken, "Parseable Cloud orchestrator auth token")
	CloudLoginCmd.Flags().StringVar(&cloudClerkPublishableKey, "clerk-publishable-key", cloudDefaultClerkPublishableKey, "Clerk publishable key")
	CloudLoginCmd.Flags().StringVar(&cloudCallbackAddr, "callback-addr", cloudDefaultCallbackAddr, "local callback listen address")
	CloudLoginCmd.Flags().StringVar(&cloudWorkspaceURL, "url", "", "Parseable Cloud workspace URL for direct code exchange")
	CloudLoginCmd.Flags().StringVar(&cloudTenantID, "tenant-id", "", "Parseable Cloud tenant/workspace ID for direct code exchange")
	CloudLoginCmd.Flags().StringVar(&cloudClerkSessionTokenFlag, "clerk-session-token", "", "Clerk session token header for direct code exchange")
	CloudLoginCmd.Flags().BoolVar(&cloudSetDefault, "default", false, "set this profile as default")
	CloudLoginCmd.Flags().BoolVar(&cloudForceOverwrite, "force", false, "overwrite existing profile")
	CloudLoginCmd.Flags().BoolVar(&cloudDirectCodeExchange, "direct-code-exchange", false, "skip orchestrator and call workspace /api/v1/o/code with the Clerk session token")

	CloudProfileAddCmd.Flags().StringVar(&cloudAPIKey, "api-key", "", "Parseable Cloud API key")
	CloudProfileAddCmd.Flags().StringVar(&cloudProfileName, "name", "", "profile name")
	CloudProfileAddCmd.Flags().StringVar(&cloudOrchestratorURL, "orchestrator-url", cloudDefaultOrchestratorURL, "Parseable Cloud orchestrator URL")
	CloudProfileAddCmd.Flags().BoolVar(&cloudSetDefault, "default", false, "set this profile as default")
	CloudProfileAddCmd.Flags().BoolVar(&cloudForceOverwrite, "force", false, "overwrite existing profile")

	CloudProfileCmd.AddCommand(CloudProfileAddCmd)
	CloudCmd.AddCommand(CloudLoginCmd)
	CloudCmd.AddCommand(CloudProfileCmd)
}

func validateCloudAPIKey(orchestratorURL, apiKey string) (*cloudAPIKeyValidationResponse, error) {
	endpoint, err := url.JoinPath(orchestratorURL, "api/v1/apikey/validate")
	if err != nil {
		return nil, fmt.Errorf("invalid orchestrator URL: %w", err)
	}

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to validate cloud api key: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("cloud api key validation failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var result cloudAPIKeyValidationResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode cloud api key validation response: %w", err)
	}

	if result.URL == "" || result.TenantID == "" || result.WorkspaceID == "" {
		return nil, errors.New("cloud api key validation response missing url, tenant_id, or workspace_id")
	}

	return &result, nil
}

func cloudProfileFromOAuthTokens(orchestratorURL string, tokens *cloudOAuthTokenResponse) (*config.Profile, error) {
	if tokens == nil || strings.TrimSpace(tokens.AccessToken) == "" {
		return nil, errors.New("cloud oauth token response missing access_token")
	}
	clerkSessionToken := cloudClerkSessionToken(tokens)
	if strings.TrimSpace(clerkSessionToken) == "" {
		return nil, errors.New("cloud oauth token response missing clerk session token")
	}

	claims, err := cloudOAuthClaims(tokens)
	if err != nil {
		return nil, err
	}
	userID := claims.Subject

	endpoint, err := url.JoinPath(orchestratorURL, cloudOAuthProfilePath)
	if err != nil {
		return nil, fmt.Errorf("invalid orchestrator URL: %w", err)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid cloud organization URL: %w", err)
	}
	query := u.Query()
	query.Set("user_id", userID)
	u.RawQuery = query.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	authToken := strings.TrimSpace(cloudOrchestratorAuthToken)
	if authToken == "" {
		authToken = cloudDefaultOrchestratorAuthToken
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	} else {
		req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	}
	req.Header.Set("X-Clerk-Session-Token", clerkSessionToken)
	req.Header.Set("Accept", "application/json")

	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve cloud oauth profile: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("cloud oauth profile resolution failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	result, err := decodeCloudOrganizationResponse(body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode cloud organization response: %w", err)
	}

	workspace, err := cloudWorkspaceForProfile(result.Workspaces)
	if err != nil {
		return nil, err
	}

	workspaceURL := cloudWorkspaceEndpoint(*workspace)
	sessionToken, err := exchangeCloudParseableSession(workspaceURL, workspace.WorkspaceID, clerkSessionToken, clerkSessionToken)
	if err != nil {
		return nil, err
	}

	return &config.Profile{
		URL:             workspaceURL,
		Cloud:           true,
		SessionToken:    sessionToken,
		RefreshToken:    tokens.RefreshToken,
		TenantID:        workspace.WorkspaceID,
		WorkspaceID:     workspace.WorkspaceID,
		WorkspaceName:   workspace.WorkspaceName,
		OrchestratorURL: orchestratorURL,
		ClerkSessionID:  claims.SessionID,
	}, nil
}

func cloudProfileFromDirectCodeExchange(workspaceURL, tenantID, code, clerkSessionToken string) (*config.Profile, error) {
	workspaceURL = strings.TrimSpace(workspaceURL)
	tenantID = strings.TrimSpace(tenantID)
	clerkSessionToken = strings.TrimSpace(clerkSessionToken)
	if clerkSessionToken == "" {
		clerkSessionToken = strings.TrimSpace(cloudClerkSessionTokenFlag)
	}
	if workspaceURL == "" {
		return nil, errors.New("workspace URL is required for direct code exchange. pass --url")
	}
	if tenantID == "" {
		return nil, errors.New("tenant ID is required for direct code exchange. pass --tenant-id")
	}

	sessionToken, err := exchangeCloudParseableSession(workspaceURL, tenantID, code, clerkSessionToken)
	if err != nil {
		return nil, err
	}

	return &config.Profile{
		URL:          workspaceURL,
		Cloud:        true,
		SessionToken: sessionToken,
		TenantID:     tenantID,
		WorkspaceID:  tenantID,
	}, nil
}

func cloudOAuthClaims(tokens *cloudOAuthTokenResponse) (*cloudOAuthJWTClaims, error) {
	for _, token := range []string{tokens.IDToken, tokens.AccessToken} {
		claims, err := decodeCloudOAuthJWTClaims(token)
		if err == nil && claims.Subject != "" {
			return claims, nil
		}
	}
	return nil, errors.New("cloud oauth tokens missing user subject")
}

func cloudClerkSessionToken(tokens *cloudOAuthTokenResponse) string {
	if strings.TrimSpace(tokens.IDToken) != "" {
		return tokens.IDToken
	}
	return tokens.AccessToken
}

func decodeCloudOAuthJWTClaims(token string) (*cloudOAuthJWTClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, errors.New("invalid jwt")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}

	var claims cloudOAuthJWTClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	return &claims, nil
}

func decodeCloudOrganizationResponse(body []byte) (*cloudOrganizationResponse, error) {
	var org cloudOrganizationResponse
	if err := json.Unmarshal(body, &org); err == nil && len(org.Workspaces) > 0 {
		return &org, nil
	}

	var orgs []cloudOrganizationResponse
	if err := json.Unmarshal(body, &orgs); err == nil {
		return cloudOrganizationWithWorkspaces(orgs)
	}

	var wrapped cloudOrganizationsResponse
	if err := json.Unmarshal(body, &wrapped); err == nil {
		return cloudOrganizationWithWorkspaces(wrapped.Organizations)
	}

	return nil, errors.New("unknown cloud organization response shape")
}

func cloudOrganizationWithWorkspaces(orgs []cloudOrganizationResponse) (*cloudOrganizationResponse, error) {
	for i := range orgs {
		if len(orgs[i].Workspaces) > 0 {
			return &orgs[i], nil
		}
	}
	return nil, errors.New("cloud organization response has no workspaces")
}

func cloudWorkspaceForProfile(workspaces []cloudWorkspaceResponse) (*cloudWorkspaceResponse, error) {
	var missingURL, missingID, unusableInvitation int
	for _, workspace := range workspaces {
		if !cloudWorkspaceInvitationUsable(workspace.InvitationStatus) {
			unusableInvitation++
			continue
		}
		if cloudWorkspaceEndpoint(workspace) == "" {
			missingURL++
			continue
		}
		if strings.TrimSpace(workspace.WorkspaceID) == "" {
			missingID++
			continue
		}
		return &workspace, nil
	}
	return nil, fmt.Errorf(
		"cloud organization response missing usable workspace (total=%d missing_url=%d missing_workspace_id=%d unusable_invitation=%d)",
		len(workspaces), missingURL, missingID, unusableInvitation,
	)
}

func cloudWorkspaceEndpoint(workspace cloudWorkspaceResponse) string {
	if prismURL := strings.TrimSpace(workspace.PrismURL); prismURL != "" {
		return prismURL
	}
	return strings.TrimSpace(workspace.URL)
}

func cloudWorkspaceInvitationUsable(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "accepted", "active", "joined", "member":
		return true
	default:
		return false
	}
}

func exchangeCloudParseableSession(prismURL, tenantID, code, bearerToken string) (string, error) {
	if strings.TrimSpace(code) == "" {
		return "", errors.New("cloud oauth callback missing code")
	}

	endpoint, err := url.JoinPath(prismURL, cloudParseableSessionPath)
	if err != nil {
		return "", fmt.Errorf("invalid cloud workspace URL: %w", err)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("invalid cloud session URL: %w", err)
	}
	query := u.Query()
	query.Set("code", code)
	u.RawQuery = query.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	if tenantID != "" {
		req.Header.Set("x-p-tenant", tenantID)
	}
	if bearerToken != "" {
		req.Header.Set("X-Clerk-Session-Token", bearerToken)
	}

	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to exchange cloud oauth code for parseable session: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("cloud parseable session exchange failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var result cloudParseableSessionResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to decode cloud parseable session response: %w", err)
	}
	if strings.TrimSpace(result.Session) == "" {
		return "", errors.New("cloud parseable session response missing session")
	}
	return result.Session, nil
}

func startCloudClerkLoginServer(ctx context.Context, addr, expectedState, publishableKey string) (<-chan cloudLoginCallbackResult, func(), string, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to listen for cloud login callback on %s: %w", addr, err)
	}
	loginURL := "http://" + listener.Addr().String() + "/login"

	resultCh := make(chan cloudLoginCallbackResult, 1)
	mux := http.NewServeMux()
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login", http.StatusFound)
	})
	mux.HandleFunc("/login", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, cloudClerkLoginPage(publishableKey, expectedState))
	})
	mux.HandleFunc("/sso-callback", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, cloudClerkCallbackPage(publishableKey, expectedState))
	})
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			State string `json:"state"`
			Token string `json:"token"`
		}
		if r.Method == http.MethodPost {
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, "invalid callback payload", http.StatusBadRequest)
				return
			}
		} else {
			query := r.URL.Query()
			payload.State = query.Get("state")
			payload.Token = query.Get("token")
		}

		state := payload.State
		if state == "" || state != expectedState {
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}

		token := strings.TrimSpace(payload.Token)
		if token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}

		select {
		case resultCh <- cloudLoginCallbackResult{Token: token}:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true}`)
		default:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true}`)
		}
	})

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			select {
			case resultCh <- cloudLoginCallbackResult{Err: err}:
			default:
			}
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	stop := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}

	return resultCh, stop, loginURL, nil
}

func cloudClerkLoginPage(publishableKey, state string) string {
	publishableKeyJSON, _ := json.Marshal(publishableKey)
	stateJSON, _ := json.Marshal(state)
	clerkScriptURLJSON, _ := json.Marshal(cloudClerkScriptURL(publishableKey))
	return fmt.Sprintf(`<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Parseable Cloud Login</title>
  <script async crossorigin="anonymous" data-clerk-publishable-key=%s src=%s></script>
  <style>
    body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f8fafc}
  </style>
</head>
<body>
  <div id="clerk-ui"></div>
  <script>
    const expectedState = %s;
    const clerkUIEl = document.getElementById('clerk-ui');

    function waitForClerk() {
      return new Promise((resolve, reject) => {
        const deadline = Date.now() + 15000;
        const tick = () => {
          if (window.Clerk) return resolve(window.Clerk);
          if (Date.now() > deadline) return reject(new Error('Clerk SDK failed to load'));
          setTimeout(tick, 100);
        };
        tick();
      });
    }

    async function sendToken(token) {
      const res = await fetch('/callback', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({state: expectedState, token})
      });
      if (!res.ok) throw new Error(await res.text());
      clerkUIEl.replaceChildren();
      document.body.textContent = 'Login complete. You can close this window.';
      window.close();
    }

    async function submitIfSignedIn(clerk) {
      if (!clerk.session) return false;
      const token = await clerk.session.getToken();
      if (!token) return false;
      await sendToken(token);
      return true;
    }

    async function main() {
      const clerk = await waitForClerk();
      await clerk.load();

      const params = new URLSearchParams(window.location.search);
      if (params.get('complete') === '1') {
        if (await submitIfSignedIn(clerk)) return;
        const timer = setInterval(() => {
          submitIfSignedIn(clerk).then((done) => {
            if (done) clearInterval(timer);
          }).catch((err) => {
            document.body.textContent = err.message || String(err);
          });
        }, 1000);
        return;
      }

      if (clerk.session && typeof clerk.signOut === 'function') {
        await clerk.signOut();
        await clerk.load();
      }

      if (typeof clerk.mountSignIn !== 'function') {
        throw new Error('Clerk default sign-in UI unavailable for this SDK build');
      }

      clerk.mountSignIn(clerkUIEl, {
        afterSignInUrl: window.location.origin + '/login?state=' + encodeURIComponent(expectedState) + '&complete=1',
        afterSignUpUrl: window.location.origin + '/login?state=' + encodeURIComponent(expectedState) + '&complete=1',
        redirectUrl: window.location.origin + '/sso-callback?state=' + encodeURIComponent(expectedState),
        redirectUrlComplete: window.location.origin + '/login?state=' + encodeURIComponent(expectedState) + '&complete=1'
      });
    }

    main().catch((err) => {
      document.body.textContent = err.message || String(err);
    });
  </script>
</body>
</html>`, string(publishableKeyJSON), string(clerkScriptURLJSON), string(stateJSON))
}

func cloudClerkCallbackPage(publishableKey, state string) string {
	publishableKeyJSON, _ := json.Marshal(publishableKey)
	stateJSON, _ := json.Marshal(state)
	clerkScriptURLJSON, _ := json.Marshal(cloudClerkScriptURL(publishableKey))
	return fmt.Sprintf(`<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Parseable Cloud Login</title>
  <script async crossorigin="anonymous" data-clerk-publishable-key=%s src=%s></script>
</head>
<body>
  <script>
    const expectedState = %s;

    function waitForClerk() {
      return new Promise((resolve, reject) => {
        const deadline = Date.now() + 15000;
        const tick = () => {
          if (window.Clerk) return resolve(window.Clerk);
          if (Date.now() > deadline) return reject(new Error('Clerk SDK failed to load'));
          setTimeout(tick, 100);
        };
        tick();
      });
    }

    async function main() {
      const params = new URLSearchParams(window.location.search);
      if (params.get('state') !== expectedState) throw new Error('Invalid state');

      const clerk = await waitForClerk();
      await clerk.load();
      if (typeof clerk.handleRedirectCallback === 'function') {
        setTimeout(() => {
          window.location.href = '/login?state=' + encodeURIComponent(expectedState) + '&complete=1';
        }, 5000);
        await clerk.handleRedirectCallback({
          afterSignInUrl: '/login?state=' + encodeURIComponent(expectedState) + '&complete=1',
          afterSignUpUrl: '/login?state=' + encodeURIComponent(expectedState) + '&complete=1'
        });
        return;
      }

      window.location.href = '/login?state=' + encodeURIComponent(expectedState) + '&complete=1';
    }

    main().catch((err) => {
      document.body.textContent = err.message || String(err);
    });
  </script>
</body>
</html>`, string(publishableKeyJSON), string(clerkScriptURLJSON), string(stateJSON))
}

func cloudClerkScriptURL(publishableKey string) string {
	host := cloudClerkFrontendHost(publishableKey)
	if host == "" {
		return "https://cdn.jsdelivr.net/npm/@clerk/clerk-js@latest/dist/clerk.browser.js"
	}
	return "https://" + host + "/npm/@clerk/clerk-js@latest/dist/clerk.browser.js"
}

func cloudClerkFrontendHost(publishableKey string) string {
	for _, prefix := range []string{"pk_test_", "pk_live_"} {
		encoded, ok := strings.CutPrefix(strings.TrimSpace(publishableKey), prefix)
		if !ok {
			continue
		}
		decoded, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			decoded, err = base64.StdEncoding.DecodeString(encoded)
		}
		if err != nil {
			return ""
		}
		return strings.TrimSuffix(string(decoded), "$")
	}
	return ""
}

func generateCloudLoginState() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("failed to generate cloud login state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func openExternalBrowser(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}

func saveCloudProfile(name string, profile config.Profile, setDefault, force bool) error {
	if name == "" {
		return errors.New("profile name is required")
	}

	fileConfig, err := config.ReadConfigFromFile()
	if os.IsNotExist(err) {
		fileConfig = &config.Config{
			Profiles: make(map[string]config.Profile),
		}
	} else if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	if fileConfig.Profiles == nil {
		fileConfig.Profiles = make(map[string]config.Profile)
	}

	if _, exists := fileConfig.Profiles[name]; exists && !force {
		return fmt.Errorf("profile %q already exists. use --force to overwrite", name)
	}

	fileConfig.Profiles[name] = profile
	if fileConfig.DefaultProfile == "" || setDefault {
		fileConfig.DefaultProfile = name
	}

	return config.WriteConfigToFile(fileConfig)
}

func cloudProfileNameFromWorkspace(result *cloudAPIKeyValidationResponse) string {
	name := strings.TrimSpace(result.WorkspaceName)
	if name == "" {
		name = strings.TrimSpace(result.WorkspaceID)
	}

	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	return name
}

func cloudProfileNameFromSession(profile *config.Profile) string {
	name := strings.TrimSpace(profile.WorkspaceName)
	if name == "" {
		name = strings.TrimSpace(profile.WorkspaceID)
	}
	if name == "" {
		name = "cloud"
	}

	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	return name
}
