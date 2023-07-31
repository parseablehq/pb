package main

import (
	"cli/config"
	"cli/model"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var AddProfileCmd = &cobra.Command{
	Use: "add name url <username?> <password?>",
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
		url := args[1]

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
			username = args[3]
			password = args[4]
		}

		profile := config.Profile{
			Url:      url,
			Username: username,
			Password: password,
		}

		file_config, err := config.ReadConfigFromFile("config.toml")

		if err != nil {
			// create new file
			new_config := config.Config{
				Profiles: map[string]config.Profile{
					name: profile,
				},
				Default_profile: name,
			}
			err = config.WriteConfigToFile(&new_config, "config.toml")
			global_profile = profile
			return err
		} else {
			if file_config.Profiles == nil {
				file_config.Profiles = make(map[string]config.Profile)
			}
			file_config.Profiles[name] = profile
			if file_config.Default_profile == "" {
				file_config.Default_profile = name
			}
			config.WriteConfigToFile(file_config, "config.toml")
		}

		return nil
	},
}

var DeleteProfileCmd = &cobra.Command{
	Use:  "delete name",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		file_config, err := config.ReadConfigFromFile("config.toml")
		if err != nil {
			return nil
		} else {
			_, exists := file_config.Profiles[name]
			if exists {
				delete(file_config.Profiles, name)
			}
		}

		if len(file_config.Profiles) == 0 {
			file_config.Default_profile = ""
		}

		config.WriteConfigToFile(file_config, "config.toml")
		return nil
	},
}

var DefaultProfileCmd = &cobra.Command{
	Use:  "default name",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		file_config, err := config.ReadConfigFromFile("config.toml")
		if err != nil {
			return nil
		} else {
			_, exists := file_config.Profiles[name]
			if exists {
				file_config.Default_profile = name
			}
		}

		config.WriteConfigToFile(file_config, "config.toml")
		return nil
	},
}

var ListProfileCmd = &cobra.Command{
	Use: "list",
	RunE: func(cmd *cobra.Command, args []string) error {
		file_config, err := config.ReadConfigFromFile("config.toml")
		if err != nil {
			return nil
		} else {
			for key, value := range file_config.Profiles {
				fmt.Println(key, value.Url, value.Username)
			}
		}
		return nil
	},
}

var QueryProfileCmd = &cobra.Command{
	Use:  "query name",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		stream := args[0]

		if global_profile.Url == "" {
			fmt.Println("No Profile Set. Use profile add command to create new profile to use")
			return nil
		}

		p := tea.NewProgram(model.NewQueryModel(global_profile, stream), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Printf("Alas, there's been an error: %v", err)
			os.Exit(1)
		}
		return nil
	},
}
