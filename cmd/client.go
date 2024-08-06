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

	"pb/pkg/config"
)

type HTTPClient struct {
	client  http.Client
	profile *config.Profile
}

func DefaultClient() HTTPClient {
	return HTTPClient{
		client: http.Client{
			Timeout: 60 * time.Second,
		},
		profile: &DefaultProfile,
	}
}

func (client *HTTPClient) baseAPIURL(path string) (x string) {
	x, _ = url.JoinPath(client.profile.URL, "api/v1/", path)
	return
}

func (client *HTTPClient) NewRequest(method string, path string, body io.Reader) (req *http.Request, err error) {
	req, err = http.NewRequest(method, client.baseAPIURL(path), body)
	if err != nil {
		return
	}
	req.SetBasicAuth(client.profile.Username, client.profile.Password)
	req.Header.Add("Content-Type", "application/json")
	return
}
