package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"pb/pkg/analyze/k8s"
	"pb/pkg/common"
	"pb/pkg/helm"
	"strings"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// InstallOssCmd deploys Parseable OSS
var InstallOssCmd = &cobra.Command{
	Use:     "oss",
	Short:   "Deploy Parseable OSS",
	Example: "pb install oss",
	RunE: func(cmd *cobra.Command, args []string) error {
		printBanner()
		// Prompt for Kubernetes context
		_, err := k8s.PromptK8sContext()
		if err != nil {
			return fmt.Errorf(common.Red+"Error prompting Kubernetes context: %w"+common.Reset, err)
		}

		// Prompt user for namespace
		fmt.Print(common.Yellow + "Enter the Kubernetes namespace for deployment: " + common.Reset)
		reader := bufio.NewReader(os.Stdin)
		namespace, _ := reader.ReadString('\n')
		namespace = strings.TrimSpace(namespace)

		// Prompt for username
		fmt.Print(common.Yellow + "Enter the Parseable username: " + common.Reset)
		username, _ := reader.ReadString('\n')
		username = strings.TrimSpace(username)

		// Prompt for password
		fmt.Print(common.Yellow + "Enter the Parseable password: " + common.Reset)
		password, _ := reader.ReadString('\n')
		password = strings.TrimSpace(password)

		// Encode username and password to base64 for the Secret
		encodedUsername := base64.StdEncoding.EncodeToString([]byte(username))
		encodedPassword := base64.StdEncoding.EncodeToString([]byte(password))

		// Prompt for deployment type
		prompt := promptui.Select{
			Label: "Select Deployment Type",
			Items: []string{"Standalone", "Distributed"},
		}
		_, deploymentType, err := prompt.Run()
		if err != nil {
			log.Fatalf("Prompt failed: %v", err)
		}

		// Define Helm chart configuration based on deployment type
		var chartValues []string
		if deploymentType == "Standalone" {
			chartValues = []string{
				"parseable.image.repository=nikhilsinhaparseable/parseable",
				"parseable.image.tag=debug-issue"}
		} else {
			chartValues = []string{"nikhilsinhaparseable/parseable=debug-issue"}
		}

		// Helm application configuration
		apps := []helm.Helm{
			{
				ReleaseName: "parseable",
				Namespace:   namespace,
				RepoName:    "parseable",
				RepoUrl:     "https://charts.parseable.com",
				ChartName:   "parseable",
				Version:     "1.6.3",
				Values:      chartValues,
			},
		}

		// Generate Kubernetes Secret manifest
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
`, namespace, encodedUsername, encodedPassword, "MC4wLjAuMDo4MDAw", "Li9kYXRh", "Li9zdGFnaW5n")

		// Apply the Kubernetes Secret
		if err := ApplyManifest(secretManifest); err != nil {
			return fmt.Errorf(common.Red+"Failed to create secret: %w"+common.Reset, err)
		}

		// Deploy using Helm
		var wg sync.WaitGroup
		errCh := make(chan error, len(apps))
		for _, app := range apps {
			wg.Add(1)
			go func(app helm.Helm) {
				defer wg.Done()
				log.Printf("Deploying %s in namespace %s...", app.ReleaseName, app.Namespace)
				if err := helm.Apply(app); err != nil {
					log.Printf(common.Red+"Failed to deploy %s: %v"+common.Reset, app.ReleaseName, err)
					errCh <- err
					return
				}
			}(app)
		}

		wg.Wait()
		close(errCh)

		// Check for errors
		for err := range errCh {
			if err != nil {
				return err
			}
		}

		log.Println(common.Green + "Parseable deployed successfully." + common.Reset)
		return nil
	},
}

// ApplyManifest ensures the namespace exists and applies a Kubernetes manifest YAML to the cluster
func ApplyManifest(manifest string) error {
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
			fmt.Printf("Namespace %s created successfully.\n", namespace)
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

	fmt.Println("Manifest applied successfully.")
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

// printBanner displays a welcome banner
func printBanner() {
	banner := `
 --------------------------------------
  Welcome to Parseable OSS Installation
 --------------------------------------
`
	fmt.Println(common.Green + banner + common.Reset)
}
