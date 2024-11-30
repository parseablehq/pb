package cmd

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"pb/pkg/analyze/k8s"
	"pb/pkg/helm"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

var InstallOssCmd = &cobra.Command{
	Use:     "oss",
	Short:   "Deploy Parseable OSS",
	Example: "pb install oss",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := k8s.PromptK8sContext()
		if err != nil {
			return fmt.Errorf(red+"Error prompting Kubernetes context: %w"+reset, err)
		}

		// Prompt user to enter a namespace with yellow color
		fmt.Print(yellow + "Enter the Kubernetes namespace for deployment: " + reset)
		reader := bufio.NewReader(os.Stdin)
		namespace, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf(red+"Error reading namespace input: %w"+reset, err)
		}

		// Trim newline character and spaces from the input
		namespace = strings.TrimSpace(namespace)

		apps := []helm.Helm{
			{
				ReleaseName: "parseable",
				Namespace:   namespace,
				RepoName:    "parseable",
				RepoUrl:     "https://charts.parseable.com",
				ChartName:   "parseable",
				Version:     "1.6.3",
				Values:      []string{"parseable.image.tag=v1.6.2"},
			},
		}

		// Create a WaitGroup to manage Go routines
		var wg sync.WaitGroup

		// Use a channel to capture errors from Go routines
		errCh := make(chan error, len(apps))

		for _, app := range apps {
			wg.Add(1)
			go func(app helm.Helm) {
				defer wg.Done() // Mark this Go routine as done when it finishes
				log.Printf("Deploying %s in namespace %s...", app.ReleaseName, app.Namespace)
				if err := helm.Apply(app); err != nil {
					log.Printf(red+"Failed to deploy %s: %v"+reset, app.ReleaseName, err)
					errCh <- err // Send the error to the channel
					return
				}
				log.Printf(green+"%s deployed successfully."+reset, app.ReleaseName)
			}(app) // Pass the app variable to the closure to avoid capturing issues
		}

		// Wait for all Go routines to complete
		wg.Wait()
		close(errCh) // Close the error channel after all routines finish

		// Check for errors from Go routines
		for err := range errCh {
			if err != nil {
				return err // Return the first error encountered
			}
		}

		log.Println(green + "Parseable deployed successfully." + reset)
		return nil
	},
}
