package cmd

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	internalHTTP "pb/pkg/http"

	"github.com/briandowns/spinner"
	"github.com/manifoldco/promptui"

	"pb/pkg/analyze/anthropic"
	"pb/pkg/analyze/duckdb"
	"pb/pkg/analyze/k8s"
	"pb/pkg/analyze/openai"

	_ "github.com/marcboeker/go-duckdb"
	"github.com/spf13/cobra"
)

// ANSI escape codes for colors
const (
	yellow = "\033[33m"
	green  = "\033[32m"
	red    = "\033[31m"
	reset  = "\033[0m"
)

// Check if required environment variables are set and valid
func validateLLMConfig() {
	provider, exists := os.LookupEnv("P_LLM_PROVIDER")
	if !exists || (provider != "openai" && provider != "ollama" && provider != "anthropic") {
		log.Fatalf(red + "Error: P_LLM_PROVIDER must be set to one of: openai, ollama, claude\n" + reset)
	}

	_, keyExists := os.LookupEnv("P_LLM_KEY")
	if !keyExists {
		log.Fatalf(red + "Error: P_LLM_KEY must be set\n" + reset)
	}

	if provider == "ollama" {
		_, endpointExists := os.LookupEnv("P_LLM_ENDPOINT")
		if !endpointExists {
			log.Fatalf(red + "Error: P_LLM_ENDPOINT must be set when using ollama as the provider\n" + reset)
		}
	}

	fmt.Printf(green+"Using %s for analysis.\n"+reset, provider)
}

var AnalyzeCmd = &cobra.Command{
	Use:     "stream",
	Short:   "Analyze streams in the Parseable server",
	Example: "pb analyze stream <stream-name>",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		checkAndInstallDuckDB()
		validateLLMConfig()

		llmProvider := os.Getenv("P_LLM_PROVIDER")

		name := args[0]
		fmt.Printf(yellow+"Analyzing stream: %s\n"+reset, name)
		detectSchema(name)

		var wg sync.WaitGroup

		s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
		s.Suffix = fmt.Sprintf(yellow+" Querying data from Parseable server...(%s)", DefaultProfile.URL)
		s.Start()

		client := internalHTTP.DefaultClient(&DefaultProfile)
		query := `with distinct_name as (select distinct(\"involvedObject_name\") as name from \"k8s-events\" where reason ilike '%kill%' or reason ilike '%fail%' or reason ilike '%back%') select reason, message, \"involvedObject_name\", \"involvedObject_namespace\", \"reportingComponent\", timestamp from \"k8s-events\" as t1 join distinct_name t2 on t1.\"involvedObject_name\" = t2.name order by timestamp`

		allData, err := duckdb.QueryPb(&client, query, "2024-11-11T00:00:00+00:00", "2024-11-21T00:00:00+00:00")
		s.Stop()
		if err != nil {
			log.Printf(red+"Error querying data in Parseable: %v\n"+reset, err)
			return fmt.Errorf("error querying data: %w", err)
		}
		fmt.Println(green + "Data successfully queried from Parseable." + reset)

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := duckdb.StoreInDuckDB(allData); err != nil {
				log.Fatalf(red+"Error storing data in DuckDB: %v\n"+reset, err)
			}
			fmt.Println(green + "Data successfully stored in DuckDB." + reset)
		}()
		wg.Wait()

		// Main analysis loop
		for {
			// Kubernetes context prompt
			_, err := k8s.PromptK8sContext()
			if err != nil {
				return fmt.Errorf(red+"Error prompting Kubernetes context: %w\n"+reset, err)
			}

			namespace, err := k8s.PromptNamespace(k8s.GetKubeClient())
			if err != nil {
				return fmt.Errorf(red+"Error selecting namespace: %w\n"+reset, err)
			}
			fmt.Printf(yellow+"Selected Namespace: %s\n"+reset, namespace)

			pod, err := k8s.PromptPod(k8s.GetKubeClient(), namespace)
			if err != nil {
				return fmt.Errorf(red+"Error selecting pod: %w\n"+reset, err)
			}
			fmt.Printf(yellow+"Selected Pod: %s\n"+reset, pod)

			s.Suffix = " Fetching pod events from DuckDB..."
			s.Start()
			result, err := duckdb.FetchPodEventsfromDb(pod)
			s.Stop()
			if err != nil {
				return fmt.Errorf(red+"Error fetching pod events: %w\n"+reset, err)
			}

			// debug for empty results
			if result == nil {
				fmt.Println(yellow + "No results found in DuckDB." + reset)
				return nil
			}

			s.Suffix = " Analyzing events with LLM..."
			s.Start()

			// Declare the variable to store the response
			var gptResponse string

			// Conditional logic to choose which LLM to use
			if llmProvider == "openai" {
				// Use OpenAI's AnalyzeEventsWithGPT function
				gptResponse, err = openai.AnalyzeEventsWithGPT(pod, namespace, result)

				fmt.Println(
					gptResponse,
				)
			} else if llmProvider == "anthropic" {
				// Use Anthropic's AnalyzeEventsWithAnthropic function
				gptResponse, err = anthropic.AnalyzeEventsWithAnthropic(pod, namespace, result)
			} else if llmProvider == "ollama" {
				// Use Ollama's respective function (assuming a similar function exists)
				//gptResponse, err = ollama.AnalyzeEventsWithOllama(pod, namespace, result)
			} else {
				// This should never happen since validateLLMConfig ensures the provider is valid
				return fmt.Errorf("invalid LLM provider: %s", llmProvider)
			}

			s.Stop()
			// Handle errors from the LLM analysis
			if err != nil {
				return fmt.Errorf(red+"Failed to analyze events: %w\n"+reset, err)
			}
			// Display results using pager
			shouldContinue := parseAndSelectAnalysis(gptResponse)
			if !shouldContinue {
				break // Exit the loop if "no"
			}

		}
		return nil
	},
}

