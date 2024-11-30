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

	"encoding/json"

	"log"
	"os"
	"strings"
	"sync"
	"time"

	"pb/pkg/common"
	internalHTTP "pb/pkg/http"

	"github.com/briandowns/spinner"
	"github.com/manifoldco/promptui"

	"pb/pkg/analyze/anthropic"
	"pb/pkg/analyze/duckdb"
	"pb/pkg/analyze/k8s"
	"pb/pkg/analyze/ollama"
	"pb/pkg/analyze/openai"
	"pb/pkg/analyze/pdf"

	_ "github.com/marcboeker/go-duckdb"
	"github.com/spf13/cobra"
)

// Check if required environment variables are set and valid
func validateLLMConfig() {
	provider, exists := os.LookupEnv("P_LLM_PROVIDER")
	if !exists || (provider != "openai" && provider != "ollama" && provider != "anthropic") {
		log.Fatalf(common.Red + "Error: P_LLM_PROVIDER must be set to one of: openai, ollama, claude\n" + common.Reset)
	}

	_, keyExists := os.LookupEnv("P_LLM_KEY")
	if !keyExists {
		log.Fatalf(common.Red + "Error: P_LLM_KEY must be set\n" + common.Reset)
	}

	if provider == "ollama" {
		_, endpointExists := os.LookupEnv("P_LLM_ENDPOINT")
		if !endpointExists {
			log.Fatalf(common.Red + "Error: P_LLM_ENDPOINT must be set when using ollama as the provider\n" + common.Reset)
		}
	}

	fmt.Printf(common.Green+"Using %s for analysis.\n"+common.Reset, provider)
}

var AnalyzeCmd = &cobra.Command{
	Use:     "stream",
	Short:   "Analyze streams in the Parseable server",
	Example: "pb analyze stream <stream-name>",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		duckdb.CheckAndInstallDuckDB()
		validateLLMConfig()

		llmProvider := os.Getenv("P_LLM_PROVIDER")

		name := args[0]
		fmt.Printf(common.Yellow+"Analyzing stream: %s\n"+common.Reset, name)
		detectSchema(name)

		var wg sync.WaitGroup

		s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
		s.Suffix = fmt.Sprintf(common.Yellow+" Querying data from Parseable server...(%s)", DefaultProfile.URL)
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
			log.Printf(common.Red+"Error querying data in Parseable: %v\n"+common.Reset, err)
			return fmt.Errorf("error querying data: %w", err)
		}
		fmt.Println(common.Green + "Data successfully queried from Parseable." + common.Reset)

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := duckdb.StoreInDuckDB(allData); err != nil {
				log.Fatalf(common.Red+"Error storing data in DuckDB: %v\n"+common.Reset, err)
			}
			fmt.Println(common.Green + "Data successfully stocommon.Red in DuckDB." + common.Reset)
		}()
		wg.Wait()

		// Main analysis loop
		for {
			// Kubernetes context prompt
			_, err := k8s.PromptK8sContext()
			if err != nil {
				return fmt.Errorf(common.Red+"Error prompting Kubernetes context: %w\n"+common.Reset, err)
			}

			namespace, err := k8s.PromptNamespace(k8s.GetKubeClient())
			if err != nil {
				return fmt.Errorf(common.Red+"Error selecting namespace: %w\n"+common.Reset, err)
			}
			fmt.Printf(common.Yellow+"Selected Namespace: %s\n"+common.Reset, namespace)

			pod, err := k8s.PromptPod(k8s.GetKubeClient(), namespace)
			if err != nil {
				return fmt.Errorf(common.Red+"Error selecting pod: %w\n"+common.Reset, err)
			}
			fmt.Printf(common.Yellow+"Selected Pod: %s\n"+common.Reset, pod)

			s.Suffix = " Fetching pod events from DuckDB..."
			s.Start()
			result, err := duckdb.FetchPodEventsfromDb(pod)
			s.Stop()
			if err != nil {
				return fmt.Errorf(common.Red+"Error fetching pod events: %w\n"+common.Reset, err)
			}

			// debug for empty results
			if result == nil {
				fmt.Println(common.Yellow + "No results found for pod in DuckDB, analyzing the namespace." + common.Reset)
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
				return fmt.Errorf(common.Red+"Failed to analyze events: %w\n"+common.Reset, err)
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
	fmt.Printf(common.Yellow+"Starting schema detection for stream: %s\n"+common.Reset, streamName)

	// Dummy condition to check if the schema is known
	if streamName == "k8s-events" {
		fmt.Println(common.Green + "Kubernetes events schema found. Schema is known to the tool.\n" + common.Reset)
	} else {
		fmt.Println(common.Red + "Schema not recognized. Please ensure it's defined in the tool.\n" + common.Reset)
	}
}

func parseAndSelectAnalysis(response, namespace, pod string) bool {
	type AnalysisResponse struct {
		Summary           string   `json:"summary"`
		RootCauseAnalysis string   `json:"root_cause_analysis"`
		MitigationSteps   []string `json:"mitigation_steps"`
	}

	var analysis AnalysisResponse
	err := json.Unmarshal([]byte(response), &analysis)
	if err != nil {
		fmt.Println(common.Green + "Summary:\n" + common.Reset + response)
		return true
	}

	// Start with Summary
	currentView := "Summary"

	for {
		switch currentView {
		case "Summary":
			fmt.Println(common.Green + "Summary:\n" + common.Reset + analysis.Summary)
			currentView = promptNextStep([]string{"Root Cause Analysis", "Mitigation Steps", "Generate Postmortem Report", "Analyze another pod in namespace"})

		case "Root Cause Analysis":
			fmt.Println(common.Green + "Root Cause Analysis:\n" + common.Reset + analysis.RootCauseAnalysis)
			currentView = promptNextStep([]string{"Mitigation Steps", "Generate Postmortem Report", "Analyze another pod in namespace", "Summary"})

		case "Mitigation Steps":
			fmt.Println(common.Green + "Mitigation Steps:\n" + common.Reset)
			for i, step := range analysis.MitigationSteps {
				fmt.Printf("%d. %s\n", i+1, step)
			}
			currentView = promptNextStep([]string{"Root Cause Analysis", "Generate Postmortem Report", "Analyze another pod in namespace", "Summary"})

		case "Generate Postmortem Report":
			pdf.CreatePDF(analysis.Summary, analysis.RootCauseAnalysis, strings.Join(analysis.MitigationSteps, "\n"), namespace, pod)
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

// Helper function to ask about analyzing another pod/namespace
func askToAnalyzeAnotherPod() bool {
	prompt := promptui.Prompt{
		Label:   "Analyze another namespace/pod (yes/no)",
		Default: "no",
	}
	choice, _ := prompt.Run()
	return strings.ToLower(choice) == "yes"
}
