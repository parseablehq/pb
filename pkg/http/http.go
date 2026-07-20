// Copyright (c) 2024 Parseable, Inc
//
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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/parseablehq/pb/pkg/config"
)

const (
	apiKeyHeader = "x-api-key"
	tenantHeader = "X-P-Tenant"
)

type HTTPClient struct {
	Client  http.Client
	Profile *config.Profile
}

func DefaultClient(profile *config.Profile) HTTPClient {
	return DefaultClientWithTransport(profile, http.DefaultTransport)
}

func DefaultClientWithTransport(profile *config.Profile, transport http.RoundTripper) HTTPClient {
	if transport == nil {
		transport = http.DefaultTransport
	}
	return HTTPClient{
		Client: http.Client{
			Timeout: 60 * time.Second,
			Transport: &cloudRefreshTransport{
				base:    transport,
				profile: profile,
			},
		},
		Profile: profile,
	}
}

func (client *HTTPClient) baseAPIURL(path string) (x string) {
	x, _ = url.JoinPath(client.Profile.URL, "api/v1/", path)
	return
}

func (client *HTTPClient) NewRequest(method string, path string, body io.Reader) (req *http.Request, err error) {
	if client.Profile == nil {
		return nil, errors.New("profile is nil")
	}
	req, err = http.NewRequest(method, client.baseAPIURL(path), body)
	if err != nil {
		return
	}
	if err = AddAuthHeaders(req, client.Profile); err != nil {
		return
	}
	req.Header.Add("Content-Type", "application/json")
	debugRequestHeaders(req)
	return
}

func AddAuthHeaders(req *http.Request, profile *config.Profile) error {
	if profile == nil {
		return errors.New("profile is nil")
	}

	mode, err := profile.AuthMode()
	if err != nil {
		return err
	}

	switch mode {
	case config.AuthCloudAPIKey:
		req.Header.Set(apiKeyHeader, profile.APIKey)
		req.Header.Set(tenantHeader, profile.TenantID)
	case config.AuthCloudOAuth:
		req.AddCookie(&http.Cookie{Name: "session", Value: profile.SessionToken})
		req.Header.Set(tenantHeader, profile.TenantID)
	case config.AuthSelfHostedAPIKey:
		req.Header.Set(apiKeyHeader, profile.APIKey)
	case config.AuthSelfHostedBasic:
		req.SetBasicAuth(profile.Username, profile.Password)
	}
	return nil
}

// AddCloudOrchestratorAuth adds the bearer token required by CLI auth routes.
func AddCloudOrchestratorAuth(req *http.Request) error {
	if req == nil {
		return errors.New("request is nil")
	}
	token := strings.TrimSpace(config.CloudOrchestratorAuthToken)
	if token == "" {
		return errors.New("cloud orchestrator auth token is not configured")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

func debugRequestHeaders(req *http.Request) {
	if os.Getenv("PB_DEBUG_HTTP_HEADERS") == "" {
		return
	}

	fmt.Fprintf(os.Stderr, "pb debug request: %s %s\n", req.Method, req.URL.String())
	for key, values := range req.Header {
		value := strings.Join(values, ",")
		switch strings.ToLower(key) {
		case "authorization", apiKeyHeader, "cookie":
			value = "<redacted>"
		}
		fmt.Fprintf(os.Stderr, "pb debug header: %s: %s\n", key, value)
	}
}
