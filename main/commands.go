package main

import (
	"config"
	"fmt"
	"model"
	"os"

	"github.com/alecthomas/kong"
	tea "github.com/charmbracelet/bubbletea"
)

type AddProfileCmd struct {
	Name     string `arg required type:"string"`
	Url      string `arg required type:"string"`
	Username string `arg optional type:"string"`
	Password string `arg optional type:"string"`
}

func (cmd *AddProfileCmd) Run(ctx *kong.Context) error {
	username := cmd.Username
	password := cmd.Password

	if username == "" || password == "" {
		_m, err := tea.NewProgram(model.NewPromptModel()).Run()
		if err != nil {
			fmt.Printf("Alas, there's been an error: %v", err)
			os.Exit(1)
		}
		m := _m.(model.ProfilePrompt)

		username, password = m.Values()
	}

	// If prompt is terminated without valid input then do nothing
	if username == "" || password == "" {
		return nil
	}

	profile := config.Profile{
		Url:      cmd.Url,
		Username: cmd.Username,
		Password: cmd.Password,
	}

	file_config, err := config.ReadConfigFromFile("config.toml")

	if err != nil {
		// create new file
		new_config := config.Config{
			Profiles: map[string]config.Profile{
				cmd.Name: profile,
			},
			Default_profile: cmd.Name,
		}
		config.WriteConfigToFile(&new_config, "config.toml")
		global_profile = profile
		return nil
	} else {
		file_config.Profiles[cmd.Name] = profile
		if file_config.Default_profile == "" {
			file_config.Default_profile = cmd.Name
		}
		config.WriteConfigToFile(file_config, "config.toml")
	}

	return nil
}

type DeleteProfileCmd struct {
	Name string `arg required type:"string"`
}

func (cmd *DeleteProfileCmd) Run(ctx *kong.Context) error {
	file_config, err := config.ReadConfigFromFile("config.toml")
	if err != nil {
		return nil
	} else {
		_, exists := file_config.Profiles[cmd.Name]
		if exists {
			delete(file_config.Profiles, cmd.Name)
		}
	}

	if len(file_config.Profiles) == 0 {
		file_config.Default_profile = ""
	}

	config.WriteConfigToFile(file_config, "config.toml")
	return nil
}

type DefaultProfileCmd struct {
	Name string `arg required type:"string"`
}

func (cmd *DefaultProfileCmd) Run(ctx *kong.Context) error {
	file_config, err := config.ReadConfigFromFile("config.toml")
	if err != nil {
		return nil
	} else {
		_, exists := file_config.Profiles[cmd.Name]
		if exists {
			file_config.Default_profile = cmd.Name
		}
	}

	config.WriteConfigToFile(file_config, "config.toml")
	return nil
}

type ListProfileCmd struct {
}

func (cmd *ListProfileCmd) Run(ctx *kong.Context) error {
	file_config, err := config.ReadConfigFromFile("config.toml")
	if err != nil {
		return nil
	} else {
		for key, value := range file_config.Profiles {
			fmt.Println(key, value.Url, value.Username)
		}
	}
	return nil
}

type QueryCmd struct {
	Stream string `arg required type:"string"`
}

func (cmd *QueryCmd) Run(ctx *kong.Context) error {

	if global_profile.Url == "" {
		fmt.Println("No Profile Set. Use profile add command to create new profile to use")
		return nil
	}

	p := tea.NewProgram(model.NewQueryModel(global_profile, cmd.Stream), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
	return nil
}
