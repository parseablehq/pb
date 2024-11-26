package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/manifoldco/promptui"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// promptPod prompts the user to select a pod from the chosen namespace
func PromptPod(clientset *kubernetes.Clientset, namespace string) (string, error) {
	// Retrieve the list of pods in the specified namespace
	pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to retrieve pods in namespace %s: %v", namespace, err)
	}

	// Extract pod names
	var podNames []string
	for _, pod := range pods.Items {
		podNames = append(podNames, pod.Name)
	}

	if len(podNames) == 0 {
		return "", fmt.Errorf("no pods found in namespace %s", namespace)
	}

	// Prompt user to select a pod
	prompt := promptui.Select{
		Label: fmt.Sprintf("\033[32mSelect a Pod in Namespace '%s':\033[0m", namespace),
		Items: podNames,
		Templates: &promptui.SelectTemplates{
			Active:   "\033[33m▸ {{ . }}\033[0m",
			Inactive: "{{ . }}",
			Selected: "\033[32mPod '{{ . }}' selected successfully.\033[0m",
		},
	}

	_, selectedPod, err := prompt.Run()
	if err != nil {
		return "", fmt.Errorf("pod selection cancelled: %v", err)
	}

	return selectedPod, nil
}

// promptK8sContext retrieves Kubernetes contexts from kubeconfig.
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
		Label: "\033[32mSelect your Kubernetes context:\033[0m",
		Items: contexts,
		Templates: &promptui.SelectTemplates{
			Active:   "\033[33m▸ {{ . }}\033[0m", // Yellow arrow and context name for active selection
			Inactive: "{{ . }}",                  // Default color for inactive items
			Selected: "\033[32mKubernetes context '{{ . }}' selected successfully.\033[0m",
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

func GetKubeClient() *kubernetes.Clientset {
	var kubeconfig string
	path, ok := os.LookupEnv("KUBECONFIG")
	if ok {
		kubeconfig = path
	} else {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	return clientset
}

// promptNamespace prompts the user to select a namespace from available namespaces
func PromptNamespace(clientset *kubernetes.Clientset) (string, error) {
	// Retrieve the list of namespaces
	namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to retrieve namespaces: %v", err)
	}

	// Extract namespace names
	var namespaceNames []string
	for _, ns := range namespaces.Items {
		namespaceNames = append(namespaceNames, ns.Name)
	}

	// Prompt user to select a namespace
	prompt := promptui.Select{
		Label: "\033[32mSelect your Namespace:\033[0m",
		Items: namespaceNames,
		Templates: &promptui.SelectTemplates{
			Active:   "\033[33m▸ {{ . }}\033[0m", // Yellow arrow and context name for active selection
			Inactive: "{{ . }}",                  // Default color for inactive items
			Selected: "\033[32mNamespace '{{ . }}' selected successfully.\033[0m",
		},
	}

	_, selectedNamespace, err := prompt.Run()
	if err != nil {
		return "", fmt.Errorf("namespace selection cancelled: %v", err)
	}

	return selectedNamespace, nil
}
