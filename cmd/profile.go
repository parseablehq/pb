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
	"net/url"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/parseablehq/pb/pkg/config"
	"github.com/parseablehq/pb/pkg/model/credential"
	"github.com/parseablehq/pb/pkg/model/defaultprofile"
	"github.com/spf13/cobra"
)

// ProfileListItem is a struct to hold the profile list items
type ProfileListItem struct {
	title, url, user string
}

type profileOutput struct {
	URL             string `json:"url"`
	Username        string `json:"username,omitempty"`
	Cloud           bool   `json:"cloud"`
	TenantID        string `json:"tenant_id,omitempty"`
	IngestURL       string `json:"ingest_url,omitempty"`
	WorkspaceID     string `json:"workspace_id,omitempty"`
	WorkspaceName   string `json:"workspace_name,omitempty"`
	OrchestratorURL string `json:"orchestrator_url,omitempty"`
}

func safeProfileOutput(profile config.Profile) profileOutput {
	return profileOutput{
		URL:             profile.URL,
		Username:        profile.Username,
		Cloud:           profile.Cloud,
		TenantID:        profile.TenantID,
		IngestURL:       profile.IngestURL,
		WorkspaceID:     profile.WorkspaceID,
		WorkspaceName:   profile.WorkspaceName,
		OrchestratorURL: profile.OrchestratorURL,
	}
}

func safeProfilesOutput(profiles map[string]config.Profile) map[string]profileOutput {
	result := make(map[string]profileOutput, len(profiles))
	for name, profile := range profiles {
		result[name] = safeProfileOutput(profile)
	}
	return result
}

func (item *ProfileListItem) Render(highlight bool) string {
	if highlight {
		lines := []string{
			SelectedStyle.Render(item.title),
			SelectedStyleAlt.Render(fmt.Sprintf("url: %s", item.url)),
		}
		if item.user != "" {
			lines = append(lines, SelectedStyleAlt.Render(fmt.Sprintf("user: %s", item.user)))
		}
		return SelectedItemOuter.Render(strings.Join(lines, "\n"))
	}
	lines := []string{
		StandardStyle.Render(item.title),
		StandardStyleAlt.Render(fmt.Sprintf("url: %s", item.url)),
	}
	if item.user != "" {
		lines = append(lines, StandardStyleAlt.Render(fmt.Sprintf("user: %s", item.user)))
	}
	return ItemOuter.Render(strings.Join(lines, "\n"))
}

// Add an output flag to specify the output format.
var (
	outputFormat  string
	profileAPIKey string
)

// Initialize flags
func init() {
	AddProfileCmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (text|json)")
	AddProfileCmd.Flags().StringVar(&profileAPIKey, "api-key", "", "API key for self-hosted authentication")
	RemoveProfileCmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (text|json)")
	DefaultProfileCmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (text|json)")
	ListProfileCmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (text|json)")
}

func outputResult(v interface{}) error {
	if outputFormat == "json" {
		jsonData, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(jsonData))
	} else {
		fmt.Println(v)
	}
	return nil
}

