package config

import (
	"errors"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/shibukawa/configdir"
)

type Config struct {
	Profiles        map[string]Profile
	Default_profile string
}

type Profile struct {
	Url      string
	Username string
	Password string
}

func ConfigDir() configdir.ConfigDir {
	return configdir.New("parseable", "pb")
}

func WriteConfigToFile(config *Config, filename string) error {
	tomlData, _ := toml.Marshal(config)

	// Stores to user folder
	folders := ConfigDir().QueryFolders(configdir.Global)
	err := folders[0].WriteFile("config.toml", tomlData)

	return err
}

func ReadConfigFromFile(filename string) (*Config, error) {
	var config Config

	folder := ConfigDir().QueryFolderContainsFile("config.toml")
	if folder != nil {
		data, _ := folder.ReadFile("config.toml")
		toml.Unmarshal(data, &config)
		return &config, nil
	} else {
		return nil, errors.New("Config file not found")
	}
}
