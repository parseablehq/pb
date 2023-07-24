package config

import (
	"os"

	toml "github.com/pelletier/go-toml/v2"
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

func WriteConfigToFile(config *Config, filename string) error {

	tomlData, err := toml.Marshal(config)
	if err != nil {
		return err
	}

	err = os.WriteFile(filename, tomlData, 0644)
	if err != nil {
		return err
	}

	return nil
}

func ReadConfigFromFile(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config Config
	err = toml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}
