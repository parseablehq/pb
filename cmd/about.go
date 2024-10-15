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
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Define the struct
type About struct {
	Version         string    `json:"version"`
	UIVersion       string    `json:"uiVersion"`
	Commit          string    `json:"commit"`
	DeploymentID    string    `json:"deploymentId"`
	UpdateAvailable bool      `json:"updateAvailable"`
	LatestVersion   string    `json:"latestVersion"`
	LLMActive       bool      `json:"llmActive"`
	LLMProvider     string    `json:"llmProvider"`
	OIDCActive      bool      `json:"oidcActive"`
	License         string    `json:"license"`
	Mode            string    `json:"mode"`
	Staging         string    `json:"staging"`
	HotTier         string    `json:"hotTier"`
	GRPCPort        int       `json:"grpcPort"`
	Store           Store     `json:"store"`
	Analytics       Analytics `json:"analytics"`
	QueryEngine     string    `json:"queryEngine"`
}

// Define the Store struct
type Store struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

// Define the Analytics struct
type Analytics struct {
	ClarityTag string `json:"clarityTag"`
}

func FetchAbout(client *HTTPClient) (about About, err error) {
	req, err := client.NewRequest("GET", "about", nil)
	if err != nil {
		return
	}

	resp, err := client.client.Do(req)
	if err != nil {
		return
	}

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		err = json.Unmarshal(bytes, &about)
	} else {
		body := string(bytes)
		body = fmt.Sprintf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		err = errors.New(body)
	}
	return
}
