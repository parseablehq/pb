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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"pb/pkg/common"
	"pb/pkg/helm"

	"github.com/manifoldco/promptui"
	yamlv3 "gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

func Installer(verbose bool) {
	printBanner()
	waterFall(verbose)
}

// waterFall orchestrates the installation process
func waterFall(verbose bool) {
	var chartValues []string
	plan, err := promptUserPlanSelection()
	if err != nil {
		log.Fatalf("Failed to prompt for plan selection: %v", err)
	}

	context, err := promptK8sContext()
	if err != nil {
		log.Fatalf("Failed to prompt for kubernetes context: %v", err)
	}

	// pb supports only distributed deployments
	chartValues = append(chartValues, "parseable.highAvailability.enabled=true")

	// Prompt for namespace and credentials
	pbInfo, err := promptNamespaceAndCredentials()
	if err != nil {
		log.Fatalf("Failed to prompt for namespace and credentials: %v", err)
	}

	// Prompt for agent deployment
	_, agentValues, err := promptAgentDeployment(chartValues, distributed, *pbInfo)
	if err != nil {
		log.Fatalf("Failed to prompt for agent deployment: %v", err)
	}

	// Prompt for store configuration
	store, storeValues, err := promptStore(agentValues)
	if err != nil {
		log.Fatalf("Failed to prompt for store configuration: %v", err)
	}

	// Prompt for object store configuration and get the final chart values
	objectStoreConfig, storeConfigs, err := promptStoreConfigs(store, storeValues, plan)
	if err != nil {
		log.Fatalf("Failed to prompt for object store configuration: %v", err)
	}

	if err := applyParseableSecret(pbInfo, store, objectStoreConfig); err != nil {
		log.Fatalf("Failed to apply secret object store configuration: %v", err)
	}

	// Define the deployment configuration
	config := HelmDeploymentConfig{
		ReleaseName: pbInfo.Name,
		Namespace:   pbInfo.Namespace,
		RepoName:    "parseable",
		RepoURL:     "https://charts.parseable.com",
		ChartName:   "parseable",
		Version:     "1.6.6",
		Values:      storeConfigs,
		Verbose:     verbose,
	}

	if err := deployRelease(config); err != nil {
		log.Fatalf("Failed to deploy parseable, err: %v", err)
	}

	if err := updateInstallerFile(InstallerEntry{
		Name:      pbInfo.Name,
		Namespace: pbInfo.Namespace,
		Version:   config.Version,
		Context:   context,
		Status:    "success",
	}); err != nil {
		log.Fatalf("Failed to update parseable installer file, err: %v", err)
	}
	printSuccessBanner(*pbInfo, config.Version)

}

// promptStorageClass fetches and prompts the user to select a Kubernetes storage class
func promptStorageClass() (string, error) {
	// Load the kubeconfig from the default location
	kubeconfig := clientcmd.NewDefaultClientConfigLoadingRules().GetDefaultFilename()
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return "", fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Create a Kubernetes client
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Fetch the storage classes
	storageClasses, err := clientset.StorageV1().StorageClasses().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to fetch storage classes: %w", err)
	}

	// Extract the names of storage classes
	var storageClassNames []string
	for _, sc := range storageClasses.Items {
		storageClassNames = append(storageClassNames, sc.Name)
	}

	// Check if there are no storage classes available
	if len(storageClassNames) == 0 {
		return "", fmt.Errorf("no storage classes found in the cluster")
	}

	// Use promptui to allow the user to select a storage class
	prompt := promptui.Select{
		Label: "Select a Kubernetes storage class",
		Items: storageClassNames,
	}

	_, selectedStorageClass, err := prompt.Run()
	if err != nil {
		return "", fmt.Errorf("failed to select storage class: %w", err)
	}

	return selectedStorageClass, nil
}

