package cmd

import (
	"fmt"
	"log"

	"pb/pkg/common"
	"pb/pkg/helm"

	"github.com/spf13/cobra"
)

// UninstallOssCmd removes Parseable OSS servers
var UninstallOssCmd = &cobra.Command{
	Use:     "oss",
	Short:   "Uninstall Parseable OSS servers",
	Example: "pb uninstall oss",
	Run: func(cmd *cobra.Command, _ []string) {
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
			fmt.Println(common.Yellow + "\nNo Parseable OSS servers found to uninstall.\n")
			return
		}

		// Prompt user to select a cluster
		selectedCluster, err := common.PromptClusterSelection(entries)
		if err != nil {
			log.Fatalf("Failed to select a cluster: %v", err)
		}

		// Confirm uninstallation
		fmt.Printf("\nYou have selected to uninstall the cluster '%s' in namespace '%s'.\n", selectedCluster.Name, selectedCluster.Namespace)
		if !common.PromptConfirmation(fmt.Sprintf("Do you want to proceed with uninstalling '%s'?", selectedCluster.Name)) {
			fmt.Println(common.Yellow + "Uninstall operation canceled.")
			return
		}

		// Perform uninstallation
		if err := uninstallCluster(selectedCluster); err != nil {
			log.Fatalf("Failed to uninstall cluster: %v", err)
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

	spinner := common.CreateDeploymentSpinner(entry.Namespace, fmt.Sprintf("Uninstalling Parseable OSS '%s'...", entry.Name))
	spinner.Start()

	_, err := helm.Uninstall(helmApp, false)
	spinner.Stop()

	if err != nil {
		return fmt.Errorf("failed to uninstall Parseable OSS: %v", err)
	}

	fmt.Printf(common.Green+"Successfully uninstalled '%s' from namespace '%s'.\n"+common.Reset, entry.Name, entry.Namespace)
	return nil
}
