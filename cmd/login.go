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
	"pb/pkg/config"
	"pb/pkg/model/login"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var LoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to Parseable",
	Long: `Interactive login wizard for Parseable.

Select self-hosted and enter your server URL, credentials, and a
profile name. All settings are saved to ~/.config/pb/config.toml.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		_m, err := tea.NewProgram(login.New()).Run()
		if err != nil {
			return err
		}

		m, ok := _m.(login.Model)
		if !ok || !m.Done {
			return nil
		}

		if err := writeProfile(m.Profile, m.Name); err != nil {
			return fmt.Errorf("failed to save profile: %w", err)
		}
		return nil
	},
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
	if fileConfig.DefaultProfile == "" {
		fileConfig.DefaultProfile = profileName
	}
	return config.WriteConfigToFile(fileConfig)
}
