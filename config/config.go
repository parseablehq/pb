package config

import (
	"fmt"
	"os"
	path "path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

var (
	ConfigFilename = "config.toml"
	ConfigAppName  = "parseable"
)

func ConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return path.Join(dir, ConfigAppName, ConfigFilename), nil
}

type Config struct {
	Profiles        map[string]Profile
	Default_profile string
}

type Profile struct {
	Url      string
	Username string
	Password string
}

func WriteConfigToFile(config *Config) error {
	tomlData, _ := toml.Marshal(config)
	filePath, err := ConfigPath()
	if err != nil {
		return err
	}
	// Open or create the file for writing (it will truncate the file if it already exists
	err = os.MkdirAll(path.Dir(filePath), os.ModePerm)
	if err != nil {
		return err
	}

	file, err := os.Create(filePath)
	if err != nil {
		fmt.Println("Error creating the file:", err)
		return err
	}
	defer file.Close()
	// Write the data into the file
	_, err = file.Write(tomlData)
	if err != nil {
		fmt.Println("Error writing to the file:", err)
		return err
	}
	return err
}

func ReadConfigFromFile() (config *Config, err error) {
	filePath, err := ConfigPath()
	if err != nil {
		return
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}
	toml.Unmarshal(data, &config)
	return
}
