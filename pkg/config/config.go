// Copyright (c) 2024 Parseable, Inc
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
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	path "path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

var (
	configFilename = "config.toml"
	configAppName  = "parseable"
)

// Path returns user directory that can be used for the config file
func Path() (string, error) {
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

func (p *Profile) GrpcAddr(port string) string {
	urlv, _ := url.Parse(p.URL)
	return net.JoinHostPort(urlv.Hostname(), port)
}

// WriteConfigToFile writes the configuration to the config file
func WriteConfigToFile(config *Config) error {
	tomlData, _ := toml.Marshal(config)
	filePath, err := Path()
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
	filePath, err := Path()
	if err != nil {
		return &Config{}, err
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return &Config{}, err
	}

	err = toml.Unmarshal(data, &config)
	if err != nil {
		return &Config{}, err
	}

	return config, nil
}

func GetProfile() (Profile, error) {
	conf, err := ReadConfigFromFile()
	if os.IsNotExist(err) {
		return Profile{}, errors.New("no config found to run this command. add a profile using pb profile command")
	} else if err != nil {
		return Profile{}, err
	}

	if conf.Profiles == nil || conf.DefaultProfile == "" {
		return Profile{}, errors.New("no profile is configured to run this command. please create one using profile command")
	}

	return conf.Profiles[conf.DefaultProfile], nil

}