// Dummy function to simulate schema detection
func detectSchema(streamName string) {
	// Simulate schema detection
	fmt.Printf(yellow+"Starting schema detection for stream: %s\n"+reset, streamName)

	// Dummy condition to check if the schema is known
	if streamName == "k8s-events" {
		fmt.Println(green + "Kubernetes events schema found. Schema is known to the tool.\n" + reset)
	} else {
		fmt.Println(red + "Schema not recognized. Please ensure it's defined in the tool.\n" + reset)
	}
}

func checkAndInstallDuckDB() {
	// Check if DuckDB is already installed
	if _, err := exec.LookPath("duckdb"); err == nil {
		fmt.Println(green + "DuckDB is already installed." + reset)
		return
	}

	fmt.Println(yellow + "DuckDB is not installed. Installing..." + reset)

	// Define the download URL based on the OS
	var url, binaryName string
	if runtime.GOOS == "linux" {
		url = "https://github.com/duckdb/duckdb/releases/download/v1.1.3/duckdb_cli-linux-amd64.zip"
		binaryName = "duckdb"
	} else if runtime.GOOS == "darwin" {
		url = "https://github.com/duckdb/duckdb/releases/download/v1.1.3/duckdb_cli-osx-universal.zip"
		binaryName = "duckdb"
	} else {
		fmt.Println(red + "Unsupported OS." + reset)
		os.Exit(1)
	}

	// Download the DuckDB ZIP file
	zipFilePath := "/tmp/duckdb_cli.zip"
	err := downloadFile(zipFilePath, url)
	if err != nil {
		fmt.Printf(red+"Failed to download DuckDB: %v\n"+reset, err)
		os.Exit(1)
	}

	// Extract the ZIP file
	extractPath := "/tmp/duckdb_extracted"
	err = unzip(zipFilePath, extractPath)
	if err != nil {
		fmt.Printf(red+"Failed to extract DuckDB: %v\n"+reset, err)
		os.Exit(1)
	}

	// Move the binary to /usr/local/bin
	finalPath := "/usr/local/bin/duckdb"
	err = os.Rename(filepath.Join(extractPath, binaryName), finalPath)
	if err != nil {
		fmt.Printf(red+"Failed to install DuckDB: %v\n"+reset, err)
		os.Exit(1)
	}

	// Ensure the binary is executable
	if err := os.Chmod(finalPath, 0755); err != nil {
		fmt.Printf(red+"Failed to set permissions on DuckDB: %v\n"+reset, err)
		os.Exit(1)
	}

	fmt.Println(green + "DuckDB successfully installed." + reset)
}

