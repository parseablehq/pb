package cmd

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

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"pb/pkg/installer"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

// ListOssCmd lists the Parseable OSS servers
var ListOssCmd = &cobra.Command{
	Use:     "oss",
	Short:   "List available Parseable OSS servers",
	Example: "pb list oss",
	Run: func(cmd *cobra.Command, _ []string) {
		// Read the installer file
		entries, err := readInstallerFile()
		if err != nil {
			log.Fatalf("Failed to list OSS servers: %v", err)
		}

		// Check if there are no entries
		if len(entries) == 0 {
			fmt.Println("No OSS servers found.")
			return
		}

		// Display the entries in a table format
		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"Name", "Namespace", "Version", "Status"})

		for _, entry := range entries {
			table.Append([]string{entry.Name, entry.Namespace, entry.Version, entry.Status})
		}

		table.Render()
	},
}

// readInstallerFile reads and parses the installer.yaml file
func readInstallerFile() ([]installer.InstallerEntry, error) {
	// Define the file path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}
	filePath := filepath.Join(homeDir, ".parseable", "installer.yaml")

	// Check if the file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("installer file not found at %s", filePath)
	}

	// Read and parse the file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read installer file: %w", err)
	}

	var entries []installer.InstallerEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("failed to parse installer file: %w", err)
	}

	return entries, nil
}