var AddProfileCmd = &cobra.Command{
	Use:     "add profile-name url [username] [password]",
	Example: "  pb profile add local_parseable http://0.0.0.0:8000 admin admin\n  pb profile add local_parseable http://0.0.0.0:8000 --api-key psk_xxx",
	Short:   "Add a new profile",
	Long:    "Add a new profile to the config file",
	Args: func(cmd *cobra.Command, args []string) error {
		if err := cobra.MinimumNArgs(2)(cmd, args); err != nil {
			return err
		}
		if strings.TrimSpace(profileAPIKey) != "" && len(args) != 2 {
			return errors.New("--api-key cannot be combined with username/password")
		}
		return cobra.MaximumNArgs(4)(cmd, args)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Annotations == nil {
			cmd.Annotations = make(map[string]string)
		}
		startTime := time.Now()
		var commandError error

		// Parsing input and handling errors
		name := args[0]
		url, err := url.Parse(args[1])
		if err != nil {
			commandError = fmt.Errorf("error parsing URL: %s", err)
			cmd.Annotations["error"] = commandError.Error()
			return commandError
		}

		apiKey := strings.TrimSpace(profileAPIKey)
		profile := config.Profile{Cloud: false, URL: url.String()}
		if apiKey != "" {
			profile.APIKey = apiKey
		} else {
			var username, password string
			if len(args) < 4 {
				_m, err := tea.NewProgram(credential.New()).Run()
				if err != nil {
					commandError = fmt.Errorf("error reading credentials: %s", err)
					cmd.Annotations["error"] = commandError.Error()
					return commandError
				}
				m := _m.(credential.Model)
				username, password = m.Values()
			} else {
				username = args[2]
				password = args[3]
			}
			profile.Username = username
			profile.Password = password
		}

		fileConfig, err := config.ReadConfigFromFile()
		if err != nil {
			newConfig := config.Config{
				Profiles:       map[string]config.Profile{name: profile},
				DefaultProfile: name,
			}
			err = config.WriteConfigToFile(&newConfig)
			commandError = err
		} else {
			if fileConfig.Profiles == nil {
				fileConfig.Profiles = make(map[string]config.Profile)
			}
			fileConfig.Profiles[name] = profile
			fileConfig.DefaultProfile = name
			commandError = config.WriteConfigToFile(fileConfig)
		}

		cmd.Annotations["executionTime"] = time.Since(startTime).String()
		if commandError != nil {
			cmd.Annotations["error"] = commandError.Error()
			return commandError
		}

		if outputFormat == "json" {
			return outputResult(safeProfileOutput(profile))
		}
		fmt.Printf("Profile %s added successfully\n", name)
		return nil
	},
}

var RemoveProfileCmd = &cobra.Command{
	Use:     "remove profile-name",
	Aliases: []string{"rm"},
	Example: "  pb profile remove local_parseable",
	Args:    cobra.ExactArgs(1),
	Short:   "Delete a profile",
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Annotations == nil {
			cmd.Annotations = make(map[string]string)
		}
		startTime := time.Now()

		name := args[0]
		fileConfig, err := config.ReadConfigFromFile()
		if err != nil {
			cmd.Annotations["error"] = fmt.Sprintf("error reading config: %s", err)
			return err
		}

		_, exists := fileConfig.Profiles[name]
		if !exists {
			msg := fmt.Sprintf("No profile found with the name: %s", name)
			cmd.Annotations["error"] = msg
			fmt.Println(msg)
			return nil
		}

		wasDefault := fileConfig.DefaultProfile == name
		delete(fileConfig.Profiles, name)

		if wasDefault {
			switch len(fileConfig.Profiles) {
			case 0:
				fileConfig.DefaultProfile = ""
			case 1:
				for k := range fileConfig.Profiles {
					fileConfig.DefaultProfile = k
					fmt.Printf("'%s' is now set as the default profile\n", k)
				}
			default:
				fmt.Println("Select a new default profile:")
				_m, err := tea.NewProgram(defaultprofile.New(fileConfig.Profiles)).Run()
				if err != nil {
					return fmt.Errorf("error selecting new default profile: %w", err)
				}
				m := _m.(defaultprofile.Model)
				if m.Success {
					fileConfig.DefaultProfile = m.Choice
					fmt.Printf("'%s' is now set as the default profile\n", m.Choice)
				} else {
					fileConfig.DefaultProfile = ""
				}
			}
		}

		commandError := config.WriteConfigToFile(fileConfig)
		cmd.Annotations["executionTime"] = time.Since(startTime).String()
		if commandError != nil {
			cmd.Annotations["error"] = commandError.Error()
			return commandError
		}

		if outputFormat == "json" {
			return outputResult(fmt.Sprintf("Deleted profile %s", name))
		}
		fmt.Printf("Deleted profile %s\n", name)
		return nil
	},
}

