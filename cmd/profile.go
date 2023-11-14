// Copyright (c) 2023 Cloudnatively Services Pvt Ltd
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
	"errors"
	"fmt"
	"net/url"
	"os"

	"pb/pkg/config"
	"pb/pkg/model/credential"
	"pb/pkg/model/defaultprofile"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
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
		name := args[0]
		url, err := url.Parse(args[1])
		if err != nil {
			return err
		}

		var username string
		var password string

		if len(args) < 4 {
			_m, err := tea.NewProgram(credential.New()).Run()
			if err != nil {
				fmt.Printf("Alas, there's been an error: %v", err)
				os.Exit(1)
			}
			m := _m.(credential.Model)

			username, password = m.Values()
		} else {
			username = args[2]
			password = args[3]
		}

		profile := config.Profile{
			URL:      url.String(),
			Username: username,
			Password: password,
		}

		fileConfig, err := config.ReadConfigFromFile()
		if err != nil {
			// create new file
			newConfig := config.Config{
				Profiles: map[string]config.Profile{
					name: profile,
				},
				DefaultProfile: name,
			}
			err = config.WriteConfigToFile(&newConfig)
			return err
		}
		if fileConfig.Profiles == nil {
			fileConfig.Profiles = make(map[string]config.Profile)
		}
		fileConfig.Profiles[name] = profile
		if fileConfig.DefaultProfile == "" {
			fileConfig.DefaultProfile = name
		}
		config.WriteConfigToFile(fileConfig)

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
		name := args[0]
		fileConfig, err := config.ReadConfigFromFile()
		if err != nil {
			return nil
		}

		_, exists := fileConfig.Profiles[name]
		if exists {
			delete(fileConfig.Profiles, name)
			if len(fileConfig.Profiles) == 0 {
				fileConfig.DefaultProfile = ""
			}
			config.WriteConfigToFile(fileConfig)
			fmt.Printf("Deleted profile %s\n", StyleBold.Render(name))
		} else {
			fmt.Printf("No profile found with the name: %s", StyleBold.Render(name))
		}

		return nil
	},
}

var DefaultProfileCmd = &cobra.Command{
	Use:     "default profile-name",
	Args:    cobra.MaximumNArgs(1),
	Short:   "Set default profile to use with all commands",
	Example: "  pb profile default local_parseable",
	RunE: func(cmd *cobra.Command, args []string) error {
		var name string

		fileConfig, err := config.ReadConfigFromFile()
		if err != nil {
			return nil
		}

		if len(args) > 0 {
			name = args[0]
		} else {
			model := defaultprofile.New(fileConfig.Profiles)
			_m, err := tea.NewProgram(model).Run()
			if err != nil {
				fmt.Printf("Alas, there's been an error: %v", err)
				os.Exit(1)
			}
			m := _m.(defaultprofile.Model)
			termenv.DefaultOutput().ClearLines(lipgloss.Height(model.View()) - 1)
			if m.Success {
				name = m.Choice
			} else {
				return nil
			}
		}

		_, exists := fileConfig.Profiles[name]
		if exists {
			fileConfig.DefaultProfile = name
		} else {
			name = lipgloss.NewStyle().Bold(true).Render(name)
			err := fmt.Sprintf("profile %s does not exist", StyleBold.Render(name))
			return errors.New(err)
		}

		config.WriteConfigToFile(fileConfig)
		fmt.Printf("%s is now set as default profile\n", StyleBold.Render(name))
		return nil
	},
}

var ListProfileCmd = &cobra.Command{
	Use:     "list profiles",
	Short:   "List all added profiles",
	Example: "  pb profile list",
	RunE: func(cmd *cobra.Command, args []string) error {
		fileConfig, err := config.ReadConfigFromFile()
		if err != nil {
			return nil
		}

		if len(fileConfig.Profiles) != 0 {
			println()
		}

		row := 0
		for key, value := range fileConfig.Profiles {
			item := ProfileListItem{key, value.URL, value.Username}
			fmt.Println(item.Render(fileConfig.DefaultProfile == key))
			row++
			fmt.Println()
		}
		return nil
	},
}

func Max(a int, b int) int {
	if a >= b {
		return a
	}
	return b
}
