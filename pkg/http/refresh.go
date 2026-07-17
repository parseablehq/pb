package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/parseablehq/pb/pkg/config"
)

type cloudRefreshTransport struct {
	base    http.RoundTripper
	profile *config.Profile
	mu      sync.Mutex
}

type cloudRefreshRequest struct {
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
}

type cloudRefreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (transport *cloudRefreshTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := transport.base.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized || !transport.isProfileRequest(req) || !transport.canRefresh() {
		return resp, err
	}

	transport.mu.Lock()
	defer transport.mu.Unlock()

	requestToken := requestSessionCookie(req)
	if requestToken == transport.profile.SessionToken {
		if err := transport.refresh(req); err != nil {
			return resp, nil
		}
	}

	retry, err := cloneRequestForCloudRetry(req)
	if err != nil {
		return resp, nil
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		resp.Body.Close()
		return nil, err
	}
	resp.Body.Close()
	retry.Header.Del("Cookie")
	if err := AddAuthHeaders(retry, transport.profile); err != nil {
		return nil, err
	}
	return transport.base.RoundTrip(retry)
}

func (transport *cloudRefreshTransport) isProfileRequest(req *http.Request) bool {
	if transport.profile == nil || req == nil || req.URL == nil {
		return false
	}
	profileURL, err := url.Parse(transport.profile.URL)
	if err != nil {
		return false
	}
	return strings.EqualFold(req.URL.Scheme, profileURL.Scheme) &&
		strings.EqualFold(req.URL.Host, profileURL.Host)
}

func (transport *cloudRefreshTransport) canRefresh() bool {
	if transport.profile == nil || transport.profile.RefreshToken == "" || transport.profile.OrchestratorURL == "" {
		return false
	}
	mode, err := transport.profile.AuthMode()
	return err == nil && mode == config.AuthCloudOAuth
}

func (transport *cloudRefreshTransport) refresh(original *http.Request) error {
	oldRefreshToken := transport.profile.RefreshToken
	payload, err := json.Marshal(cloudRefreshRequest{
		GrantType:    "refresh_token",
		RefreshToken: oldRefreshToken,
	})
	if err != nil {
		return err
	}
	endpoint, err := url.JoinPath(transport.profile.OrchestratorURL, "api/v1/cli/oauth/token")
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(original.Context(), http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := transport.base.RoundTrip(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("cloud session refresh failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var result cloudRefreshResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}
	if result.AccessToken == "" || result.RefreshToken == "" {
		return fmt.Errorf("cloud session refresh response missing access_token or refresh_token")
	}
	if err := config.UpdateCloudTokens(oldRefreshToken, result.AccessToken, result.RefreshToken); err != nil {
		return err
	}
	transport.profile.SessionToken = result.AccessToken
	transport.profile.RefreshToken = result.RefreshToken
	return nil
}

func requestSessionCookie(req *http.Request) string {
	cookie, err := req.Cookie("session")
	if err != nil {
		return ""
	}
	return cookie.Value
}

func cloneRequestForCloudRetry(req *http.Request) (*http.Request, error) {
	retry := req.Clone(req.Context())
	if req.Body == nil {
		return retry, nil
	}
	if req.GetBody == nil {
		return nil, fmt.Errorf("request body cannot be replayed after cloud session refresh")
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	retry.Body = body
	return retry, nil
}
