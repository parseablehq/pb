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
	"runtime"
	"strings"
	"sync"
	"time"

	"pb/pkg/common"
	"pb/pkg/helm"

	"github.com/manifoldco/promptui"
	yamling "gopkg.in/yaml.v3"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	_, err = common.PromptK8sContext()
	if err != nil {
		log.Fatalf("Failed to prompt for kubernetes context: %v", err)
	}

	if plan.Name == "Playground" {
		chartValues = append(chartValues, "parseable.store=local-store")
		chartValues = append(chartValues, "parseable.localModeSecret.enabled=true")

		// Prompt for namespace and credentials
		pbInfo, err := promptNamespaceAndCredentials()
		if err != nil {
			log.Fatalf("Failed to prompt for namespace and credentials: %v", err)
		}

		// Prompt for agent deployment
		_, agentValues, err := promptAgentDeployment(chartValues, *pbInfo)
		if err != nil {
			log.Fatalf("Failed to prompt for agent deployment: %v", err)
		}

		if err := applyParseableSecret(pbInfo, LocalStore, ObjectStoreConfig{}); err != nil {
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
			Values:      agentValues,
			Verbose:     verbose,
		}

		if err := deployRelease(config); err != nil {
			log.Fatalf("Failed to deploy parseable, err: %v", err)
		}

		if err := updateInstallerConfigMap(common.InstallerEntry{
			Name:      pbInfo.Name,
			Namespace: pbInfo.Namespace,
			Version:   config.Version,
			Status:    "success",
		}); err != nil {
			log.Fatalf("Failed to update parseable installer file, err: %v", err)
		}

		printSuccessBanner(*pbInfo, config.Version, "parseable", "parseable")

		return
	}

	// pb supports only distributed deployments
	chartValues = append(chartValues, "parseable.highAvailability.enabled=true")

	// Prompt for namespace and credentials
	pbInfo, err := promptNamespaceAndCredentials()
	if err != nil {
		log.Fatalf("Failed to prompt for namespace and credentials: %v", err)
	}

	// Prompt for agent deployment
	_, agentValues, err := promptAgentDeployment(chartValues, *pbInfo)
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

	if err := updateInstallerConfigMap(common.InstallerEntry{
		Name:      pbInfo.Name,
		Namespace: pbInfo.Namespace,
		Version:   config.Version,
		Status:    "success",
	}); err != nil {
		log.Fatalf("Failed to update parseable installer file, err: %v", err)
	}

	ingestorURL, queryURL := getParseableSvcUrls(pbInfo.Name, pbInfo.Namespace)

	printSuccessBanner(*pbInfo, config.Version, ingestorURL, queryURL)

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
		base64.StdEncoding.EncodeToString([]byte(objectStore.BlobStore.StorageAccountName)),
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
func promptAgentDeployment(chartValues []string, pbInfo ParseableInfo) (string, []string, error) {
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

	ingestorURL, _ := getParseableSvcUrls(pbInfo.Name, pbInfo.Namespace)

	if agentDeploymentType == string(fluentbit) {
		chartValues = append(chartValues, "fluent-bit.serverHost="+ingestorURL)
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
			Region:    promptForInputWithDefault(common.Yellow+"  Enter S3 Region (default: us-east-1): "+common.Reset, "us-east-1"),
			AccessKey: promptForInputWithDefault(common.Yellow+"  Enter S3 Access Key: "+common.Reset, ""),
			SecretKey: promptForInputWithDefault(common.Yellow+"  Enter S3 Secret Key: "+common.Reset, ""),
			Bucket:    promptForInputWithDefault(common.Yellow+"  Enter S3 Bucket: "+common.Reset, ""),
		}

		// Dynamically construct the URL after Region is set
		storeValues.S3Store.URL = promptForInputWithDefault(
			common.Yellow+"  Enter S3 URL (default: https://s3."+storeValues.S3Store.Region+".amazonaws.com): "+common.Reset,
			"https://s3."+storeValues.S3Store.Region+".amazonaws.com",
		)

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
			StorageAccountName: promptForInputWithDefault(common.Yellow+"  Enter Blob Storage Account Name: "+common.Reset, ""),
			Container:          promptForInputWithDefault(common.Yellow+"  Enter Blob Container: "+common.Reset, ""),
			// ClientID:           promptForInputWithDefault(common.Yellow+"  Enter Client ID: "+common.Reset, ""),
			// ClientSecret:       promptForInputWithDefault(common.Yellow+"  Enter Client Secret: "+common.Reset, ""),
			// TenantID:           promptForInputWithDefault(common.Yellow+"  Enter Tenant ID: "+common.Reset, ""),
			AccessKey: promptForInputWithDefault(common.Yellow+"  Enter Access Keys: "+common.Reset, ""),
		}

		// Dynamically construct the URL after Region is set
		storeValues.BlobStore.URL = promptForInputWithDefault(
			common.Yellow+
				"  Enter Blob URL (default: https://"+storeValues.BlobStore.StorageAccountName+".blob.core.windows.net): "+
				common.Reset,
			"https://"+storeValues.BlobStore.StorageAccountName+".blob.core.windows.net")

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
			Bucket:    promptForInputWithDefault(common.Yellow+"  Enter GCS Bucket: "+common.Reset, ""),
			Region:    promptForInputWithDefault(common.Yellow+"  Enter GCS Region (default: us-east1): "+common.Reset, "us-east1"),
			URL:       promptForInputWithDefault(common.Yellow+"  Enter GCS URL (default: https://storage.googleapis.com):", "https://storage.googleapis.com"),
			AccessKey: promptForInputWithDefault(common.Yellow+"  Enter GCS Access Key: "+common.Reset, ""),
			SecretKey: promptForInputWithDefault(common.Yellow+"  Enter GCS Secret Key: "+common.Reset, ""),
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

