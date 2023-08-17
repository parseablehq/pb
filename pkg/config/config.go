// Copyright (c) 2023 Cloudnatively Services Pvt Ltd
//
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

package config

import (
	"fmt"
	"os"
	path "path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

var (
	configFilename = "config.toml"
	configAppName  = "parseable"
)

func ConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return path.Join(dir, configAppName, configFilename), nil
}

// Config is the struct that holds the configuration
type Config struct {
	Profiles       map[string]Profile
	DefaultProfile string
}

// Profile is the struct that holds the profile configuration
type Profile struct {
	URL      string
	Username string
	Password string
}

// WriteConfigToFile writes the configuration to the config file
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

// ReadConfigFromFile reads the configuration from the config file
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
