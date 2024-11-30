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

package cmd

import (
	"fmt"

	"github.com/jung-kurt/gofpdf"

	"archive/zip"
	"encoding/json"

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
	"pb/pkg/analyze/ollama"
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
		query := `with distinct_name as (select distinct(\"involvedObject_name\") as name from \"k8s-events\") select reason, message, \"involvedObject_name\", \"involvedObject_namespace\", \"reportingComponent\", timestamp from \"k8s-events\" as t1 join distinct_name t2 on t1.\"involvedObject_name\" = t2.name order by timestamp`

		endTime := time.Now().UTC()
		startTime := endTime.Add(-5 * time.Hour)

		// Format the timestamps for the query
		startTimeStr := startTime.Format(time.RFC3339)
		endTimeStr := endTime.Format(time.RFC3339)

		allData, err := duckdb.QueryPb(&client, query, startTimeStr, endTimeStr)

		if allData == "" {
			return fmt.Errorf("error no data found")
		}

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
				fmt.Println(yellow + "No results found for pod in DuckDB, analyzing the namespace." + reset)
				result, err = duckdb.FetchNamespaceEventsfromDb(namespace)
			}

			s.Suffix = " Analyzing events with LLM..."
			s.Start()

			// Declare the variable to store the response
			var gptResponse string

			// Conditional logic to choose which LLM to use
			if llmProvider == "openai" {
				// Use OpenAI's AnalyzeEventsWithGPT function
				gptResponse, err = openai.AnalyzeEventsWithGPT(pod, namespace, result)
			} else if llmProvider == "anthropic" {
				// Use Anthropic's AnalyzeEventsWithAnthropic function
				gptResponse, err = anthropic.AnalyzeEventsWithAnthropic(pod, namespace, result)
			} else if llmProvider == "ollama" {
				// Use Ollama's respective function (assuming a similar function exists)
				gptResponse, err = ollama.AnalyzeEventsWithOllama(pod, namespace, result)
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
			shouldContinue := parseAndSelectAnalysis(gptResponse, namespace, pod)
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

	// Install to the user’s bin directory
	userBinDir, _ := os.UserHomeDir()
	finalPath := filepath.Join(userBinDir, "bin", binaryName)

	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(finalPath), os.ModePerm); err != nil {
		fmt.Printf(red+"Failed to create bin directory: %v\n"+reset, err)
		os.Exit(1)
	}

	// Move the binary to ~/bin
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

	// Add ~/bin to PATH automatically
	addToPath()

	fmt.Println(green + "DuckDB successfully installed in " + finalPath + reset)
}

// addToPath ensures ~/bin is in the user's PATH
func addToPath() {
	shellProfile := ""
	if _, exists := os.LookupEnv("ZSH_VERSION"); exists {
		shellProfile = filepath.Join(os.Getenv("HOME"), ".zshrc")
	} else {
		shellProfile = filepath.Join(os.Getenv("HOME"), ".bashrc")
	}

	// Check if ~/bin is already in the PATH
	data, err := os.ReadFile(shellProfile)
	if err == nil && !strings.Contains(string(data), "export PATH=$PATH:$HOME/bin") {
		// Append the export command
		f, err := os.OpenFile(shellProfile, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Printf(red+"Failed to update PATH: %v\n"+reset, err)
			return
		}
		defer f.Close()

		if _, err := f.WriteString("\nexport PATH=$PATH:$HOME/bin\n"); err != nil {
			fmt.Printf(red+"Failed to write to shell profile: %v\n"+reset, err)
		} else {
			fmt.Println(green + "Updated PATH in " + shellProfile + reset)
		}
	}
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

func parseAndSelectAnalysis(response, namespace, pod string) bool {
	var analysis AnalysisResponse
	err := json.Unmarshal([]byte(response), &analysis)
	if err != nil {
		fmt.Println(green + "Summary:\n" + reset + response)
		return true
	}

	// Start with Summary
	currentView := "Summary"

	for {
		switch currentView {
		case "Summary":
			fmt.Println(green + "Summary:\n" + reset + analysis.Summary)
			currentView = promptNextStep([]string{"Root Cause Analysis", "Mitigation Steps", "Generate Postmortem Report", "Analyze another pod in namespace"})

		case "Root Cause Analysis":
			fmt.Println(green + "Root Cause Analysis:\n" + reset + analysis.RootCauseAnalysis)
			currentView = promptNextStep([]string{"Mitigation Steps", "Generate Postmortem Report", "Analyze another pod in namespace", "Summary"})

		case "Mitigation Steps":
			fmt.Println(green + "Mitigation Steps:\n" + reset)
			for i, step := range analysis.MitigationSteps {
				fmt.Printf("%d. %s\n", i+1, step)
			}
			currentView = promptNextStep([]string{"Root Cause Analysis", "Generate Postmortem Report", "Analyze another pod in namespace", "Summary"})

		case "Generate Postmortem Report":
			createPDF(analysis.Summary, analysis.RootCauseAnalysis, strings.Join(analysis.MitigationSteps, "\n"), namespace, pod)
			return true // Exit after generating report

		case "Analyze another pod in namespace":
			return askToAnalyzeAnotherPod() // Return to main prompt for a new pod
		}
	}
}

// Helper function to handle user prompt and return the selected option
func promptNextStep(options []string) string {
	prompt := promptui.Select{
		Label: "What would you like to do next?",
		Items: options,
		Size:  len(options),
	}

	_, choice, err := prompt.Run()
	if err != nil {
		log.Fatalf("Prompt failed: %v", err)
	}
	return choice
}

// Updated createPDF function
func createPDF(summary, rootCause, mitigation, namespace, pod string) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Arial", "B", 16)

	// Title
	pdf.Cell(40, 10, "Postmortem Report")
	pdf.Ln(12)

	// Add sections
	addSection(pdf, "Summary:", summary)
	addSection(pdf, "Root Cause Analysis:", rootCause)
	addSection(pdf, "Mitigation Steps:", mitigation)

	// Generate filename with timestamp
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("postmortem-%s-%s-%s.pdf", sanitize(namespace), sanitize(pod), timestamp)

	// Save file
	err := pdf.OutputFileAndClose(filename)
	if err != nil {
		fmt.Println("Error generating PDF:", err)
	} else {
		fmt.Printf(green+"Report saved as %s"+reset+"\n", filename)
	}
}

// Helper function to sanitize filenames by replacing invalid characters
func sanitize(input string) string {
	return strings.ReplaceAll(input, "/", "_") // Replace slashes with underscores
}

func addSection(pdf *gofpdf.Fpdf, title, content string) {
	pdf.SetFont("Arial", "B", 14)
	pdf.Cell(0, 10, title)
	pdf.Ln(10)

	pdf.SetFont("Arial", "", 12)
	pdf.MultiCell(0, 10, content, "", "", false)
	pdf.Ln(10)
}

// Helper function to ask about analyzing another pod/namespace
func askToAnalyzeAnotherPod() bool {
	prompt := promptui.Prompt{
		Label:   "Analyze another namespace/pod (yes/no)",
		Default: "no",
	}
	choice, _ := prompt.Run()
	return strings.ToLower(choice) == "yes"
}