var DefaultProfileCmd = &cobra.Command{
	Use:     "default profile-name",
	Args:    cobra.MaximumNArgs(1),
	Short:   "Set default profile to use with all commands",
	Example: "  pb profile default local_parseable",
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Annotations == nil {
			cmd.Annotations = make(map[string]string)
		}
		startTime := time.Now()

		fileConfig, err := config.ReadConfigFromFile()
		if err != nil {
			cmd.Annotations["error"] = fmt.Sprintf("error reading config: %s", err)
			return err
		}

		var name string
		if len(args) > 0 {
			name = args[0]
		} else {
			model := defaultprofile.New(fileConfig.Profiles)
			_m, err := tea.NewProgram(model).Run()
			if err != nil {
				cmd.Annotations["error"] = fmt.Sprintf("error selecting default profile: %s", err)
				return err
			}
			m := _m.(defaultprofile.Model)
			if !m.Success {
				return nil
			}
			name = m.Choice
		}

		_, exists := fileConfig.Profiles[name]
		if !exists {
			commandError := fmt.Sprintf("profile %s does not exist", name)
			cmd.Annotations["error"] = commandError
			return errors.New(commandError)
		}

		fileConfig.DefaultProfile = name
		commandError := config.WriteConfigToFile(fileConfig)
		cmd.Annotations["executionTime"] = time.Since(startTime).String()
		if commandError != nil {
			cmd.Annotations["error"] = commandError.Error()
			return commandError
		}

		if outputFormat == "json" {
			return outputResult(fmt.Sprintf("%s is now set as default profile", name))
		}
		fmt.Printf("%s is now set as default profile\n", name)
		return nil
	},
}

var UpdateProfileCmd = &cobra.Command{
	Use:     "update profile-name new-url",
	Aliases: []string{"set-url"},
	Example: "  pb profile update local http://localhost:9000",
	Short:   "Update the URL of an existing profile",
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Annotations == nil {
			cmd.Annotations = make(map[string]string)
		}
		startTime := time.Now()

		name := args[0]
		rawURL := args[1]

		if _, err := url.Parse(rawURL); err != nil {
			return fmt.Errorf("invalid URL: %w", err)
		}

		fileConfig, err := config.ReadConfigFromFile()
		if err != nil {
			return fmt.Errorf("error reading config: %w", err)
		}

		profile, exists := fileConfig.Profiles[name]
		if !exists {
			return fmt.Errorf("no profile found with the name: %s", name)
		}

		profile.URL = rawURL
		fileConfig.Profiles[name] = profile

		commandError := config.WriteConfigToFile(fileConfig)
		cmd.Annotations["executionTime"] = time.Since(startTime).String()
		if commandError != nil {
			cmd.Annotations["error"] = commandError.Error()
			return commandError
		}

		if outputFormat == "json" {
			return outputResult(safeProfileOutput(profile))
		}
		fmt.Printf("Profile '%s' URL updated to %s\n", name, rawURL)
		return nil
	},
}

var ListProfileCmd = &cobra.Command{
	Use:     "list profiles",
	Aliases: []string{"ls"},
	Short:   "List all added profiles",
	Example: "  pb profile list",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if cmd.Annotations == nil {
			cmd.Annotations = make(map[string]string)
		}
		startTime := time.Now()

		fileConfig, err := config.ReadConfigFromFile()
		if err != nil {
			cmd.Annotations["error"] = fmt.Sprintf("error reading config: %s", err)
			return err
		}

		if outputFormat == "json" {
			commandError := outputResult(safeProfilesOutput(fileConfig.Profiles))
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
			if commandError != nil {
				cmd.Annotations["error"] = commandError.Error()
				return commandError
			}
			return nil
		}

		for key, value := range fileConfig.Profiles {
			item := ProfileListItem{key, value.URL, value.Username}
			fmt.Println(item.Render(fileConfig.DefaultProfile == key))
			fmt.Println() // Add a blank line after each profile
		}
		cmd.Annotations["executionTime"] = time.Since(startTime).String()
		return nil
	},
}

func Max(a int, b int) int {
	if a >= b {
		return a
	}
	return b
}
