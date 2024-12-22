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

	"pb/pkg/common"
	"pb/pkg/helm"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

// ShowValuesCmd lists the Parseable OSS servers
var ShowValuesCmd = &cobra.Command{
	Use:     "values",
	Short:   "Show values available in Parseable OSS servers",
	Example: "pb show values",
	Run: func(_ *cobra.Command, _ []string) {
		_, err := common.PromptK8sContext()
		if err != nil {
			log.Fatalf("Failed to prompt for Kubernetes context: %v", err)
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

		// Prompt user to select a cluster
		selectedCluster, err := common.PromptClusterSelection(entries)
		if err != nil {
			log.Fatalf("Failed to select a cluster: %v", err)
		}

		values, err := helm.GetReleaseValues(selectedCluster.Name, selectedCluster.Namespace)
		if err != nil {
			log.Fatalf("Failed to get values for release: %v", err)
		}

		// Marshal values to YAML for nice formatting
		yamlOutput, err := yaml.Marshal(values)
		if err != nil {
			log.Fatalf("Failed to marshal values to YAML: %v", err)
		}

		// Print the YAML output
		fmt.Println(string(yamlOutput))

		// Print instructions for fetching secret values
		fmt.Printf("\nTo get secret values of the Parseable cluster, run the following command:\n")
		fmt.Printf("kubectl get secret -n %s parseable-env-secret -o jsonpath='{.data}' | jq -r 'to_entries[] | \"\\(.key): \\(.value | @base64d)\"'\n", selectedCluster.Namespace)
	},
}
