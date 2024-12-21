package cmd

import (
	"context"
	"fmt"
	"log"
	"os"

	"pb/pkg/common"
	"pb/pkg/installer"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ListOssCmd lists the Parseable OSS servers
var ListOssCmd = &cobra.Command{
	Use:     "oss",
	Short:   "List available Parseable OSS servers",
	Example: "pb list oss",
	Run: func(cmd *cobra.Command, _ []string) {
		_, err := installer.PromptK8sContext()
		if err != nil {
			log.Fatalf("Failed to prompt for kubernetes context: %v", err)
		}

		// Read the installer data from the ConfigMap
		entries, err := readInstallerConfigMap()
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

// readInstallerConfigMap fetches and parses installer data from a ConfigMap
func readInstallerConfigMap() ([]installer.InstallerEntry, error) {
	const (
		configMapName = "parseable-installer"
		namespace     = "pb-system"
		dataKey       = "installer-data"
	)

	// Load kubeconfig and create a Kubernetes client
	config, err := loadKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Get the ConfigMap
	cm, err := clientset.CoreV1().ConfigMaps(namespace).Get(context.TODO(), configMapName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ConfigMap: %w", err)
	}

	// Retrieve and parse the installer data
	rawData, ok := cm.Data[dataKey]
	if !ok {
		fmt.Println(common.Yellow + "\n────────────────────────────────────────────────────────────────────────────")
		fmt.Println(common.Yellow + "⚠️  No Parseable clusters found!")
		fmt.Println(common.Yellow + "To get started, run: `pb install oss`")
		fmt.Println(common.Yellow + "────────────────────────────────────────────────────────────────────────────\n")
		return nil, nil
	}

	var entries []installer.InstallerEntry
	if err := yaml.Unmarshal([]byte(rawData), &entries); err != nil {
		return nil, fmt.Errorf("failed to parse ConfigMap data: %w", err)
	}

	return entries, nil
}

// loadKubeConfig loads the kubeconfig from the default location
func loadKubeConfig() (*rest.Config, error) {
	kubeconfig := clientcmd.NewDefaultClientConfigLoadingRules().GetDefaultFilename()
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}
