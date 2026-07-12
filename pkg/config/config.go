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
	"runtime"

	toml "github.com/pelletier/go-toml/v2"
)

var (
	configFilename = "config.toml"
	configAppName  = "pb"
)

// Path returns the config file path.
// On Windows: %AppData%\pb\config.toml
// On macOS/Linux: ~/.config/pb/config.toml (XDG style)
func Path() (string, error) {
	var dir string
	if runtime.GOOS == "windows" {
		appData, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		dir = appData
	} else {
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			dir = xdg
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			dir = path.Join(home, ".config")
		}
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
	URL      string `toml:"url" json:"url"`
	Username string `toml:"username,omitempty" json:"username,omitempty"`
	Password string `toml:"password,omitempty" json:"password,omitempty"`

	Cloud           bool   `toml:"cloud" json:"cloud"`
	APIKey          string `toml:"apiKey,omitempty" json:"apiKey,omitempty"`
	SessionToken    string `toml:"session_token,omitempty" json:"session_token,omitempty"`
	RefreshToken    string `toml:"refresh_token,omitempty" json:"refresh_token,omitempty"`
	TenantID        string `toml:"tenant_id,omitempty" json:"tenant_id,omitempty"`
	IngestURL       string `toml:"ingest_url,omitempty" json:"ingest_url,omitempty"`
	WorkspaceID     string `toml:"workspace_id,omitempty" json:"workspace_id,omitempty"`
	WorkspaceName   string `toml:"workspace_name,omitempty" json:"workspace_name,omitempty"`
	OrchestratorURL string `toml:"orchestrator_url,omitempty" json:"orchestrator_url,omitempty"`
	ClerkSessionID  string `toml:"clerk_session_id,omitempty" json:"clerk_session_id,omitempty"`
}

type AuthMode string

const (
	// AuthSelfHostedBasic identifies username/password authentication.
	AuthSelfHostedBasic AuthMode = "self_hosted_basic"
	// AuthSelfHostedAPIKey identifies API-key authentication for a self-hosted server.
	AuthSelfHostedAPIKey AuthMode = "self_hosted_api_key"
	// AuthCloudAPIKey identifies API-key authentication for Parseable Cloud.
	AuthCloudAPIKey AuthMode = "cloud_api_key"
	// AuthCloudOAuth identifies browser-based OAuth authentication for Parseable Cloud.
	AuthCloudOAuth AuthMode = "cloud_oauth"
)

func (p Profile) AuthMode() (AuthMode, error) {
	hasBasic := p.Username != "" || p.Password != ""
	hasAPIKey := p.APIKey != ""
	hasCloudOAuth := p.SessionToken != ""

	if p.Cloud {
		if p.TenantID == "" {
			return "", errors.New("cloud profile missing tenant_id")
		}
		if hasBasic {
			return "", errors.New("cloud profile cannot use self-hosted credentials")
		}
		switch {
		case hasAPIKey && !hasCloudOAuth:
			return AuthCloudAPIKey, nil
		case hasCloudOAuth && !hasAPIKey:
			return AuthCloudOAuth, nil
		case hasAPIKey && hasCloudOAuth:
			return "", errors.New("cloud profile has both apiKey and session_token")
		default:
			return "", errors.New("cloud profile missing apiKey or session_token")
		}
	}

	if hasCloudOAuth || p.TenantID != "" {
		return "", errors.New("self-hosted profile cannot use cloud credentials")
	}
	switch {
	case hasBasic && !hasAPIKey:
		if p.Username == "" || p.Password == "" {
			return "", errors.New("self-hosted basic profile missing username or password")
		}
		return AuthSelfHostedBasic, nil
	case hasAPIKey && !hasBasic:
		return AuthSelfHostedAPIKey, nil
	case hasBasic && hasAPIKey:
		return "", errors.New("self-hosted profile has both basic credentials and api key")
	default:
		return "", errors.New("self-hosted profile missing credentials")
	}
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

	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		fmt.Println("Error creating the file:", err)
		return err
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		fmt.Println("Error setting file permissions:", err)
		return err
	}
	// Write the data into the file
	_, err = file.Write(tomlData)
	if err != nil {
		fmt.Println("Error writing to the file:", err)
		return err
	}
	return err
}

// ReadConfigFromFile reads the configuration from the config file
func ReadConfigFromFile() (*Config, error) {
	filePath, err := Path()
	if err != nil {
		return &Config{}, err
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return &Config{}, err
	}

	var config Config
	if err := toml.Unmarshal(data, &config); err != nil {
		return &Config{}, err
	}
	if config.Profiles == nil {
		config.Profiles = make(map[string]Profile)
	}
	applyLegacyAPIKeyConfig(data, &config)

	return &config, nil
}

func applyLegacyAPIKeyConfig(data []byte, config *Config) {
	type legacyProfile struct {
		Token  string `toml:"token,omitempty"`
		APIKey string `toml:"api_key,omitempty"`
	}
	type legacyConfig struct {
		Profiles map[string]legacyProfile
	}

	var legacy legacyConfig
	if err := toml.Unmarshal(data, &legacy); err != nil {
		return
	}
	for name, legacyProfile := range legacy.Profiles {
		profile := config.Profiles[name]
		if profile.APIKey == "" {
			profile.APIKey = legacyProfile.APIKey
		}
		if profile.APIKey == "" {
			profile.APIKey = legacyProfile.Token
		}
		if profile.APIKey != "" {
			config.Profiles[name] = profile
		}
	}
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
