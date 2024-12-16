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

	"gopkg.in/yaml.v2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func Uninstaller(verbose bool) error {
	// Load configuration from the parseable.yaml file
	configPath := filepath.Join(os.Getenv("HOME"), ".parseable", "parseable.yaml")
	config, err := loadParseableConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %v", err)
	}

	if config == (&ValuesHolder{}) {
		return fmt.Errorf("no existing configuration found in ~/.parseable/parseable.yaml")
	}

	// Prompt for Kubernetes context
	_, err = promptK8sContext()
	if err != nil {
		return fmt.Errorf("failed to prompt for Kubernetes context: %v", err)
	}

	// Prompt user to confirm namespace
	namespace := config.ParseableSecret.Namespace
	confirm, err := promptUserConfirmation(fmt.Sprintf(common.Yellow+"Do you wish to uninstall Parseable from namespace '%s'?", namespace))
	if err != nil {
		return fmt.Errorf("failed to get user confirmation: %v", err)
	}
	if !confirm {
		return fmt.Errorf("Uninstall cancelled.")
	}

	// Helm application configuration
	helmApp := helm.Helm{
		ReleaseName: "parseable",
		Namespace:   namespace,
		RepoName:    "parseable",
		RepoURL:     "https://charts.parseable.com",
		ChartName:   "parseable",
		Version:     "1.6.5",
	}

	// Create a spinner
	spinner := createDeploymentSpinner(namespace, "Uninstalling parseable in ")

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

	// Namespace cleanup using Kubernetes client
	fmt.Printf(common.Yellow+"Cleaning up namespace '%s'...\n"+common.Reset, namespace)
	cleanupErr := cleanupNamespaceWithClient(namespace)
	if cleanupErr != nil {
		return fmt.Errorf("failed to clean up namespace '%s': %v", namespace, cleanupErr)
	}

	// Print success banner
	fmt.Printf(common.Green+"Successfully uninstalled Parseable from namespace '%s'.\n"+common.Reset, namespace)

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

// loadParseableConfig loads the configuration from the specified file
func loadParseableConfig(path string) (*ValuesHolder, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config ValuesHolder
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// cleanupNamespaceWithClient deletes the specified namespace using Kubernetes client-go
func cleanupNamespaceWithClient(namespace string) error {
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

	// Create a context with a timeout for namespace deletion
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Delete the namespace
	err = clientset.CoreV1().Namespaces().Delete(ctx, namespace, v1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("error deleting namespace: %v", err)
	}

	// Wait for the namespace to be fully removed
	fmt.Printf("Waiting for namespace '%s' to be deleted...\n", namespace)
	for {
		_, err := clientset.CoreV1().Namespaces().Get(ctx, namespace, v1.GetOptions{})
		if err != nil {
			fmt.Printf("Namespace '%s' successfully deleted.\n", namespace)
			break
		}
		time.Sleep(2 * time.Second)
	}

	return nil
}
