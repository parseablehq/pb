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
	"pb/pkg/ui"
	"strings"

	"github.com/charmbracelet/lipgloss"
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
			statusMessage := statusErrorMessage(err)
			errStyle := lipgloss.NewStyle().Foreground(ui.Active.Err).Bold(true)
			fmt.Printf("Status  : %s\n", errStyle.Render("✗ Not connected"))
			fmt.Printf("Error   : %s\n", statusMessage)
			return fmt.Errorf("status check failed: %s", statusMessage)
		}

		okStyle := lipgloss.NewStyle().Foreground(ui.Active.Ok).Bold(true)
		fmt.Printf("Status  : %s\n", okStyle.Render("✓ Connected"))
		fmt.Printf("Version : %s\n", about.Version)
		return nil
	},
}

func statusErrorMessage(err error) string {
	message := err.Error()
	if strings.Contains(message, "Status Code: 401") || strings.Contains(message, "Status Code: 403") {
		return "Authentication failed: invalid username/password or API key"
	}
	return message
}
