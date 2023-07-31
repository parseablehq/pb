package main

import (
	"cli/config"
	"cli/model"
	"errors"
	"fmt"
	"net/url"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

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
		} else {
			_, exists := file_config.Profiles[name]
			if exists {
				delete(file_config.Profiles, name)
			}
		}

		if len(file_config.Profiles) == 0 {
			file_config.Default_profile = ""
		}

		config.WriteConfigToFile(file_config)
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
				err := fmt.Sprintf("profile %s does not exist", name)
				return errors.New(err)
			}
		}

		config.WriteConfigToFile(file_config)
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
		} else {
			for key, value := range file_config.Profiles {
				fmt.Println(key, value.Url, value.Username)
			}
		}
		return nil
	},
}

var QueryProfileCmd = &cobra.Command{
	Use:   "query",
	Short: "Open Query TUI",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		config, err := config.ReadConfigFromFile()
		if err != nil {
			return err
		}

		stream := args[0]

		if config.Default_profile == "" {
			fmt.Println("No Profile Set. Use profile add command to create new profile to use")
			return nil
		}

		p := tea.NewProgram(model.NewQueryModel(config.Profiles[config.Default_profile], stream), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Printf("Alas, there's been an error: %v", err)
			os.Exit(1)
		}
		return nil
	},
}
