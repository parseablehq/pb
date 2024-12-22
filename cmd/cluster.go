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
	"context"
	"fmt"
	"log"
	"os"
	"pb/pkg/common"
	"pb/pkg/helm"
	"pb/pkg/installer"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var verbose bool

var InstallOssCmd = &cobra.Command{
	Use:     "install",
	Short:   "Deploy Parseable",
	Example: "pb cluster install",
	Run: func(cmd *cobra.Command, _ []string) {
		// Add verbose flag
		cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
		installer.Installer(verbose)
	},
}

// ListOssCmd lists the Parseable OSS servers
var ListOssCmd = &cobra.Command{
	Use:     "list",
	Short:   "List available Parseable servers",
	Example: "pb list",
	Run: func(_ *cobra.Command, _ []string) {
		_, err := common.PromptK8sContext()
		if err != nil {
			log.Fatalf("Failed to prompt for kubernetes context: %v", err)
		}

		// Read the installer data from the ConfigMap
		entries, err := common.ReadInstallerConfigMap()
		if err != nil {
			log.Fatalf("Failed to list servers: %v", err)
		}

		// Check if there are no entries
		if len(entries) == 0 {
			fmt.Println("No clusters found.")
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

// ShowValuesCmd lists the Parseable OSS servers
var ShowValuesCmd = &cobra.Command{
	Use:     "show values",
	Short:   "Show values available in Parseable servers",
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

// UninstallOssCmd removes Parseable OSS servers
var UninstallOssCmd = &cobra.Command{
	Use:     "uninstall",
	Short:   "Uninstall Parseable servers",
	Example: "pb uninstall",
	Run: func(_ *cobra.Command, _ []string) {
		_, err := common.PromptK8sContext()
		if err != nil {
			log.Fatalf("Failed to prompt for Kubernetes context: %v", err)
		}

		// Read the installer data from the ConfigMap
		entries, err := common.ReadInstallerConfigMap()
		if err != nil {
			log.Fatalf("Failed to fetch OSS servers: %v", err)
		}

		// Check if there are no entries
		if len(entries) == 0 {
			fmt.Println(common.Yellow + "\nNo Parseable OSS servers found to uninstall.")
			return
		}

		// Prompt user to select a cluster
		selectedCluster, err := common.PromptClusterSelection(entries)
		if err != nil {
			log.Fatalf("Failed to select a cluster: %v", err)
		}

		// Display a warning banner
		fmt.Println("\n────────────────────────────────────────────────────────────────────────────")
		fmt.Println("⚠️  Deleting this cluster will not delete any data on object storage.")
		fmt.Println("   This operation will clean up the Parseable deployment on Kubernetes.")
		fmt.Println("────────────────────────────────────────────────────────────────────────────")

		// Confirm uninstallation
		fmt.Printf("\nYou have selected to uninstall the cluster '%s' in namespace '%s'.\n", selectedCluster.Name, selectedCluster.Namespace)
		if !common.PromptConfirmation(fmt.Sprintf("Do you want to proceed with uninstalling '%s'?", selectedCluster.Name)) {
			fmt.Println(common.Yellow + "Uninstall operation canceled.")
			return
		}

		//Perform uninstallation
		if err := uninstallCluster(selectedCluster); err != nil {
			log.Fatalf("Failed to uninstall cluster: %v", err)
		}

		// Remove entry from ConfigMap
		if err := common.RemoveInstallerEntry(selectedCluster.Name); err != nil {
			log.Fatalf("Failed to remove entry from ConfigMap: %v", err)
		}

		// Delete secret
		if err := deleteSecret(selectedCluster.Namespace, "parseable-env-secret"); err != nil {
			log.Printf("Warning: Failed to delete secret 'parseable-env-secret': %v", err)
		} else {
			fmt.Println(common.Green + "Secret 'parseable-env-secret' deleted successfully." + common.Reset)
		}

		fmt.Println(common.Green + "Uninstallation completed successfully." + common.Reset)
	},
}

func uninstallCluster(entry common.InstallerEntry) error {
	helmApp := helm.Helm{
		ReleaseName: entry.Name,
		Namespace:   entry.Namespace,
		RepoName:    "parseable",
		RepoURL:     "https://charts.parseable.com",
		ChartName:   "parseable",
		Version:     entry.Version,
	}

	fmt.Println(common.Yellow + "Starting uninstallation process..." + common.Reset)

	spinner := common.CreateDeploymentSpinner(fmt.Sprintf("Uninstalling Parseable OSS '%s'...", entry.Name))
	spinner.Start()

	_, err := helm.Uninstall(helmApp, false)
	spinner.Stop()

	if err != nil {
		return fmt.Errorf("failed to uninstall Parseable OSS: %v", err)
	}

	fmt.Printf(common.Green+"Successfully uninstalled '%s' from namespace '%s'.\n"+common.Reset, entry.Name, entry.Namespace)
	return nil
}

func deleteSecret(namespace, secretName string) error {
	config, err := common.LoadKubeConfig()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	err = clientset.CoreV1().Secrets(namespace).Delete(context.TODO(), "parseable-env-secret", metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete secret '%s': %v", secretName, err)
	}

	return nil
}
