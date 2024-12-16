package installer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"pb/pkg/common"

	"github.com/manifoldco/promptui"
	yamlv2 "gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// Installer orchestrates the installation process
func Installer(_ Plan) (values *ValuesHolder, chartValues []string) {
	if _, err := promptK8sContext(); err != nil {
		log.Fatalf("Failed to prompt for kubernetes context: %v", err)
	}

	// pb supports only distributed deployments
	chartValues = append(chartValues, "parseable.highAvailability.enabled=true")

	// Prompt for namespace and credentials
	pbSecret, err := promptNamespaceAndCredentials()
	if err != nil {
		log.Fatalf("Failed to prompt for namespace and credentials: %v", err)
	}

	// Prompt for agent deployment
	agent, agentValues, err := promptAgentDeployment(chartValues, distributed, pbSecret.Namespace)
	if err != nil {
		log.Fatalf("Failed to prompt for agent deployment: %v", err)
	}

	// Prompt for store configuration
	store, storeValues, err := promptStore(agentValues)
	if err != nil {
		log.Fatalf("Failed to prompt for store configuration: %v", err)
	}

	// Prompt for object store configuration and get the final chart values
	objectStoreConfig, storeConfigValues, err := promptStoreConfigs(store, storeValues)
	if err != nil {
		log.Fatalf("Failed to prompt for object store configuration: %v", err)
	}

	if err := applyParseableSecret(pbSecret, store, objectStoreConfig); err != nil {
		log.Fatalf("Failed to apply secret object store configuration: %v", err)
	}

	valuesHolder := ValuesHolder{
		DeploymentType:    distributed,
		ObjectStoreConfig: objectStoreConfig,
		LoggingAgent:      loggingAgent(agent),
		ParseableSecret:   *pbSecret,
	}

	if err := writeParseableConfig(&valuesHolder); err != nil {
		log.Fatalf("Failed to write Parseable configuration: %v", err)
	}

	return &valuesHolder, append(chartValues, storeConfigValues...)
}

// promptStorageClass prompts the user to enter a Kubernetes storage class
func promptStorageClass() (string, error) {
	// Prompt user for storage class
	fmt.Print(common.Yellow + "Enter the kubernetes storage class: " + common.Reset)
	reader := bufio.NewReader(os.Stdin)
	storageClass, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read storage class: %w", err)
	}

	storageClass = strings.TrimSpace(storageClass)

	// Validate that the storage class is not empty
	if storageClass == "" {
		return "", fmt.Errorf("storage class cannot be empty")
	}

	return storageClass, nil
}

// promptNamespaceAndCredentials prompts the user for namespace and credentials
func promptNamespaceAndCredentials() (*ParseableSecret, error) {
	// Prompt user for namespace
	fmt.Print(common.Yellow + "Enter the Kubernetes namespace for deployment: " + common.Reset)
	reader := bufio.NewReader(os.Stdin)
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

	return &ParseableSecret{
		Namespace: namespace,
		Username:  username,
		Password:  password,
	}, nil
}

// applyParseableSecret creates and applies the Kubernetes secret
func applyParseableSecret(ps *ParseableSecret, store ObjectStore, objectStoreConfig ObjectStoreConfig) error {
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

func getParseableSecretBlob(ps *ParseableSecret, objectStore ObjectStoreConfig) string {
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

func getParseableSecretS3(ps *ParseableSecret, objectStore ObjectStoreConfig) string {
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

func getParseableSecretGcs(ps *ParseableSecret, objectStore ObjectStoreConfig) string {
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

func getParseableSecretLocal(ps *ParseableSecret) string {
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
func promptAgentDeployment(chartValues []string, deployment deploymentType, namespace string) (string, []string, error) {
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

	if agentDeploymentType == string(vector) {
		chartValues = append(chartValues, "vector.enabled=true")
	} else if agentDeploymentType == string(fluentbit) {
		if deployment == standalone {
			chartValues = append(chartValues, "fluent-bit.serverHost=parseable."+namespace+".svc.cluster.local")
		} else if deployment == distributed {
			chartValues = append(chartValues, "fluent-bit.serverHost=parseable-ingestor-service."+namespace+".svc.cluster.local")
		}
		chartValues = append(chartValues, "fluent-bit.enabled=true")
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
func promptStoreConfigs(store ObjectStore, chartValues []string) (ObjectStoreConfig, []string, error) {
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
		chartValues = append(chartValues, "parseable.persistence.staging.storageClass="+sc)
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
		chartValues = append(chartValues, "parseable.persistence.staging.storageClass="+sc)
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
		chartValues = append(chartValues, "parseable.persistence.staging.storageClass="+sc)
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

func writeParseableConfig(valuesHolder *ValuesHolder) error {
	// Create config directory
	configDir := filepath.Join(os.Getenv("HOME"), ".parseable")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Define config file path
	configPath := filepath.Join(configDir, valuesHolder.ParseableSecret.Namespace+".yaml")

	// Marshal values to YAML
	configBytes, err := yamlv2.Marshal(valuesHolder)
	if err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}

	// Write config file
	if err := os.WriteFile(configPath, configBytes, 0o644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
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
