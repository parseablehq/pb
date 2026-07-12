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
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/parseablehq/pb/pkg/config"
	"github.com/parseablehq/pb/pkg/model/defaultprofile"
	"github.com/spf13/cobra"
)

var LogoutCmd = &cobra.Command{
	Use:     "logout",
	Short:   "Logout from the current Parseable profile",
	Long:    "Removes the active profile (URL and credentials) from config.",
	Example: "  pb logout\n  pb logout --yes -o json",
	RunE: func(cmd *cobra.Command, _ []string) error {
		outputFormat, err := cmd.Flags().GetString("output")
		if err != nil {
			return err
		}
		yes, err := cmd.Flags().GetBool("yes")
		if err != nil {
			return err
		}
		if outputFormat == "json" && !yes {
			return fmt.Errorf("use --yes with -o json to avoid interactive prompts")
		}

		fileConfig, err := config.ReadConfigFromFile()
		if err != nil {
			return fmt.Errorf("no config found — nothing to logout from")
		}

		profileName := fileConfig.DefaultProfile
		activeProfile, exists := fileConfig.Profiles[profileName]
		if !exists || profileName == "" {
			if len(fileConfig.Profiles) == 0 {
				return fmt.Errorf("no active profile found")
			}
			if outputFormat == "json" && len(fileConfig.Profiles) > 1 {
				return fmt.Errorf("no active profile found")
			}
			selectedProfile, err := selectLogoutProfile(fileConfig.Profiles)
			if err != nil {
				return err
			}
			if selectedProfile == "" {
				fmt.Println("Logout canceled")
				return nil
			}
			profileName = selectedProfile
			activeProfile = fileConfig.Profiles[profileName]
		}

		if !yes && !confirmLogout(profileName, activeProfile.URL) {
			fmt.Println("Logout canceled")
			return nil
		}

		delete(fileConfig.Profiles, profileName)
		newDefaultProfile := ""
		switch len(fileConfig.Profiles) {
		case 0:
			fileConfig.DefaultProfile = ""
		case 1:
			for name := range fileConfig.Profiles {
				fileConfig.DefaultProfile = name
				newDefaultProfile = name
			}
		default:
			if yes {
				fileConfig.DefaultProfile = firstSortedProfileName(fileConfig.Profiles)
				newDefaultProfile = fileConfig.DefaultProfile
			} else {
				fmt.Println("Select a new default profile:")
				_m, err := tea.NewProgram(defaultprofile.New(fileConfig.Profiles)).Run()
				if err != nil {
					return fmt.Errorf("error selecting new default profile: %w", err)
				}
				m := _m.(defaultprofile.Model)
				if m.Success {
					fileConfig.DefaultProfile = m.Choice
					newDefaultProfile = m.Choice
				} else {
					fileConfig.DefaultProfile = ""
				}
			}
		}

		if err := config.WriteConfigToFile(fileConfig); err != nil {
			return fmt.Errorf("failed to update config: %w", err)
		}

		if outputFormat == "json" {
			return printLogoutJSON(logoutOutput{
				Status:         "ok",
				Profile:        profileName,
				URL:            activeProfile.URL,
				Removed:        true,
				DefaultProfile: fileConfig.DefaultProfile,
			})
		}

		fmt.Printf("Logged out and removed profile '%s'\n", profileName)
		if newDefaultProfile != "" {
			fmt.Printf("'%s' is now set as the default profile\n", newDefaultProfile)
		}
		return nil
	},
}

type logoutOutput struct {
	Status         string `json:"status"`
	Profile        string `json:"profile"`
	URL            string `json:"url"`
	Removed        bool   `json:"removed"`
	DefaultProfile string `json:"default_profile"`
}

func printLogoutJSON(result logoutOutput) error {
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal logout JSON: %w", err)
	}
	fmt.Println(string(jsonData))
	return nil
}

func firstSortedProfileName(profiles map[string]config.Profile) string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func selectLogoutProfile(profiles map[string]config.Profile) (string, error) {
	if len(profiles) == 1 {
		for name := range profiles {
			return name, nil
		}
	}

	fmt.Println("Select profile to logout:")
	_m, err := tea.NewProgram(defaultprofile.New(profiles)).Run()
	if err != nil {
		return "", fmt.Errorf("error selecting profile to logout: %w", err)
	}
	m := _m.(defaultprofile.Model)
	if !m.Success {
		return "", nil
	}
	return m.Choice, nil
}

func confirmLogout(profileName, profileURL string) bool {
	fmt.Printf(
		"Logout from profile %s %s?\n",
		SelectedStyle.Render("'"+profileName+"'"),
		StandardStyleAlt.Render("("+profileURL+")"),
	)
	fmt.Println(StandardStyleAlt.Render("This will remove the saved URL and credentials from config"))
	fmt.Printf("%s ", SelectedStyle.Render("Continue? [y/N]:"))

	response, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(response)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func init() {
	LogoutCmd.Flags().Bool("yes", false, "confirm logout without prompting")
	LogoutCmd.Flags().StringP("output", "o", "text", "Output format (text|json)")
}