// downloadFile downloads a file from the given URL
func downloadFile(filepath string, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// unzip extracts a ZIP file to the specified destination
func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		extractedFilePath := filepath.Join(dest, f.Name)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()

		// Create directories if needed
		if f.FileInfo().IsDir() {
			os.MkdirAll(extractedFilePath, os.ModePerm)
		} else {
			os.MkdirAll(filepath.Dir(extractedFilePath), os.ModePerm)
			outFile, err := os.Create(extractedFilePath)
			if err != nil {
				return err
			}
			defer outFile.Close()

			_, err = io.Copy(outFile, rc)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

type AnalysisResponse struct {
	Summary           string   `json:"summary"`
	RootCauseAnalysis string   `json:"root_cause_analysis"`
	MitigationSteps   []string `json:"mitigation_steps"`
}

// In parseAndSelectAnalysis, modify to handle user decision to continue or not.
func parseAndSelectAnalysis(response string) bool {
	var analysis AnalysisResponse
	err := json.Unmarshal([]byte(response), &analysis)
	if err != nil {
		log.Println("Failed to parse LLM response: %v", err)
	}

	// Display the summary by default
	fmt.Println(green + "Summary:\n" + reset + analysis.Summary)

	// Prompt the user to choose between "Root Cause Analysis" and "Mitigation Steps"
	initialPrompt := promptui.Select{
		Label: "Select Analysis to View",
		Items: []string{"Root Cause Analysis", "Mitigation Steps", "Analyze another pod in namespace (yes/no)"},
		Size:  3,
	}

	_, choice, err := initialPrompt.Run()
	if err != nil {
		log.Fatalf("Prompt failed: %v", err)
	}

	switch choice {
	case "Root Cause Analysis":
		// Show Root Cause Analysis
		fmt.Println(green + "Root Cause Analysis:\n" + reset + analysis.RootCauseAnalysis)

		// Now prompt the user to choose between "Mitigation" or "Pods"
		secondPrompt := promptui.Select{
			Label: "What would you like to do next?",
			Items: []string{"Mitigation", "Analyze another pod in namespace (yes/no)"},
			Size:  3,
		}

		_, secondChoice, err := secondPrompt.Run()
		if err != nil {
			log.Fatalf("Prompt failed: %v", err)
		}

		switch secondChoice {
		case "Mitigation":
			// Show Mitigation Steps
			fmt.Println(green + "Mitigation Steps:\n" + reset)
			for i, step := range analysis.MitigationSteps {
				fmt.Printf("%d. %s\n", i+1, step)
			}

			// After displaying mitigation steps, ask if the user wants to analyze another pod/namespace
			prompt := promptui.Prompt{
				Label:   "Analyze another namespace/pod (yes/no)",
				Default: "no",
			}
			choice, _ := prompt.Run()
			if strings.ToLower(choice) != "yes" {
				return false // Exit the loop if "no"
			}
		case "Analyze another pod in namespace (yes/no)":
			prompt := promptui.Prompt{
				Label:   "Analyze another namespace/pod (yes/no)",
				Default: "no",
			}
			choice, _ := prompt.Run()
			if strings.ToLower(choice) != "yes" {
				return false // Exit the loop if "no"
			}
		}

	case "Mitigation Steps":
		// Show Mitigation Steps directly
		fmt.Println(green + "Mitigation Steps:\n" + reset)
		for i, step := range analysis.MitigationSteps {
			fmt.Printf("%d. %s\n", i+1, step)
		}

		// After displaying mitigation steps, ask if the user wants to analyze another pod/namespace
		prompt := promptui.Prompt{
			Label:   "Analyze another namespace/pod (yes/no)",
			Default: "no",
		}
		choice, _ := prompt.Run()
		if strings.ToLower(choice) != "yes" {
			return false // Exit the loop if "no"
		}
	}

	return true // Continue the loop if "yes"
}