// promptNamespaceAndCredentials prompts the user for namespace and credentials
func promptNamespaceAndCredentials() (*ParseableInfo, error) {
	// Prompt user for release name
	fmt.Print(common.Yellow + "Enter the Name for deployment: " + common.Reset)
	reader := bufio.NewReader(os.Stdin)
	name, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read namespace: %w", err)
	}
	name = strings.TrimSpace(name)

	// Prompt user for namespace
	fmt.Print(common.Yellow + "Enter the Kubernetes namespace for deployment: " + common.Reset)
	namespace, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read namespace: %w", err)
	}
	namespace = strings.TrimSpace(namespace)

	// Prompt for username
	fmt.Print(common.Yellow + "Enter the Parseable username: " + common.Reset)
	username, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read username: %w", err)
	}
	username = strings.TrimSpace(username)

	// Prompt for password
	fmt.Print(common.Yellow + "Enter the Parseable password: " + common.Reset)
	password, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read password: %w", err)
	}
	password = strings.TrimSpace(password)

	return &ParseableInfo{
		Name:      name,
		Namespace: namespace,
		Username:  username,
		Password:  password,
	}, nil
}

// applyParseableSecret creates and applies the Kubernetes secret
func applyParseableSecret(ps *ParseableInfo, store ObjectStore, objectStoreConfig ObjectStoreConfig) error {
	var secretManifest string
	if store == LocalStore {
		secretManifest = getParseableSecretLocal(ps)
	} else if store == S3Store {
		secretManifest = getParseableSecretS3(ps, objectStoreConfig)
	} else if store == BlobStore {
		secretManifest = getParseableSecretBlob(ps, objectStoreConfig)
	} else if store == GcsStore {
		secretManifest = getParseableSecretGcs(ps, objectStoreConfig)
	}

	// apply the Kubernetes Secret
	if err := applyManifest(secretManifest); err != nil {
		return fmt.Errorf("failed to create and apply secret: %w", err)
	}

	fmt.Println(common.Green + "Parseable Secret successfully created and applied!" + common.Reset)
	return nil
}

func getParseableSecretBlob(ps *ParseableInfo, objectStore ObjectStoreConfig) string {
	// Create the Secret manifest
	secretManifest := fmt.Sprintf(`
apiVersion: v1
kind: Secret
metadata:
  name: parseable-env-secret
  namespace: %s
type: Opaque
data:
- addr
  azr.access_key: %s
  azr.account: %s
  azr.container: %s
  azr.url: %s
  username: %s
  password: %s
  addr: %s
  fs.dir: %s
  staging.dir: %s
`,
		ps.Namespace,
		base64.StdEncoding.EncodeToString([]byte(objectStore.BlobStore.AccessKey)),
		base64.StdEncoding.EncodeToString([]byte(objectStore.BlobStore.AccountName)),
		base64.StdEncoding.EncodeToString([]byte(objectStore.BlobStore.Container)),
		base64.StdEncoding.EncodeToString([]byte(objectStore.BlobStore.URL)),
		base64.StdEncoding.EncodeToString([]byte(ps.Username)),
		base64.StdEncoding.EncodeToString([]byte(ps.Password)),
		base64.StdEncoding.EncodeToString([]byte("0.0.0.0:8000")),
		base64.StdEncoding.EncodeToString([]byte("./data")),
		base64.StdEncoding.EncodeToString([]byte("./staging")),
	)
	return secretManifest
}

func getParseableSecretS3(ps *ParseableInfo, objectStore ObjectStoreConfig) string {
	// Create the Secret manifest
	secretManifest := fmt.Sprintf(`
apiVersion: v1
kind: Secret
metadata:
  name: parseable-env-secret
  namespace: %s
type: Opaque
data:
  s3.url: %s
  s3.region: %s
  s3.bucket: %s
  s3.access.key: %s
  s3.secret.key: %s
  username: %s
  password: %s
  addr: %s
  fs.dir: %s
  staging.dir: %s
`,
		ps.Namespace,
		base64.StdEncoding.EncodeToString([]byte(objectStore.S3Store.URL)),
		base64.StdEncoding.EncodeToString([]byte(objectStore.S3Store.Region)),
		base64.StdEncoding.EncodeToString([]byte(objectStore.S3Store.Bucket)),
		base64.StdEncoding.EncodeToString([]byte(objectStore.S3Store.AccessKey)),
		base64.StdEncoding.EncodeToString([]byte(objectStore.S3Store.SecretKey)),
		base64.StdEncoding.EncodeToString([]byte(ps.Username)),
		base64.StdEncoding.EncodeToString([]byte(ps.Password)),
		base64.StdEncoding.EncodeToString([]byte("0.0.0.0:8000")),
		base64.StdEncoding.EncodeToString([]byte("./data")),
		base64.StdEncoding.EncodeToString([]byte("./staging")),
	)
	return secretManifest
}

