package cmd

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
	Run: func(cmd *cobra.Command, _ []string) {
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
