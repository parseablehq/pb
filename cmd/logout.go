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

	"github.com/spf13/cobra"
)

var LogoutCmd = &cobra.Command{
	Use:     "logout",
	Short:   "Logout from the current Parseable profile",
	Long:    "Removes the active profile (URL and credentials) from config.",
	Example: "  pb logout",
	RunE: func(_ *cobra.Command, _ []string) error {
		fileConfig, err := config.ReadConfigFromFile()
		if err != nil {
			return fmt.Errorf("no config found — nothing to logout from")
		}

		profileName := fileConfig.DefaultProfile
		if _, exists := fileConfig.Profiles[profileName]; !exists {
			return fmt.Errorf("no active profile found")
		}

		delete(fileConfig.Profiles, profileName)
		fileConfig.DefaultProfile = ""

		if err := config.WriteConfigToFile(fileConfig); err != nil {
			return fmt.Errorf("failed to update config: %w", err)
		}

		fmt.Printf("Logged out and removed profile '%s'\n", profileName)
		return nil
	},
}