func getParseableSecretGcs(ps *ParseableInfo, objectStore ObjectStoreConfig) string {
	// Create the Secret manifest
	secretManifest := fmt.Sprintf(`
apiVersion: v1
kind: Secret
metadata:
  name: parseable-env-secret
  namespace: %s
type: Opaque
data:
  gcs.url: %s
  gcs.region: %s
  gcs.bucket: %s
  gcs.access.key: %s
  gcs.secret.key: %s
  username: %s
  password: %s
  addr: %s
  fs.dir: %s
  staging.dir: %s
`,
		ps.Namespace,
		base64.StdEncoding.EncodeToString([]byte(objectStore.GCSStore.URL)),
		base64.StdEncoding.EncodeToString([]byte(objectStore.GCSStore.Region)),
		base64.StdEncoding.EncodeToString([]byte(objectStore.GCSStore.Bucket)),
		base64.StdEncoding.EncodeToString([]byte(objectStore.GCSStore.AccessKey)),
		base64.StdEncoding.EncodeToString([]byte(objectStore.GCSStore.SecretKey)),
		base64.StdEncoding.EncodeToString([]byte(ps.Username)),
		base64.StdEncoding.EncodeToString([]byte(ps.Password)),
		base64.StdEncoding.EncodeToString([]byte("0.0.0.0:8000")),
		base64.StdEncoding.EncodeToString([]byte("./data")),
		base64.StdEncoding.EncodeToString([]byte("./staging")),
	)
	return secretManifest
}

func getParseableSecretLocal(ps *ParseableInfo) string {
	// Create the Secret manifest
	secretManifest := fmt.Sprintf(`
apiVersion: v1
kind: Secret
metadata:
  name: parseable-env-secret
  namespace: %s
type: Opaque
data:
  username: %s
  password: %s
  addr: %s
  fs.dir: %s
  staging.dir: %s
  
`,
		ps.Namespace,
		base64.StdEncoding.EncodeToString([]byte(ps.Username)),
		base64.StdEncoding.EncodeToString([]byte(ps.Password)),
		base64.StdEncoding.EncodeToString([]byte("0.0.0.0:8000")),
		base64.StdEncoding.EncodeToString([]byte("./data")),
		base64.StdEncoding.EncodeToString([]byte("./staging")),
	)
	return secretManifest
}

// promptAgentDeployment prompts the user for agent deployment options
func promptAgentDeployment(chartValues []string, deployment deploymentType, pbInfo ParseableInfo) (string, []string, error) {
	// Prompt for Agent Deployment type
	promptAgentSelect := promptui.Select{
		Items: []string{string(fluentbit), string(vector), "I have my agent running / I'll set up later"},
		Templates: &promptui.SelectTemplates{
			Label:    "{{ `Logging agent` | yellow }}",
			Active:   "▸ {{ . | yellow }} ", // Yellow arrow and context name for active selection
			Inactive: "  {{ . | yellow }}",  // Default color for inactive items
			Selected: "{{ `Selected option:` | green }} '{{ . | green }}' ✔ ",
		},
	}
	_, agentDeploymentType, err := promptAgentSelect.Run()
	if err != nil {
		return "", nil, fmt.Errorf("failed to prompt for agent deployment type: %w", err)
	}

	ingestorUrl, _ := getParseableSvcUrls(pbInfo.Name, pbInfo.Namespace)

	if agentDeploymentType == string(fluentbit) {
		chartValues = append(chartValues, "fluent-bit.serverHost="+ingestorUrl)
		chartValues = append(chartValues, "fluent-bit.serverUsername="+pbInfo.Username)
		chartValues = append(chartValues, "fluent-bit.serverPassword="+pbInfo.Password)
		chartValues = append(chartValues, "fluent-bit.serverStream="+"$NAMESPACE")

		// Prompt for namespaces to exclude
		promptExcludeNamespaces := promptui.Prompt{
			Label: "Enter namespaces to exclude from collection (comma-separated, e.g., kube-system,default): ",
			Templates: &promptui.PromptTemplates{
				Prompt:  "{{ `Namespaces to exclude` | yellow }}: ",
				Valid:   "{{ `` | green }}: {{ . | yellow }}",
				Invalid: "{{ `Invalid input` | red }}",
			},
		}
		excludeNamespaces, err := promptExcludeNamespaces.Run()
		if err != nil {
			return "", nil, fmt.Errorf("failed to prompt for exclude namespaces: %w", err)
		}

		chartValues = append(chartValues, "fluent-bit.excludeNamespaces="+strings.ReplaceAll(excludeNamespaces, ",", "\\,"))
		chartValues = append(chartValues, "fluent-bit.enabled=true")
	} else if agentDeploymentType == string(vector) {
		chartValues = append(chartValues, "vector.enabled=true")
	}

	return agentDeploymentType, chartValues, nil
}

