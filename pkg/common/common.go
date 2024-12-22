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

package common

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/briandowns/spinner"
	"github.com/manifoldco/promptui"
	"gopkg.in/yaml.v2"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	configMapName = "parseable-installer"
	namespace     = "pb-system"
	dataKey       = "installer-data"
)

// ANSI escape codes for colors
const (
	Yellow = "\033[33m"
	Green  = "\033[32m"
	Red    = "\033[31m"
	Reset  = "\033[0m"
	Blue   = "\033[34m"
	Cyan   = "\033[36m"
)

// InstallerEntry represents an entry in the installer.yaml file
type InstallerEntry struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
	Version   string `yaml:"version"`
	Status    string `yaml:"status"` // todo ideally should be a heartbeat
}

// ReadInstallerConfigMap fetches and parses installer data from a ConfigMap
func ReadInstallerConfigMap() ([]InstallerEntry, error) {

	// Load kubeconfig and create a Kubernetes client
	config, err := LoadKubeConfig()
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
		if apiErrors.IsNotFound(err) {
			fmt.Println(Yellow + "\nNo existing Parseable OSS clusters found.\n" + Reset)
			return nil, nil
		}
		return nil, fmt.Errorf("failed to fetch ConfigMap: %w", err)
	}
	// Retrieve and parse the installer data
	rawData, ok := cm.Data[dataKey]
	if !ok {
		fmt.Println(Yellow + "\n────────────────────────────────────────────────────────────────────────────")
		fmt.Println(Yellow + "⚠️  No Parseable clusters found!")
		fmt.Println(Yellow + "To get started, run: `pb install oss`")
		fmt.Println(Yellow + "────────────────────────────────────────────────────────────────────────────\n")
		return nil, nil
	}

	var entries []InstallerEntry
	if err := yaml.Unmarshal([]byte(rawData), &entries); err != nil {
		return nil, fmt.Errorf("failed to parse ConfigMap data: %w", err)
	}

	return entries, nil
}

// LoadKubeConfig loads the kubeconfig from the default location
func LoadKubeConfig() (*rest.Config, error) {
	kubeconfig := clientcmd.NewDefaultClientConfigLoadingRules().GetDefaultFilename()
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}

// PromptK8sContext retrieves Kubernetes contexts from kubeconfig.
func PromptK8sContext() (clusterName string, err error) {
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		kubeconfigPath = os.Getenv("HOME") + "/.kube/config"
	}

	// Load kubeconfig file
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		fmt.Printf("\033[31mError loading kubeconfig: %v\033[0m\n", err)
		os.Exit(1)
	}

	// Get current contexts
	currentContext := config.Contexts
	var contexts []string
	for i := range currentContext {
		contexts = append(contexts, i)
	}

	// Prompt user to select Kubernetes context
	promptK8s := promptui.Select{
		Items: contexts,
		Templates: &promptui.SelectTemplates{
			Label:    "{{ `Select your Kubernetes context` | yellow }}",
			Active:   "▸ {{ . | yellow }} ", // Yellow arrow and context name for active selection
			Inactive: "  {{ . | yellow }}",  // Default color for inactive items
			Selected: "{{ `Selected Kubernetes context:` | green }} '{{ . | green }}' ✔",
		},
	}

	_, clusterName, err = promptK8s.Run()
	if err != nil {
		return "", err
	}

	// Set current context as selected
	config.CurrentContext = clusterName
	err = clientcmd.WriteToFile(*config, kubeconfigPath)
	if err != nil {
		return "", err
	}

	return clusterName, nil
}

func PromptClusterSelection(entries []InstallerEntry) (InstallerEntry, error) {
	clusterNames := make([]string, len(entries))
	for i, entry := range entries {
		clusterNames[i] = fmt.Sprintf("[Name: %s] [Namespace: %s] [Version: %s]", entry.Name, entry.Namespace, entry.Version)
	}

	prompt := promptui.Select{
		Label: "Select a cluster to uninstall",
		Items: clusterNames,
		Templates: &promptui.SelectTemplates{
			Label:    "{{ `Select Cluster` | yellow }}",
			Active:   "▸ {{ . | yellow }}",
			Inactive: "  {{ . | yellow }}",
			Selected: "{{ `Selected:` | green }} {{ . | green }}",
		},
	}

	index, _, err := prompt.Run()
	if err != nil {
		return InstallerEntry{}, fmt.Errorf("failed to prompt for cluster selection: %v", err)
	}

	return entries[index], nil
}

func PromptConfirmation(message string) bool {
	prompt := promptui.Prompt{
		Label:     message,
		IsConfirm: true,
	}

	_, err := prompt.Run()
	return err == nil
}

func CreateDeploymentSpinner(namespace, infoMsg string) *spinner.Spinner {
	// Custom spinner with multiple character sets for dynamic effect
	spinnerChars := []string{
		"●", "○", "◉", "○", "◉", "○", "◉", "○", "◉",
	}

	s := spinner.New(
		spinnerChars,
		120*time.Millisecond,
		spinner.WithColor(Yellow),
		spinner.WithSuffix(" ..."),
	)

	s.Prefix = fmt.Sprintf(Yellow + infoMsg)

	return s
}
func RemoveInstallerEntry(name string) error {
	// Load kubeconfig and create a Kubernetes client
	config, err := LoadKubeConfig()
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Fetch the ConfigMap
	configMap, err := clientset.CoreV1().ConfigMaps(namespace).Get(context.TODO(), configMapName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to fetch ConfigMap: %v", err)
	}

	// Log the current data in the ConfigMap

	// Assuming the entries are stored as YAML or JSON string, unmarshal them into a slice
	var entries []map[string]interface{}
	if err := yaml.Unmarshal([]byte(configMap.Data["installer-data"]), &entries); err != nil {
		return fmt.Errorf("failed to unmarshal installer data: %w", err)
	}

	// Find the entry to remove by name
	var indexToRemove = -1
	for i, entry := range entries {
		if entry["name"] == name {
			indexToRemove = i
			break
		}
	}

	// Check if the entry was found
	if indexToRemove == -1 {
		return fmt.Errorf("entry '%s' does not exist in ConfigMap", name)
	}

	// Remove the entry
	entries = append(entries[:indexToRemove], entries[indexToRemove+1:]...)

	// Marshal the updated entries back into YAML
	updatedData, err := yaml.Marshal(entries)
	if err != nil {
		return fmt.Errorf("failed to marshal updated entries: %w", err)
	}
	configMap.Data["installer-data"] = string(updatedData)

	// Update the ConfigMap in Kubernetes
	_, err = clientset.CoreV1().ConfigMaps(namespace).Update(context.TODO(), configMap, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update ConfigMap: %v", err)
	}

	return nil
}
