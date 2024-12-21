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

package installer

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"pb/pkg/common"
	"pb/pkg/helm"
	"strings"
	"time"

	"github.com/manifoldco/promptui"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
)

// Uninstaller uninstalls Parseable from the selected cluster
func Uninstaller(verbose bool) error {
	// Define the installer file path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}
	installerFilePath := filepath.Join(homeDir, ".parseable", "pb", "installer.yaml")

	// Read the installer file
	data, err := os.ReadFile(installerFilePath)
	if err != nil {
		return fmt.Errorf("failed to read installer file: %w", err)
	}

	// Unmarshal the installer file content
	var entries []InstallerEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("failed to parse installer file: %w", err)
	}

	// Prompt the user to select a cluster
	clusterNames := make([]string, len(entries))
	for i, entry := range entries {
		clusterNames[i] = fmt.Sprintf("[Name: %s] [Namespace: %s] [Context: %s]", entry.Name, entry.Namespace)
	}

	promptClusterSelect := promptui.Select{
		Label: "Select a cluster to delete",
		Items: clusterNames,
		Templates: &promptui.SelectTemplates{
			Label:    "{{ `Select Cluster` | yellow }}",
			Active:   "▸ {{ . | yellow }}", // Yellow arrow for active selection
			Inactive: "  {{ . | yellow }}",
			Selected: "{{ `Selected:` | green }} {{ . | green }}",
		},
	}

	index, _, err := promptClusterSelect.Run()
	if err != nil {
		return fmt.Errorf("failed to prompt for cluster selection: %v", err)
	}

	selectedCluster := entries[index]

	// Display a warning banner
	fmt.Println("\n────────────────────────────────────────────────────────────────────────────")
	fmt.Println("⚠️  Deleting this cluster will not delete any data on object storage.")
	fmt.Println("   This operation will clean up the Parseable deployment on Kubernetes.")
	fmt.Println("────────────────────────────────────────────────────────────────────────────")

	// Confirm deletion
	confirm, err := promptUserConfirmation(fmt.Sprintf(common.Yellow+"Do you still want to proceed with deleting the cluster '%s'?", selectedCluster.Name))
	if err != nil {
		return fmt.Errorf("failed to get user confirmation: %v", err)
	}
	if !confirm {
		fmt.Println(common.Yellow + "Uninstall canceled." + common.Reset)
		return nil
	}

	// Helm application configuration
	helmApp := helm.Helm{
		ReleaseName: selectedCluster.Name,
		Namespace:   selectedCluster.Namespace,
		RepoName:    "parseable",
		RepoURL:     "https://charts.parseable.com",
		ChartName:   "parseable",
		Version:     selectedCluster.Version,
	}

	// Create a spinner
	spinner := createDeploymentSpinner(selectedCluster.Namespace, "Uninstalling Parseable in ")

	// Redirect standard output if not in verbose mode
	var oldStdout *os.File
	if !verbose {
		oldStdout = os.Stdout
		_, w, _ := os.Pipe()
		os.Stdout = w
	}

	spinner.Start()

	// Run Helm uninstall
	_, err = helm.Uninstall(helmApp, verbose)
	spinner.Stop()

	// Restore stdout
	if !verbose {
		os.Stdout = oldStdout
	}

	if err != nil {
		return fmt.Errorf("failed to uninstall Parseable: %v", err)
	}

	// Call to clean up the secret instead of the namespace
	fmt.Printf(common.Yellow+"Cleaning up 'parseable-env-secret' in namespace '%s'...\n"+common.Reset, selectedCluster.Namespace)
	cleanupErr := cleanupParseableSecret(selectedCluster.Namespace)
	if cleanupErr != nil {
		return fmt.Errorf("failed to clean up secret in namespace '%s': %v", selectedCluster.Namespace, cleanupErr)
	}

	// Print success banner
	fmt.Printf(common.Green+"Successfully uninstalled Parseable from namespace '%s'.\n"+common.Reset, selectedCluster.Namespace)

	return nil
}

// promptUserConfirmation prompts the user for a yes/no confirmation
func promptUserConfirmation(message string) (bool, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s [y/N]: ", message)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes", nil
}

// cleanupParseableSecret deletes the "parseable-env-secret" in the specified namespace using Kubernetes client-go
func cleanupParseableSecret(namespace string) error {
	// Load the kubeconfig
	config, err := loadKubeConfig()
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	// Create a context with a timeout for secret deletion
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Define the secret name
	secretName := "parseable-env-secret"

	// Delete the secret
	err = clientset.CoreV1().Secrets(namespace).Delete(ctx, secretName, v1.DeleteOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			fmt.Printf("Secret '%s' not found in namespace '%s'. Nothing to delete.\n", secretName, namespace)
			return nil
		}
		return fmt.Errorf("error deleting secret '%s' in namespace '%s': %v", secretName, namespace, err)
	}

	// Confirm the deletion
	fmt.Printf("Secret '%s' successfully deleted from namespace '%s'.\n", secretName, namespace)

	return nil
}