// promptStore prompts the user for object store options
func promptStore(chartValues []string) (ObjectStore, []string, error) {
	// Prompt for store type
	promptStore := promptui.Select{
		Templates: &promptui.SelectTemplates{
			Label:    "{{ `Object store` | yellow }}",
			Active:   "▸ {{ . | yellow }} ", // Yellow arrow and context name for active selection
			Inactive: "  {{ . | yellow }}",  // Default color for inactive items
			Selected: "{{ `Selected object store:` | green }} '{{ . | green }}' ✔ ",
		},
		Items: []string{string(S3Store), string(BlobStore), string(GcsStore)}, // local store not supported
	}
	_, promptStoreType, err := promptStore.Run()
	if err != nil {
		return "", nil, fmt.Errorf("failed to prompt for object store type: %w", err)
	}

	newChartValues := []string{
		"parseable.store=" + promptStoreType,
	}

	chartValues = append(chartValues, newChartValues...)
	return ObjectStore(promptStoreType), chartValues, nil
}

// promptStoreConfigs prompts for object store configurations and appends chart values
func promptStoreConfigs(store ObjectStore, chartValues []string, plan Plan) (ObjectStoreConfig, []string, error) {

	cpuIngestors := "parseable.highAvailability.ingestor.resources.limits.cpu=" + plan.CPU
	memoryIngestors := "parseable.highAvailability.ingestor.resources.limits.memory=" + plan.Memory

	cpuQuery := "parseable.resources.limits.cpu=" + plan.CPU
	memoryQuery := "parseable.resources.limits.memory=" + plan.Memory

	// Initialize a struct to hold store values
	var storeValues ObjectStoreConfig

	fmt.Println(common.Green + "Configuring:" + common.Reset + " " + store)

	// Store selected store type in chart values
	switch store {
	case S3Store:
		storeValues.S3Store = S3{
			URL:       promptForInput(common.Yellow + "  Enter S3 URL: " + common.Reset),
			AccessKey: promptForInput(common.Yellow + "  Enter S3 Access Key: " + common.Reset),
			SecretKey: promptForInput(common.Yellow + "  Enter S3 Secret Key: " + common.Reset),
			Bucket:    promptForInput(common.Yellow + "  Enter S3 Bucket: " + common.Reset),
			Region:    promptForInput(common.Yellow + "  Enter S3 Region: " + common.Reset),
		}
		sc, err := promptStorageClass()
		if err != nil {
			log.Fatalf("Failed to prompt for storage class: %v", err)
		}
		storeValues.StorageClass = sc
		storeValues.ObjectStore = S3Store
		chartValues = append(chartValues, "parseable.store="+string(S3Store))
		chartValues = append(chartValues, "parseable.s3ModeSecret.enabled=true")
		chartValues = append(chartValues, "parseable.persistence.staging.enabled=true")
		chartValues = append(chartValues, "parseable.persistence.staging.size=5Gi")
		chartValues = append(chartValues, "parseable.persistence.staging.storageClass="+sc)
		chartValues = append(chartValues, cpuIngestors)
		chartValues = append(chartValues, memoryIngestors)
		chartValues = append(chartValues, cpuQuery)
		chartValues = append(chartValues, memoryQuery)

		return storeValues, chartValues, nil
	case BlobStore:
		sc, err := promptStorageClass()
		if err != nil {
			log.Fatalf("Failed to prompt for storage class: %v", err)
		}
		storeValues.BlobStore = Blob{
			URL:       promptForInput(common.Yellow + "  Enter Blob URL: " + common.Reset),
			Container: promptForInput(common.Yellow + "  Enter Blob Container: " + common.Reset),
		}
		storeValues.StorageClass = sc
		storeValues.ObjectStore = BlobStore
		chartValues = append(chartValues, "parseable.store="+string(BlobStore))
		chartValues = append(chartValues, "parseable.blobModeSecret.enabled=true")
		chartValues = append(chartValues, "parseable.persistence.staging.enabled=true")
		chartValues = append(chartValues, "parseable.persistence.staging.size=5Gi")
		chartValues = append(chartValues, "parseable.persistence.staging.storageClass="+sc)
		chartValues = append(chartValues, cpuIngestors)
		chartValues = append(chartValues, memoryIngestors)
		chartValues = append(chartValues, cpuQuery)
		chartValues = append(chartValues, memoryQuery)
		return storeValues, chartValues, nil
	case GcsStore:
		sc, err := promptStorageClass()
		if err != nil {
			log.Fatalf("Failed to prompt for storage class: %v", err)
		}
		storeValues.GCSStore = GCS{
			URL:       promptForInput(common.Yellow + "  Enter GCS URL: " + common.Reset),
			AccessKey: promptForInput(common.Yellow + "  Enter GCS Access Key: " + common.Reset),
			SecretKey: promptForInput(common.Yellow + "  Enter GCS Secret Key: " + common.Reset),
			Bucket:    promptForInput(common.Yellow + "  Enter GCS Bucket: " + common.Reset),
			Region:    promptForInput(common.Yellow + "  Enter GCS Region: " + common.Reset),
		}
		storeValues.StorageClass = sc
		storeValues.ObjectStore = GcsStore
		chartValues = append(chartValues, "parseable.store="+string(GcsStore))
		chartValues = append(chartValues, "parseable.gcsModeSecret.enabled=true")
		chartValues = append(chartValues, "parseable.persistence.staging.enabled=true")
		chartValues = append(chartValues, "parseable.persistence.staging.size=5Gi")
		chartValues = append(chartValues, "parseable.persistence.staging.storageClass="+sc)
		chartValues = append(chartValues, cpuIngestors)
		chartValues = append(chartValues, memoryIngestors)
		chartValues = append(chartValues, cpuQuery)
		chartValues = append(chartValues, memoryQuery)
		return storeValues, chartValues, nil
	}

	return storeValues, chartValues, nil
}

