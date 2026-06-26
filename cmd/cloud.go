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
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/parseablehq/pb/pkg/config"
	"github.com/spf13/cobra"
)

const (
	defaultCloudOrchestratorURL = "https://orchestrator.cloud-staging.parseable.com"
	envCloudAPIKey              = "PB_CLOUD_API_KEY"
	envCloudOrchestratorURL     = "PB_CLOUD_ORCHESTRATOR_URL"
)

var (
	cloudAPIKey          string
	cloudProfileName     string
	cloudOrchestratorURL string
	cloudSetDefault      bool
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
	RunE: func(_ *cobra.Command, _ []string) error {
		apiKey := strings.TrimSpace(cloudAPIKey)
		if apiKey == "" {
			return fmt.Errorf("api key is required. pass --api-key or set %s", envCloudAPIKey)
		}

		orchestratorURL := strings.TrimSpace(cloudOrchestratorURL)
		if orchestratorURL == "" {
			orchestratorURL = defaultCloudOrchestratorURL
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

		if err := saveCloudProfile(profileName, profile, cloudSetDefault); err != nil {
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
	CloudProfileAddCmd.Flags().StringVar(&cloudAPIKey, "api-key", os.Getenv(envCloudAPIKey), "Parseable Cloud API key")
	CloudProfileAddCmd.Flags().StringVar(&cloudProfileName, "name", "", "profile name")
	CloudProfileAddCmd.Flags().StringVar(&cloudOrchestratorURL, "orchestrator-url", cloudOrchestratorURLFromEnv(), "Parseable Cloud orchestrator URL")
	CloudProfileAddCmd.Flags().BoolVar(&cloudSetDefault, "default", false, "set this profile as default")

	CloudProfileCmd.AddCommand(CloudProfileAddCmd)
	CloudCmd.AddCommand(CloudProfileCmd)
}

func cloudOrchestratorURLFromEnv() string {
	if orchestratorURL := os.Getenv(envCloudOrchestratorURL); orchestratorURL != "" {
		return orchestratorURL
	}
	return defaultCloudOrchestratorURL
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

func saveCloudProfile(name string, profile config.Profile, setDefault bool) error {
	if name == "" {
		return errors.New("profile name is required")
	}

	fileConfig, err := config.ReadConfigFromFile()
	if err != nil {
		fileConfig = &config.Config{
			Profiles: make(map[string]config.Profile),
		}
	}

	if fileConfig.Profiles == nil {
		fileConfig.Profiles = make(map[string]config.Profile)
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
