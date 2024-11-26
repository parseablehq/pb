package cmd

import (
	"fmt"
	"log"
	"os"
	"time"

	internalHTTP "pb/pkg/http"

	"github.com/briandowns/spinner"

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

// Check if the required environment variable is set
func checkEnvVar(key string) bool {
	_, exists := os.LookupEnv(key)
	return exists
}

// Prompt the user to set the environment variable
func promptForEnvVar(key string) {
	var value string
	fmt.Printf(yellow+"Environment variable %s is not set. Please enter its value: "+reset, key)
	fmt.Scanln(&value)
	os.Setenv(key, value)
	fmt.Println(green + "Environment variable set successfully." + reset)
}

var AnalyzeCmd = &cobra.Command{
	Use:     "stream", // Subcommand for "analyze"
	Short:   "Analyze streams in the Parseable server",
	Example: "pb analyze stream <stream-name>",
	Args:    cobra.ExactArgs(1), // Ensure exactly one argument is passed
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		fmt.Printf(yellow+"Analyzing stream: %s\n"+reset, name)

		detectSchema(name)
		// Prompt the user to select LLM
		fmt.Println(yellow + "Select LLM for analysis (1: GPT, 2: Claude): " + reset)
		var choice int
		fmt.Scanln(&choice)

		var llmType string
		switch choice {
		case 1:
			llmType = "GPT"
			if !checkEnvVar("OPENAI_API_KEY") {
				promptForEnvVar("OPENAI_API_KEY")
			}
		case 2:
			llmType = "Claude"
			if !checkEnvVar("CLAUDE_API_KEY") {
				promptForEnvVar("CLAUDE_API_KEY")
			}
		default:
			fmt.Println(red + "Invalid choice. Exiting..." + reset)
			return fmt.Errorf("invalid LLM selection")
		}
		fmt.Printf(green+"Using %s for analysis.\n"+reset, llmType)

		// Initialize spinner
		s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)

		// Step 1: Query Data
		s.Suffix = " Querying data from Parseable server..."
		s.Start()
		client := internalHTTP.DefaultClient(&DefaultProfile)

		query := `with distinct_name as (select distinct(\"involvedObject_name\") as name from \"k8s-events\" where reason ilike '%kill%' or reason ilike '%fail%' or reason ilike '%back%') select reason, message, \"involvedObject_name\", \"involvedObject_namespace\", \"reportingComponent\", timestamp from \"k8s-events\" as t1 join distinct_name t2 on t1.\"involvedObject_name\" = t2.name order by timestamp`

		allData, err := duckdb.QueryPb(&client, query, "2024-11-11T00:00:00+00:00", "2024-11-21T00:00:00+00:00")
		s.Stop()
		if err != nil {
			fmt.Printf(red+"Error querying data in Parseable: %v\n"+reset, err)
			return fmt.Errorf("error querying data in Parseable, err [%w]", err)
		}
		fmt.Println(green + "Data successfully queried from Parseable." + reset)

		// Step 2: Store Data in DuckDB
		s.Suffix = " Storing data in DuckDB..."
		s.Start()
		if err := duckdb.StoreInDuckDB(allData); err != nil {
			s.Stop()
			fmt.Printf(red+"Error storing data in DuckDB: %v\n"+reset, err)
			return fmt.Errorf("error storing data in DuckDB, err [%w]", err)
		}
		s.Stop()
		fmt.Println(green + "Data successfully stored in DuckDB." + reset)

		// Step 3: Prompt Kubernetes Context
		_, err = k8s.PromptK8sContext()
		if err != nil {
			fmt.Printf(red+"Error prompting Kubernetes context: %v\n"+reset, err)
			return err
		}

		// Step 4: Select Namespace
		namespace, err := k8s.PromptNamespace(k8s.GetKubeClient())
		if err != nil {
			log.Fatalf(red+"Error selecting namespace: %v\n"+reset, err)
		}
		fmt.Printf(yellow+"Selected Namespace: %s\n"+reset, namespace)

		// Step 5: Select Pod
		pod, err := k8s.PromptPod(k8s.GetKubeClient(), namespace)
		if err != nil {
			log.Fatalf(red+"Error selecting pod: %v\n"+reset, err)
		}
		fmt.Printf(yellow+"Selected Pod: %s\n"+reset, pod)

		// Step 6: Fetch Events from DuckDB
		s.Suffix = " Fetching pod events from DuckDB..."
		s.Start()
		result, err := duckdb.FetchPodEventsfromDb(pod)
		s.Stop()
		if err != nil {
			fmt.Printf(red+"Error fetching pod events from DuckDB: %v\n"+reset, err)
			return err
		}

		// Step 7: Analyze with GPT
		s.Suffix = " Analyzing events with GPT..."
		s.Start()
		gptResponse, err := openai.AnalyzeEventsWithGPT(pod, namespace, result)
		s.Stop()
		if err != nil {
			fmt.Printf(red+"Failed to analyze events with GPT: %v\n"+reset, err)
			return fmt.Errorf("failed to analyze events with GPT: %w", err)
		}

		// Display GPT Analysis Result
		fmt.Println(green + "\nGPT Analysis:\n" + reset + gptResponse)

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
