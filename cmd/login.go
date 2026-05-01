// Copyright (c) 2024 Parseable, Inc
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
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"pb/pkg/config"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const cloudURL = "https://app.parseable.com"

var (
	loginToken       string
	loginURL         string
	loginUsername    string
	loginPassword    string
	loginProfileName string
)

func init() {
	LoginCmd.Flags().StringVar(&loginToken, "token", "", "Auth token for cloud login")
	LoginCmd.Flags().StringVar(&loginURL, "url", "", "Server URL for self-hosted Parseable")
	LoginCmd.Flags().StringVar(&loginUsername, "username", "", "Username for self-hosted login")
	LoginCmd.Flags().StringVar(&loginPassword, "password", "", "Password for self-hosted login")
	LoginCmd.Flags().StringVar(&loginProfileName, "profile", "default", "Profile name to save as")
}

var LoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to Parseable",
	Long: `Login to Parseable cloud or a self-hosted instance.

Cloud login (opens browser):
  pb login

Cloud login with token:
  pb login --token <token>

Self-hosted login:
  pb login --url http://localhost:8000 --username admin --password admin`,
	RunE: func(_ *cobra.Command, _ []string) error {
		// --- Self-hosted path ---
		if loginURL != "" {
			return selfHostedLogin()
		}

		// --- Cloud path ---
		return cloudLogin()
	},
}

func selfHostedLogin() error {
	username := loginUsername
	password := loginPassword

	if username == "" {
		fmt.Print("Username: ")
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read username: %w", err)
		}
		username = strings.TrimSpace(line)
	}

	if password == "" {
		fmt.Print("Password: ")
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}
		password = strings.TrimSpace(line)
	}

	if username == "" || password == "" {
		return fmt.Errorf("username and password are required for self-hosted login")
	}

	profile := config.Profile{
		URL:      loginURL,
		Username: username,
		Password: password,
	}
	if err := writeProfile(profile, loginProfileName); err != nil {
		return fmt.Errorf("failed to save profile: %w", err)
	}

	fmt.Printf("✓ Logged in. Profile '%s' saved.\n", loginProfileName)
	fmt.Printf("  URL: %s\n", loginURL)
	return nil
}

func cloudLogin() error {
	token := loginToken

	if token == "" {
		loginPageURL := cloudURL + "/login"
		fmt.Printf("Opening login page: %s\n\n", loginPageURL)

		if err := openBrowser(loginPageURL); err != nil {
			fmt.Println("Could not open browser automatically. Please visit the URL above and copy your token.")
		} else {
			fmt.Println("Browser opened. After logging in, copy your token from the dashboard.")
		}

		fmt.Print("\nPaste your token here: ")
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read token: %w", err)
		}
		token = strings.TrimSpace(line)
		if token == "" {
			return fmt.Errorf("no token provided, login canceled")
		}
	}

	profile := config.Profile{
		URL:   cloudURL,
		Token: token,
	}
	if err := writeProfile(profile, loginProfileName); err != nil {
		return fmt.Errorf("failed to save profile: %w", err)
	}

	fmt.Printf("✓ Logged in. Profile '%s' saved.\n", loginProfileName)
	fmt.Printf("  URL: %s\n", cloudURL)
	return nil
}

func writeProfile(profile config.Profile, profileName string) error {
	fileConfig, err := config.ReadConfigFromFile()
	if err != nil {
		newConfig := config.Config{
			Profiles:       map[string]config.Profile{profileName: profile},
			DefaultProfile: profileName,
		}
		return config.WriteConfigToFile(&newConfig)
	}

	if fileConfig.Profiles == nil {
		fileConfig.Profiles = make(map[string]config.Profile)
	}
	fileConfig.Profiles[profileName] = profile
	if fileConfig.DefaultProfile == "" {
		fileConfig.DefaultProfile = profileName
	}
	return config.WriteConfigToFile(fileConfig)
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return cmd.Start()
}
