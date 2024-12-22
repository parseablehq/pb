package cmd

import (
	"context"
	"fmt"
	"log"

	"pb/pkg/common"
	"pb/pkg/helm"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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

		// Perform uninstallation
		// if err := uninstallCluster(selectedCluster); err != nil {
		// 	log.Fatalf("Failed to uninstall cluster: %v", err)
		// }

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
