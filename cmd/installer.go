package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"pb/pkg/common"
	"pb/pkg/helm"
	"pb/pkg/installer"

	"github.com/briandowns/spinner"
	"github.com/spf13/cobra"
)

var verbose bool

var InstallOssCmd = &cobra.Command{
	Use:     "oss",
	Short:   "Deploy Parseable OSS",
	Example: "pb install oss",
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Add verbose flag
		cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")

		// Print the banner
		printBanner()

		// Prompt user to select a deployment plan
		selectedPlan, err := installer.PromptUserPlanSelection()
		if err != nil {
			return err
		}

		fmt.Printf(
			common.Cyan+"  Ingestion Speed: %s\n"+
				common.Cyan+"  Per Day Ingestion: %s\n"+
				common.Cyan+"  Query Performance: %s\n"+
				common.Cyan+"  CPU & Memory: %s\n"+
				common.Reset, selectedPlan.IngestionSpeed, selectedPlan.PerDayIngestion,
			selectedPlan.QueryPerformance, selectedPlan.CPUAndMemorySpecs)

		// Get namespace and chart values from installer
		valuesHolder, chartValues := installer.Installer(selectedPlan)

		// Helm application configuration
		apps := []helm.Helm{
			{
				ReleaseName: "parseable",
				Namespace:   valuesHolder.ParseableSecret.Namespace,
				RepoName:    "parseable",
				RepoURL:     "https://charts.parseable.com",
				ChartName:   "parseable",
				Version:     "1.6.5",
				Values:      chartValues,
			},
		}

		// Create a spinner
		spinner := createDeploymentSpinner(valuesHolder.ParseableSecret.Namespace)

		// Redirect standard output if not in verbose mode
		var oldStdout *os.File
		if !verbose {
			oldStdout = os.Stdout
			_, w, _ := os.Pipe()
			os.Stdout = w
		}

		spinner.Start()

		// Deploy using Helm
		var wg sync.WaitGroup
		errCh := make(chan error, len(apps))
		for _, app := range apps {
			wg.Add(1)
			go func(app helm.Helm) {
				defer wg.Done()
				if err := helm.Apply(app, verbose); err != nil {
					errCh <- err
					return
				}
			}(app)
		}

		wg.Wait()
		close(errCh)

		// Stop the spinner and restore stdout
		spinner.Stop()
		if !verbose {
			os.Stdout = oldStdout
		}

		// Check for errors
		for err := range errCh {
			if err != nil {
				return err
			}
		}

		// Print success banner
		printSuccessBanner(valuesHolder.ParseableSecret.Namespace, string(valuesHolder.DeploymentType), apps[0].Version, valuesHolder.ParseableSecret.Username, valuesHolder.ParseableSecret.Password)

		return nil
	},
}

// printSuccessBanner remains the same as in the original code
func printSuccessBanner(namespace, deployment, version, username, password string) {
	var ingestionUrl, serviceName string
	if deployment == "standalone" {
		ingestionUrl = "parseable." + namespace + ".svc.cluster.local"
		serviceName = "parseable"
	} else if deployment == "distributed" {
		ingestionUrl = "parseable-ingestor-svc." + namespace + ".svc.cluster.local"
		serviceName = "parseable-query-svc"
	}

	// Encode credentials to Base64
	credentials := map[string]string{
		"username": username,
		"password": password,
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
	fmt.Printf("  • Namespace:        %s\n", common.Blue+namespace)
	fmt.Printf("  • Chart Version:    %s\n", common.Blue+version)
	fmt.Printf("  • Ingestion URL:    %s\n", ingestionUrl)

	fmt.Println("\n" + common.Blue + "🔗  Resources:" + common.Reset)
	fmt.Println(common.Blue + "  • Documentation:   https://www.parseable.com/docs/server/introduction")
	fmt.Println(common.Blue + "  • Stream Management: https://www.parseable.com/docs/server/api")

	fmt.Println("\n" + common.Blue + "Happy Logging!" + common.Reset)

	// Port-forward the service
	localPort := "8000"
	fmt.Printf(common.Green+"Port-forwarding %s service on port %s...\n"+common.Reset, serviceName, localPort)

	if err = startPortForward(namespace, serviceName, "80", localPort); err != nil {
		fmt.Sprintf(common.Red+"failed to port-forward service: %w", err)
	}

	// Redirect to UI
	localURL := fmt.Sprintf("http://localhost:%s/login?q=%s", localPort, base64EncodedString)
	fmt.Printf(common.Green+"Opening Parseable UI at %s\n"+common.Reset, localURL)
	openBrowser(localURL)
}

func createDeploymentSpinner(namespace string) *spinner.Spinner {
	// Custom spinner with multiple character sets for dynamic effect
	spinnerChars := []string{
		"●", "○", "◉", "○", "◉", "○", "◉", "○", "◉",
	}

	s := spinner.New(
		spinnerChars,
		120*time.Millisecond,
		spinner.WithColor(common.Yellow),
		spinner.WithSuffix(" ..."),
	)

	s.Prefix = fmt.Sprintf(common.Yellow+"Deploying to %s ", namespace)

	return s
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

func startPortForward(namespace, serviceName, remotePort, localPort string) error {
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