// applyManifest ensures the namespace exists and applies a Kubernetes manifest YAML to the cluster
func applyManifest(manifest string) error {
	// Load kubeconfig and create a dynamic Kubernetes client
	config, err := loadKubeConfig()
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Parse the manifest YAML into an unstructured object
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(manifest)), 1024)
	var obj unstructured.Unstructured
	if err := decoder.Decode(&obj); err != nil {
		return fmt.Errorf("failed to decode manifest: %w", err)
	}

	// Get the namespace from the manifest object
	namespace := obj.GetNamespace()

	if namespace != "" {
		// Ensure the namespace exists, create it if it doesn't
		namespaceClient := dynamic.NewForConfigOrDie(config).Resource(schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "namespaces",
		})

		// Try to get the namespace
		_, err := namespaceClient.Get(context.TODO(), namespace, metav1.GetOptions{})
		if err != nil {
			// If namespace doesn't exist, create it
			namespaceObj := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Namespace",
					"metadata": map[string]interface{}{
						"name": namespace,
					},
				},
			}
			_, err := namespaceClient.Create(context.TODO(), namespaceObj, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create namespace %s: %w", namespace, err)
			}
		}
	}

	// Get the GroupVersionResource dynamically
	gvr, err := getGVR(config, &obj)
	if err != nil {
		return fmt.Errorf("failed to get GVR: %w", err)
	}

	// Apply the manifest using the dynamic client
	_, err = dynamicClient.Resource(gvr).Namespace(namespace).Create(context.TODO(), &obj, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to apply manifest: %w", err)
	}
	return nil
}

