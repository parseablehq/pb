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
	"pb/pkg/config"
	"pb/pkg/model/credential"
	"pb/pkg/model/defaultprofile"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// ProfileListItem is a struct to hold the profile list items
type ProfileListItem struct {
	title, url, user string
}

func (item *ProfileListItem) Render(highlight bool) string {
	if highlight {
		render := fmt.Sprintf(
			"%s\n%s\n%s",
			SelectedStyle.Render(item.title),
			SelectedStyleAlt.Render(fmt.Sprintf("url: %s", item.url)),
			SelectedStyleAlt.Render(fmt.Sprintf("user: %s", item.user)),
		)
		return SelectedItemOuter.Render(render)
	}
	render := fmt.Sprintf(
		"%s\n%s\n%s",
		StandardStyle.Render(item.title),
		StandardStyleAlt.Render(fmt.Sprintf("url: %s", item.url)),
		StandardStyleAlt.Render(fmt.Sprintf("user: %s", item.user)),
	)
	return ItemOuter.Render(render)
}

// Add an output flag to specify the output format.
var outputFormat string

// Initialize flags
func init() {
	AddProfileCmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (text|json)")
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
	Use:     "add profile-name url <username?> <password?>",
	Example: "  pb profile add local_parseable http://0.0.0.0:8000 admin admin",
	Short:   "Add a new profile",
	Long:    "Add a new profile to the config file",
	Args: func(cmd *cobra.Command, args []string) error {
		if err := cobra.MinimumNArgs(2)(cmd, args); err != nil {
			return err
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

		profile := config.Profile{URL: url.String(), Username: username, Password: password}
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
			if fileConfig.DefaultProfile == "" {
				fileConfig.DefaultProfile = name
			}
			commandError = config.WriteConfigToFile(fileConfig)
		}

		cmd.Annotations["executionTime"] = time.Since(startTime).String()
		if commandError != nil {
			cmd.Annotations["error"] = commandError.Error()
			return commandError
		}

		if outputFormat == "json" {
			return outputResult(profile)
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

		delete(fileConfig.Profiles, name)
		if len(fileConfig.Profiles) == 0 {
			fileConfig.DefaultProfile = ""
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

var ListProfileCmd = &cobra.Command{
	Use:     "list profiles",
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
			commandError := outputResult(fileConfig.Profiles)
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
