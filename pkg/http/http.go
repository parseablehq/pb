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
	"io"
	"net/http"
	"net/url"
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
	return HTTPClient{
		Client: http.Client{
			Timeout: 60 * time.Second,
		},
		Profile: profile,
	}
}

func (client *HTTPClient) baseAPIURL(path string) (x string) {
	x, _ = url.JoinPath(client.Profile.URL, "api/v1/", path)
	return
}

func (client *HTTPClient) NewRequest(method string, path string, body io.Reader) (req *http.Request, err error) {
	req, err = http.NewRequest(method, client.baseAPIURL(path), body)
	if err != nil {
		return
	}
	AddAuthHeaders(req, client.Profile)
	req.Header.Add("Content-Type", "application/json")
	return
}

func AddAuthHeaders(req *http.Request, profile *config.Profile) {
	if profile.Cloud && profile.APIKey != "" {
		req.Header.Set(apiKeyHeader, profile.APIKey)
		if profile.TenantID != "" {
			req.Header.Set(tenantHeader, profile.TenantID)
		}
		return
	}

	if profile.Token != "" {
		req.Header.Set("Authorization", "Bearer "+profile.Token)
		return
	}

	req.SetBasicAuth(profile.Username, profile.Password)
}
