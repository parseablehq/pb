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
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/parseablehq/pb/pkg/config"
	"github.com/parseablehq/pb/pkg/model/login"
	"github.com/spf13/cobra"
)

var LoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to Parseable",
	Long: `Interactive login wizard for Parseable.

Select self-hosted or Parseable Cloud and enter the required
credentials. All settings are saved to ~/.config/pb/config.toml.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		_m, err := tea.NewProgram(login.New()).Run()
		if err != nil {
			return err
		}

		m, ok := _m.(login.Model)
		if !ok || !m.Done {
			return nil
		}

		if m.CloudDeviceLogin {
			profile, err := cloudProfileFromDeviceLogin(cmd.Context(), config.CloudOrchestratorURL)
			if err != nil {
				return err
			}
			m.Profile = *profile
			if m.Name == "" {
				m.Name = cloudProfileNameFromSession(profile)
			}
		} else if m.Profile.Cloud {
			profile, err := cloudProfileFromAPIKey(m.Profile.APIKey)
			if err != nil {
				return err
			}
			m.Profile = *profile
		}

		if err := writeProfile(m.Profile, m.Name); err != nil {
			return fmt.Errorf("failed to save profile: %w", err)
		}
		if m.CloudDeviceLogin {
			printDeviceLoginSuccess(m.Name)
		}
		return nil
	},
}

func printDeviceLoginSuccess(profileName string) {
	fmt.Printf("  ✓ Profile '%s' saved.\n\n", profileName)
	fmt.Println("  💡 Tip: You can add more profiles anytime using:")
	fmt.Println("     pb profile add <name> <url> [user] [pass]")
}

func cloudProfileFromAPIKey(apiKey string) (*config.Profile, error) {
	orchestratorURL := config.CloudOrchestratorURL
	result, err := validateCloudAPIKey(orchestratorURL, apiKey)
	if err != nil {
		return nil, err
	}

	return &config.Profile{
		URL:             result.URL,
		Cloud:           true,
		APIKey:          apiKey,
		TenantID:        result.TenantID,
		IngestURL:       result.IngestURL,
		WorkspaceID:     result.WorkspaceID,
		WorkspaceName:   result.WorkspaceName,
		OrchestratorURL: orchestratorURL,
	}, nil
}

func writeProfile(profile config.Profile, profileName string) error {
	fileConfig, err := config.ReadConfigFromFile()
	if err != nil {
		newConfig := config.Config{
			Profiles:       map[string]config.Profile{profileName: profile},
			DefaultProfile: profileName,
		}
		return config.WriteConfigToFile(&newConfig)
	}

	if fileConfig.Profiles == nil {
		fileConfig.Profiles = make(map[string]config.Profile)
	}
	fileConfig.Profiles[profileName] = profile
	fileConfig.DefaultProfile = profileName
	return config.WriteConfigToFile(fileConfig)
}
