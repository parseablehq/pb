package cmd

import (
	"fmt"
	"os"
	"pb/pkg/common"
	"pb/pkg/helm"
	"pb/pkg/installer"
	"strings"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/spf13/cobra"
)

var (
	verbose bool
)

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

		fmt.Println(common.Green + "You selected the following plan:" + common.Reset)
		fmt.Printf(common.Cyan+"Plan: %s\n"+common.Yellow+"Ingestion Speed: %s\n"+common.Green+"Per Day Ingestion: %s\n"+
			common.Blue+"Query Performance: %s\n"+common.Red+"CPU & Memory: %s\n"+common.Reset,
			selectedPlan.Name, selectedPlan.IngestionSpeed, selectedPlan.PerDayIngestion,
			selectedPlan.QueryPerformance, selectedPlan.CPUAndMemorySpecs)

		// Get namespace and chart values from installer
		namespace, chartValues := installer.Installer(selectedPlan)

		// Helm application configuration
		apps := []helm.Helm{
			{
				ReleaseName: "parseable",
				Namespace:   namespace,
				RepoName:    "parseable",
				RepoURL:     "https://charts.parseable.com",
				ChartName:   "parseable",
				Version:     "1.6.5",
				Values:      chartValues,
			},
		}

		// Create a spinner
		spinner := createDeploymentSpinner(namespace)

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
			//w.Close()
			os.Stdout = oldStdout
		}

		// Check for errors
		for err := range errCh {
			if err != nil {
				return err
			}
		}

		// Print success banner
		printSuccessBanner(namespace, apps[0].Version)

		return nil
	},
}

// printSuccessBanner remains the same as in the original code
func printSuccessBanner(namespace, version string) {
	fmt.Println("\n" + common.Green + "🎉 Parseable Deployment Successful! 🎉" + common.Reset)
	fmt.Println(strings.Repeat("=", 50))

	fmt.Printf("%s Deployment Details:\n", common.Blue+"ℹ️ ")
	fmt.Printf("  • Namespace:        %s\n", common.Blue+namespace)
	fmt.Printf("  • Chart Version:    %s\n", common.Blue+version)

	fmt.Println("\n" + common.Blue + "🔗  Resources:" + common.Reset)
	fmt.Println(common.Blue + "  • Documentation:   https://www.parseable.com/docs/server/introduction")
	fmt.Println(common.Blue + "  • Stream Management: https://www.parseable.com/docs/server/api")

	fmt.Println("\n" + common.Blue + "Happy Logging!" + common.Reset)
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
