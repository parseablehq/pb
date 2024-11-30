package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

// ProxyCmd proxies Parseable deployment
var ProxyCmd = &cobra.Command{
	Use:     "proxy",
	Short:   "Proxy Parseable deployment",
	Example: "pb proxy",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Define the kubectl command to forward ports
		cmdArgs := []string{"port-forward", "svc/parseable", "8000:80", "-n", "parseable"}

		// Run the kubectl port-forward command
		kubectlCmd := exec.Command("kubectl", cmdArgs...)
		kubectlCmd.Stdout = os.Stdout // Forward kubectl's stdout to the terminal
		kubectlCmd.Stderr = os.Stderr // Forward kubectl's stderr to the terminal

		// Start the kubectl port-forward process
		if err := kubectlCmd.Start(); err != nil {
			return fmt.Errorf("failed to start kubectl port-forward: %w", err)
		}

		// Wait for the command to complete (this will block until the user terminates it)
		if err := kubectlCmd.Wait(); err != nil {
			return fmt.Errorf("kubectl port-forward process ended with error: %w", err)
		}

		return nil
	},
}
