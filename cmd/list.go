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

	"pb/pkg/common"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// ListOssCmd lists the Parseable OSS servers
var ListOssCmd = &cobra.Command{
	Use:     "oss",
	Short:   "List available Parseable OSS servers",
	Example: "pb list oss",
	Run: func(_ *cobra.Command, _ []string) {
		_, err := common.PromptK8sContext()
		if err != nil {
			log.Fatalf("Failed to prompt for kubernetes context: %v", err)
		}

		// Read the installer data from the ConfigMap
		entries, err := common.ReadInstallerConfigMap()
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
