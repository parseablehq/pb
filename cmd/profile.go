// Copyright (c) 2023 Cloudnatively Services Pvt Ltd
//
// This file is part of MinIO Object Storage stack
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
	"pb/pkg/model"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

type ProfileListItem struct {
	title, url, user string
}

func (item *ProfileListItem) Render(highlight bool) string {
	if highlight {
		render := fmt.Sprintf(
			"%s\n%s\n%s",
			selectedStyle.Render(item.title),
			selectedStyleAlt.Render(fmt.Sprintf("url: %s", item.url)),
			selectedStyleAlt.Render(fmt.Sprintf("user: %s", item.user)),
		)
		return selectedItemOuter.Render(render)
	} else {
		render := fmt.Sprintf(
			"%s\n%s\n%s",
			lipgloss.NewStyle().Render(item.title),
			inactiveStyle.Render(fmt.Sprintf("url: %s", item.url)),
			inactiveStyle.Render(fmt.Sprintf("user: %s", item.user)),
		)
		return itemOuter.Render(render)
	}
}

var AddProfileCmd = &cobra.Command{
	Use:     "add name url <username?> <password?>",
	Example: "add local_logs http://0.0.0.0:8000 admin admin",
	Short:   "Add a new profile",
	Args: func(cmd *cobra.Command, args []string) error {
		if err := cobra.MinimumNArgs(2)(cmd, args); err != nil {
			return err
		}
		if err := cobra.MaximumNArgs(4)(cmd, args); err != nil {
			return err
		}
		return nil
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
			_m, err := tea.NewProgram(model.NewPromptModel()).Run()
			if err != nil {
				fmt.Printf("Alas, there's been an error: %v", err)
				os.Exit(1)
			}
			m := _m.(model.ProfilePrompt)

			username, password = m.Values()
		} else {
			username = args[2]
			password = args[3]
		}

		profile := config.Profile{
			Url:      url.String(),
			Username: username,
			Password: password,
		}

		file_config, err := config.ReadConfigFromFile()

		if err != nil {
			// create new file
			new_config := config.Config{
				Profiles: map[string]config.Profile{
					name: profile,
				},
				Default_profile: name,
			}
			err = config.WriteConfigToFile(&new_config)
			return err
		} else {
			if file_config.Profiles == nil {
				file_config.Profiles = make(map[string]config.Profile)
			}
			file_config.Profiles[name] = profile
			if file_config.Default_profile == "" {
				file_config.Default_profile = name
			}
			config.WriteConfigToFile(file_config)
		}

		return nil
	},
}

var DeleteProfileCmd = &cobra.Command{
	Use:   "delete name",
	Args:  cobra.ExactArgs(1),
	Short: "Delete a profile",
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		file_config, err := config.ReadConfigFromFile()
		if err != nil {
			return nil
		}

		_, exists := file_config.Profiles[name]
		if exists {
			delete(file_config.Profiles, name)
			if len(file_config.Profiles) == 0 {
				file_config.Default_profile = ""
			}
			config.WriteConfigToFile(file_config)
			fmt.Printf("Deleted profile %s\n", styleBold.Render(name))
		} else {
			fmt.Printf("No profile found with the name: %s", styleBold.Render(name))
		}

		return nil
	},
}

var DefaultProfileCmd = &cobra.Command{
	Use:   "default name",
	Args:  cobra.ExactArgs(1),
	Short: "Set default profile to use",
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		file_config, err := config.ReadConfigFromFile()
		if err != nil {
			return nil
		} else {
			_, exists := file_config.Profiles[name]
			if exists {
				file_config.Default_profile = name
			} else {
				name = lipgloss.NewStyle().Bold(true).Render(name)
				err := fmt.Sprintf("profile %s does not exist", styleBold.Render(name))
				return errors.New(err)
			}
		}

		config.WriteConfigToFile(file_config)
		fmt.Printf("%s is now set as default profile", styleBold.Render(name))
		return nil
	},
}

var ListProfileCmd = &cobra.Command{
	Use:   "list",
	Short: "List all added profiles",
	RunE: func(cmd *cobra.Command, args []string) error {
		file_config, err := config.ReadConfigFromFile()
		if err != nil {
			return nil
		}

		if len(file_config.Profiles) != 0 {
			println()
		}

		row := 0
		for key, value := range file_config.Profiles {
			item := ProfileListItem{key, value.Url, value.Username}
			fmt.Println(item.Render(file_config.Default_profile == key))
			row += 1
			fmt.Println()
		}
		return nil
	},
}

func Max(a int, b int) int {
	if a >= b {
		return a
	} else {
		return b
	}
}