// Helper function to prompt for input with a default value
func promptForInputWithDefault(label, defaultValue string) string {
	fmt.Print(label)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	// Use default if input is empty
	if input == "" {
		return defaultValue
	}
	return input
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
	spinner := common.CreateDeploymentSpinner(msg)

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
func printSuccessBanner(pbInfo ParseableInfo, version, ingestorURL, queryURL string) {

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
	fmt.Printf("  • Ingestion URL:    %s\n", ingestorURL)

	fmt.Println("\n" + common.Blue + "🔗  Resources:" + common.Reset)
	fmt.Println(common.Blue + "  • Documentation:   https://www.parseable.com/docs/server/introduction")
	fmt.Println(common.Blue + "  • Stream Management: https://www.parseable.com/docs/server/api")

	fmt.Println("\n" + common.Blue + "Happy Logging!" + common.Reset)

	// Port-forward the service
	localPort := "8001"
	fmt.Printf(common.Green+"Port-forwarding %s service on port %s in namespace %s...\n"+common.Reset, queryURL, localPort, pbInfo.Namespace)

	if err = startPortForward(pbInfo.Namespace, queryURL, "80", localPort, false); err != nil {
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

func updateInstallerConfigMap(entry common.InstallerEntry) error {
	const (
		configMapName = "parseable-installer"
		namespace     = "pb-system"
		dataKey       = "installer-data"
	)

	// Load kubeconfig and create a Kubernetes client
	config, err := loadKubeConfig()
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Ensure the namespace exists
	_, err = clientset.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = clientset.CoreV1().Namespaces().Create(context.TODO(), &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create namespace: %v", err)
			}
		} else {
			return fmt.Errorf("failed to check namespace existence: %v", err)
		}
	}

	// Create a dynamic Kubernetes client
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Define the ConfigMap resource
	configMapResource := schema.GroupVersionResource{
		Group:    "", // Core resources have an empty group
		Version:  "v1",
		Resource: "configmaps",
	}

	// Fetch the existing ConfigMap or initialize a new one
	cm, err := dynamicClient.Resource(configMapResource).Namespace(namespace).Get(context.TODO(), configMapName, metav1.GetOptions{})
	var data map[string]interface{}
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to fetch ConfigMap: %v", err)
		}
		// If not found, initialize a new ConfigMap
		data = map[string]interface{}{
			"metadata": map[string]interface{}{
				"name":      configMapName,
				"namespace": namespace,
			},
			"data": map[string]interface{}{},
		}
	} else {
		data = cm.Object
	}

	// Retrieve existing data and append the new entry
	existingData := data["data"].(map[string]interface{})
	var entries []common.InstallerEntry
	if raw, ok := existingData[dataKey]; ok {
		if err := yaml.Unmarshal([]byte(raw.(string)), &entries); err != nil {
			return fmt.Errorf("failed to parse existing ConfigMap data: %v", err)
		}
	}
	entries = append(entries, entry)

	// Marshal the updated data back to YAML
	updatedData, err := yamling.Marshal(entries)
	if err != nil {
		return fmt.Errorf("failed to marshal updated data: %v", err)
	}

	// Update the ConfigMap data
	existingData[dataKey] = string(updatedData)
	data["data"] = existingData

	// Apply the ConfigMap
	if cm == nil {
		_, err = dynamicClient.Resource(configMapResource).Namespace(namespace).Create(context.TODO(), &unstructured.Unstructured{
			Object: data,
		}, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create ConfigMap: %v", err)
		}
	} else {
		_, err = dynamicClient.Resource(configMapResource).Namespace(namespace).Update(context.TODO(), &unstructured.Unstructured{
			Object: data,
		}, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update ConfigMap: %v", err)
		}
	}

	return nil
}

func getParseableSvcUrls(releaseName, namespace string) (ingestorURL, queryURL string) {
	if releaseName == "parseable" {
		ingestorURL = releaseName + "-ingestor-service." + namespace + ".svc.cluster.local"
		queryURL = releaseName + "-querier-service"
		return ingestorURL, queryURL
	}
	ingestorURL = releaseName + "-parseable-ingestor-service." + namespace + ".svc.cluster.local"
	queryURL = releaseName + "-parseable-querier-service"
	return ingestorURL, queryURL
}
