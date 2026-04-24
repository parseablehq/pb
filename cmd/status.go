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
	"pb/pkg/analytics"
	"pb/pkg/config"
	internalHTTP "pb/pkg/http"

	"github.com/spf13/cobra"
)

var StatusCmd = &cobra.Command{
	Use:     "status",
	Short:   "Check connection status for the active profile",
	Example: "  pb status",
	RunE: func(_ *cobra.Command, _ []string) error {
		fileConfig, err := config.ReadConfigFromFile()
		if err != nil {
			return fmt.Errorf("no profile configured. run: pb login")
		}

		profileName := fileConfig.DefaultProfile
		profile, exists := fileConfig.Profiles[profileName]
		if !exists || profileName == "" {
			return fmt.Errorf("no active profile. run: pb login")
		}

		fmt.Printf("Profile : %s\n", profileName)
		fmt.Printf("URL     : %s\n", profile.URL)

		client := internalHTTP.DefaultClient(&profile)
		about, err := analytics.FetchAbout(&client)
		if err != nil {
			fmt.Printf("Status  : ✗ Not connected\n")
			fmt.Printf("Error   : %s\n", err.Error())
			return nil
		}

		fmt.Printf("Status  : ✓ Connected\n")
		fmt.Printf("Version : %s\n", about.Version)
		return nil
	},
}