// loadKubeConfig loads the kubeconfig from the default location
func loadKubeConfig() (*rest.Config, error) {
	kubeconfig := clientcmd.NewDefaultClientConfigLoadingRules().GetDefaultFilename()
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}

// getGVR fetches the GroupVersionResource for the provided object
func getGVR(config *rest.Config, obj *unstructured.Unstructured) (schema.GroupVersionResource, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("failed to create discovery client: %w", err)
	}

	groupResources, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("failed to get API group resources: %w", err)
	}

	mapper := restmapper.NewDiscoveryRESTMapper(groupResources)
	gvk := obj.GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return schema.GroupVersionResource{}, fmt.Errorf("failed to get GVR mapping: %w", err)
	}

	return mapping.Resource, nil
}

// Helper function to prompt for individual input values
func promptForInput(label string) string {
	fmt.Print(label)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// promptK8sContext retrieves Kubernetes contexts from kubeconfig.
func promptK8sContext() (clusterName string, err error) {
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

// printBanner displays a welcome banner
func printBanner() {
	banner := `
 --------------------------------------
  Welcome to Parseable OSS Installation
 --------------------------------------
`
	fmt.Println(common.Green + banner + common.Reset)
}

type HelmDeploymentConfig struct {
	ReleaseName string
	Namespace   string
	RepoName    string
	RepoURL     string
	ChartName   string
	Version     string
	Values      []string
	Verbose     bool
}

// deployRelease handles the deployment of a Helm release using a configuration struct
func deployRelease(config HelmDeploymentConfig) error {
	// Helm application configuration
	app := helm.Helm{
		ReleaseName: config.ReleaseName,
		Namespace:   config.Namespace,
		RepoName:    config.RepoName,
		RepoURL:     config.RepoURL,
		ChartName:   config.ChartName,
		Version:     config.Version,
		Values:      config.Values,
	}

	// Create a spinner
	msg := fmt.Sprintf(" Deploying parseable release name [%s] namespace [%s] ", config.ReleaseName, config.Namespace)
	spinner := createDeploymentSpinner(config.Namespace, msg)

	// Redirect standard output if not in verbose mode
	var oldStdout *os.File
	if !config.Verbose {
		oldStdout = os.Stdout
		_, w, _ := os.Pipe()
		os.Stdout = w
	}

	spinner.Start()

	// Deploy using Helm
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		if err := helm.Apply(app, config.Verbose); err != nil {
			errCh <- err
		}
	}()

	wg.Wait()
	close(errCh)

	// Stop the spinner and restore stdout
	spinner.Stop()
	if !config.Verbose {
		os.Stdout = oldStdout
	}

	// Check for errors
	if err, ok := <-errCh; ok {
		return err
	}

	return nil
}

// printSuccessBanner remains the same as in the original code
func printSuccessBanner(pbInfo ParseableInfo, version string) {

	ingestionUrl, queryUrl := getParseableSvcUrls(pbInfo.Name, pbInfo.Namespace)

	// Encode credentials to Base64
	credentials := map[string]string{
		"username": pbInfo.Username,
		"password": pbInfo.Password,
	}
	credentialsJSON, err := json.Marshal(credentials)
	if err != nil {
		fmt.Printf("failed to marshal credentials: %v\n", err)
		return
	}

	base64EncodedString := base64.StdEncoding.EncodeToString(credentialsJSON)

	fmt.Println("\n" + common.Green + "🎉 Parseable Deployment Successful! 🎉" + common.Reset)
	fmt.Println(strings.Repeat("=", 50))

	fmt.Printf("%s Deployment Details:\n", common.Blue+"ℹ️ ")
	fmt.Printf("  • Namespace:        %s\n", common.Blue+pbInfo.Namespace)
	fmt.Printf("  • Chart Version:    %s\n", common.Blue+version)
	fmt.Printf("  • Ingestion URL:    %s\n", ingestionUrl)

	fmt.Println("\n" + common.Blue + "🔗  Resources:" + common.Reset)
	fmt.Println(common.Blue + "  • Documentation:   https://www.parseable.com/docs/server/introduction")
	fmt.Println(common.Blue + "  • Stream Management: https://www.parseable.com/docs/server/api")

	fmt.Println("\n" + common.Blue + "Happy Logging!" + common.Reset)

	// Port-forward the service
	localPort := "8001"
	fmt.Printf(common.Green+"Port-forwarding %s service on port %s in namespace %s...\n"+common.Reset, queryUrl, localPort, pbInfo.Namespace)

	if err = startPortForward(pbInfo.Namespace, queryUrl, "80", localPort, false); err != nil {
		fmt.Printf(common.Red+"failed to port-forward service: %s", err.Error())
	}

	// Redirect to UI
	localURL := fmt.Sprintf("http://localhost:%s/login?q=%s", localPort, base64EncodedString)
	fmt.Printf(common.Green+"Opening Parseable UI at %s\n"+common.Reset, localURL)
	openBrowser(localURL)
}

func startPortForward(namespace, serviceName, remotePort, localPort string, verbose bool) error {
	// Build the port-forward command
	cmd := exec.Command("kubectl", "port-forward",
		fmt.Sprintf("svc/%s", serviceName),
		fmt.Sprintf("%s:%s", localPort, remotePort),
		"-n", namespace,
	)

	// Redirect the command's output to the standard output for debugging
	if !verbose {
		cmd.Stdout = nil // Suppress standard output
		cmd.Stderr = nil // Suppress standard error
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	// Run the command in the background
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start port-forward: %w", err)
	}

	// Run in a goroutine to keep it alive
	go func() {
		_ = cmd.Wait()
	}()

	// Check connection on the forwarded port
	retries := 10
	for i := 0; i < retries; i++ {
		conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%s", localPort))
		if err == nil {
			conn.Close() // Connection successful, break out of the loop
			fmt.Println(common.Green + "Port-forwarding successfully established!")
			time.Sleep(5 * time.Second) // some delay
			return nil
		}
		time.Sleep(3 * time.Second) // Wait before retrying
	}

	// If we reach here, port-forwarding failed
	cmd.Process.Kill() // Stop the kubectl process
	return fmt.Errorf(common.Red+"failed to establish port-forward connection to localhost:%s", localPort)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch os := runtime.GOOS; os {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		fmt.Printf("Please open the following URL manually: %s\n", url)
		return
	}
	cmd.Start()
}

// updateInstallerFile updates or creates the installer.yaml file with deployment info
func updateInstallerFile(entry InstallerEntry) error {
	// Define the file path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}
	filePath := filepath.Join(homeDir, ".parseable", "pb", "installer.yaml")

	// Create the directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for installer file: %w", err)
	}

	// Read existing entries if the file exists
	var entries []InstallerEntry
	if _, err := os.Stat(filePath); err == nil {
		// File exists, load existing content
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read existing installer file: %w", err)
		}

		if err := yaml.Unmarshal(data, &entries); err != nil {
			return fmt.Errorf("failed to parse existing installer file: %w", err)
		}
	}

	// Append the new entry
	entries = append(entries, entry)

	// Write the updated entries back to the file
	data, err := yamlv3.Marshal(entries)
	if err != nil {
		return fmt.Errorf("failed to marshal installer data: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write installer file: %w", err)
	}

	return nil
}

func getParseableSvcUrls(releaseName, namespace string) (ingestorUrl, queryUrl string) {
	if releaseName == "parseable" {
		ingestorUrl = releaseName + "-ingestor-service." + namespace + ".svc.cluster.local"
		queryUrl = releaseName + "-querier-service"
		return ingestorUrl, queryUrl
	}
	ingestorUrl = releaseName + "-parseable-ingestor-service." + namespace + ".svc.cluster.local"
	queryUrl = releaseName + "-parseable-querier-service"
	return ingestorUrl, queryUrl
}
