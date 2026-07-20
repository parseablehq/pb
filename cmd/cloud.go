// Copyright (c) 2024 Parseable, Inc
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/parseablehq/pb/pkg/config"
	internalHTTP "github.com/parseablehq/pb/pkg/http"
	"github.com/parseablehq/pb/pkg/ui"
	"github.com/spf13/cobra"
)

const (
	cloudDeviceClientID      = "pb-cli"
	cloudDefaultPollInterval = 5 * time.Second
	cloudDefaultLoginTimeout = 5 * time.Minute
)

var (
	cloudAPIKey          string
	cloudProfileName     string
	cloudOrchestratorURL string
	cloudForceOverwrite  bool
	cloudOutputFormat    string
)

type cloudProfileAddOutput struct {
	Status  string        `json:"status"`
	Name    string        `json:"name"`
	Profile profileOutput `json:"profile"`
}

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

type cloudDeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type cloudDeviceTokenRequest struct {
	GrantType    string `json:"grant_type"`
	DeviceCode   string `json:"device_code,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type cloudDeviceTokenResponse struct {
	AccessToken  string                  `json:"access_token"`
	TokenType    string                  `json:"token_type"`
	RefreshToken string                  `json:"refresh_token"`
	ExpiresIn    int                     `json:"expires_in"`
	IDToken      string                  `json:"id_token"`
	Workspace    cloudDeviceWorkspaceRef `json:"workspace"`
}

type cloudDeviceWorkspaceRef struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	TenantID string `json:"tenant_id"`
}

type cloudOAuthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type cloudDeviceLoginResultMsg struct {
	tokens *cloudDeviceTokenResponse
	err    error
}

type cloudDeviceLoginModel struct {
	spinner         spinner.Model
	verificationURL string
	userCode        string
	poll            func() (*cloudDeviceTokenResponse, error)
	openBrowser     tea.Cmd
	cancel          context.CancelFunc
	tokens          *cloudDeviceTokenResponse
	err             error
	done            bool
}

var cloudLoginTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(
	ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Accent }),
)

func newCloudDeviceLoginModel(
	verificationURL string,
	userCode string,
	poll func() (*cloudDeviceTokenResponse, error),
	openBrowser tea.Cmd,
	cancel context.CancelFunc,
) cloudDeviceLoginModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	return cloudDeviceLoginModel{
		spinner:         sp,
		verificationURL: verificationURL,
		userCode:        userCode,
		poll:            poll,
		openBrowser:     openBrowser,
		cancel:          cancel,
	}
}

func (m cloudDeviceLoginModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.openBrowser, func() tea.Msg {
		tokens, err := m.poll()
		return cloudDeviceLoginResultMsg{tokens: tokens, err: err}
	})
}

func (m cloudDeviceLoginModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.cancel()
			m.err = errors.New("cloud device login aborted")
			m.done = true
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case cloudDeviceLoginResultMsg:
		m.tokens = msg.tokens
		m.err = msg.err
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m cloudDeviceLoginModel) View() string {
	var status string
	switch {
	case !m.done:
		status = m.spinner.View() + " Waiting for authorization... [Ctrl+C to abort]"
	case m.err == nil:
		status = "✓ Authorization successful!"
	default:
		status = "✗ Authorization failed."
	}
	return fmt.Sprintf(
		"\n  %s\n  %s\n\n  To authenticate, please visit:\n  → %s\n\n  Enter this Confirmation Code:\n  ❯ %s\n\n  %s\n",
		cloudLoginTitleStyle.Render("PARSEABLE LOGIN"),
		strings.Repeat("─", 60),
		m.verificationURL,
		m.userCode,
		status,
	)
}

var CloudCmd = &cobra.Command{
	Use:   "cloud",
	Short: "Manage Parseable Cloud profiles",
	Long:  "\ncloud command is used to configure Parseable Cloud access.",
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
	RunE: func(cmd *cobra.Command, _ []string) error {
		apiKey := strings.TrimSpace(cloudAPIKey)
		if apiKey == "" {
			return errors.New("api key is required. pass --api-key")
		}
		if cloudOutputFormat != "text" && cloudOutputFormat != "json" {
			return fmt.Errorf("unsupported output format %q (expected text or json)", cloudOutputFormat)
		}

		orchestratorURL := cloudOrchestratorEndpoint()
		result, err := validateCloudAPIKey(cmd.Context(), orchestratorURL, apiKey)
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
		if err := saveCloudProfile(profileName, profile, cloudForceOverwrite, true); err != nil {
			return err
		}

		if cloudOutputFormat == "json" {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(cloudProfileAddOutput{
				Status:  "success",
				Name:    profileName,
				Profile: safeProfileOutput(profile),
			})
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Cloud profile %s added successfully\n", profileName)
		if result.WorkspaceName != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s (%s)\n", result.WorkspaceName, result.WorkspaceID)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s\n", result.WorkspaceID)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "URL: %s\n", result.URL)
		return nil
	},
}

func init() {
	CloudProfileAddCmd.Flags().StringVar(&cloudAPIKey, "api-key", "", "Parseable Cloud API key")
	CloudProfileAddCmd.Flags().StringVar(&cloudProfileName, "name", "", "profile name")
	CloudProfileAddCmd.Flags().StringVar(&cloudOrchestratorURL, "orchestrator-url", config.CloudOrchestratorURL, "Parseable Cloud orchestrator URL")
	CloudProfileAddCmd.Flags().BoolVar(&cloudForceOverwrite, "force", false, "overwrite existing profile")
	CloudProfileAddCmd.Flags().StringVarP(&cloudOutputFormat, "output", "o", "text", "Output format (text|json)")

	CloudProfileCmd.AddCommand(CloudProfileAddCmd)
	CloudCmd.AddCommand(CloudProfileCmd)
}

func cloudOrchestratorEndpoint() string {
	if endpoint := strings.TrimSpace(cloudOrchestratorURL); endpoint != "" {
		return endpoint
	}
	return config.CloudOrchestratorURL
}

func cloudProfileFromDeviceLogin(parent context.Context, orchestratorURL string) (*config.Profile, error) {
	if strings.TrimSpace(orchestratorURL) == "" {
		return nil, errors.New("cloud orchestrator URL is not configured")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	device, err := requestCloudDeviceCode(parent, client, orchestratorURL)
	if err != nil {
		return nil, err
	}

	verificationURL := strings.TrimSpace(device.VerificationURI)
	if verificationURL == "" {
		return nil, errors.New("cloud device authorization response missing verification URI")
	}

	timeout := cloudDefaultLoginTimeout
	if device.ExpiresIn > 0 {
		timeout = time.Duration(device.ExpiresIn) * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	model := newCloudDeviceLoginModel(
		verificationURL,
		device.UserCode,
		func() (*cloudDeviceTokenResponse, error) {
			return pollCloudDeviceToken(ctx, client, orchestratorURL, device)
		},
		func() tea.Msg {
			if err := waitForCloudPoll(ctx, 5*time.Second); err == nil {
				_ = openExternalBrowser(verificationURL)
			}
			return nil
		},
		cancel,
	)
	result, err := tea.NewProgram(model).Run()
	if err != nil {
		return nil, fmt.Errorf("failed to render cloud device login: %w", err)
	}
	loginResult, ok := result.(cloudDeviceLoginModel)
	if !ok {
		return nil, errors.New("cloud device login returned an unexpected result")
	}
	if loginResult.err != nil {
		return nil, loginResult.err
	}
	return cloudProfileFromDeviceTokens(orchestratorURL, loginResult.tokens)
}

func openExternalBrowser(rawURL string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", rawURL)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		command = exec.Command("xdg-open", rawURL)
	}
	return command.Start()
}

func requestCloudDeviceCode(ctx context.Context, client *http.Client, orchestratorURL string) (*cloudDeviceCodeResponse, error) {
	body, err := json.Marshal(map[string]string{"client_id": cloudDeviceClientID})
	if err != nil {
		return nil, err
	}
	endpoint, err := url.JoinPath(orchestratorURL, "api/v1/cli/device/code")
	if err != nil {
		return nil, fmt.Errorf("invalid orchestrator URL: %w", err)
	}

	var result cloudDeviceCodeResponse
	if err := doCloudJSON(ctx, client, http.MethodPost, endpoint, body, &result); err != nil {
		return nil, fmt.Errorf("failed to create cloud device code: %w", err)
	}
	if result.DeviceCode == "" || result.UserCode == "" {
		return nil, errors.New("cloud device authorization response missing device_code or user_code")
	}
	return &result, nil
}

func pollCloudDeviceToken(ctx context.Context, client *http.Client, orchestratorURL string, device *cloudDeviceCodeResponse) (*cloudDeviceTokenResponse, error) {
	endpoint, err := url.JoinPath(orchestratorURL, "api/v1/cli/oauth/token")
	if err != nil {
		return nil, fmt.Errorf("invalid orchestrator URL: %w", err)
	}
	interval := cloudDefaultPollInterval
	if device.Interval > 0 {
		interval = time.Duration(device.Interval) * time.Second
	}

	for {
		if err := waitForCloudPoll(ctx, interval); err != nil {
			return nil, errors.New("cloud device login expired or timed out")
		}

		payload, err := json.Marshal(cloudDeviceTokenRequest{
			GrantType:  "urn:ietf:params:oauth:grant-type:device_code",
			DeviceCode: device.DeviceCode,
		})
		if err != nil {
			return nil, err
		}
		var tokens cloudDeviceTokenResponse
		oauthErr, err := doCloudOAuthTokenRequest(ctx, client, endpoint, payload, &tokens)
		if err != nil {
			return nil, err
		}
		if oauthErr == nil {
			return &tokens, nil
		}

		switch oauthErr.Error {
		case "authorization_pending":
			continue
		case "expired_token":
			return nil, errors.New("cloud device code expired; run login again")
		default:
			return nil, fmt.Errorf("cloud device login failed: %s", cloudOAuthErrorMessage(oauthErr))
		}
	}
}

func waitForCloudPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func doCloudOAuthTokenRequest(ctx context.Context, client *http.Client, endpoint string, payload []byte, out *cloudDeviceTokenResponse) (*cloudOAuthError, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if err := internalHTTP.AddCloudOrchestratorAuth(req); err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloud token request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		if err := json.Unmarshal(body, out); err != nil {
			return nil, fmt.Errorf("failed to decode cloud token response: %w", err)
		}
		return nil, nil
	}

	var oauthErr cloudOAuthError
	if err := json.Unmarshal(body, &oauthErr); err != nil || oauthErr.Error == "" {
		return nil, fmt.Errorf("cloud token request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return &oauthErr, nil
}

func cloudOAuthErrorMessage(oauthErr *cloudOAuthError) string {
	if oauthErr.ErrorDescription != "" {
		return oauthErr.Error + ": " + oauthErr.ErrorDescription
	}
	return oauthErr.Error
}

func cloudProfileFromDeviceTokens(orchestratorURL string, tokens *cloudDeviceTokenResponse) (*config.Profile, error) {
	if tokens == nil || strings.TrimSpace(tokens.AccessToken) == "" {
		return nil, errors.New("cloud token response missing access_token")
	}
	if strings.TrimSpace(tokens.RefreshToken) == "" {
		return nil, errors.New("cloud token response missing refresh_token")
	}
	if strings.TrimSpace(tokens.Workspace.ID) == "" || strings.TrimSpace(tokens.Workspace.URL) == "" || strings.TrimSpace(tokens.Workspace.TenantID) == "" {
		return nil, errors.New("cloud token response missing workspace id, url, or tenant_id")
	}
	return &config.Profile{
		URL:             tokens.Workspace.URL,
		Cloud:           true,
		SessionToken:    tokens.AccessToken,
		RefreshToken:    tokens.RefreshToken,
		TenantID:        tokens.Workspace.TenantID,
		WorkspaceID:     tokens.Workspace.ID,
		OrchestratorURL: orchestratorURL,
	}, nil
}

func doCloudJSON(ctx context.Context, client *http.Client, method, endpoint string, payload []byte, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if err := internalHTTP.AddCloudOrchestratorAuth(req); err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

func validateCloudAPIKey(ctx context.Context, orchestratorURL, apiKey string) (*cloudAPIKeyValidationResponse, error) {
	if strings.TrimSpace(orchestratorURL) == "" {
		return nil, errors.New("cloud orchestrator URL is not configured")
	}
	endpoint, err := url.JoinPath(orchestratorURL, "api/v1/cli/apikey/validate")
	if err != nil {
		return nil, fmt.Errorf("invalid orchestrator URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if err := internalHTTP.AddCloudOrchestratorAuth(req); err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
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

func saveCloudProfile(name string, profile config.Profile, force, makeDefault bool) error {
	if name == "" {
		return errors.New("profile name is required")
	}
	fileConfig, err := config.ReadConfigFromFile()
	if os.IsNotExist(err) {
		fileConfig = &config.Config{Profiles: make(map[string]config.Profile)}
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
	if fileConfig.DefaultProfile == "" || makeDefault {
		fileConfig.DefaultProfile = name
	}
	return config.WriteConfigToFile(fileConfig)
}

func cloudProfileNameFromWorkspace(result *cloudAPIKeyValidationResponse) string {
	name := strings.TrimSpace(result.WorkspaceName)
	if name == "" {
		name = strings.TrimSpace(result.WorkspaceID)
	}
	return normalizeCloudProfileName(name)
}

func cloudProfileNameFromSession(profile *config.Profile) string {
	name := strings.TrimSpace(profile.WorkspaceName)
	if name == "" {
		name = strings.TrimSpace(profile.WorkspaceID)
	}
	if name == "" {
		name = "cloud"
	}
	return normalizeCloudProfileName(name)
}

func normalizeCloudProfileName(name string) string {
	return strings.ReplaceAll(strings.ToLower(name), " ", "-")
}
