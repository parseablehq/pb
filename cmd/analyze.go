package cmd

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// Check if required environment variables are set and valid
func validateLLMConfig() {
	provider, exists := os.LookupEnv("P_LLM_PROVIDER")
	if !exists || (provider != "openai" && provider != "ollama" && provider != "claude") {
		fmt.Printf(red + "Error: P_LLM_PROVIDER must be set to one of: openai, ollama, claude\n" + reset)
		os.Exit(1)
	}

	_, keyExists := os.LookupEnv("P_LLM_KEY")
	if !keyExists {
		fmt.Printf(red + "Error: P_LLM_KEY must be set\n" + reset)
		os.Exit(1)
	}

	if provider == "ollama" {
		_, endpointExists := os.LookupEnv("P_LLM_ENDPOINT")
		if !endpointExists {
			fmt.Printf(red + "Error: P_LLM_ENDPOINT must be set when using ollama as the provider\n" + reset)
			os.Exit(1)
		}
	}

	fmt.Printf(green+"Using %s for analysis.\n"+reset, provider)
}

var AnalyzeCmd = &cobra.Command{
	Use:     "stream", // Subcommand for "analyze"
	Short:   "Analyze streams in the Parseable server",
	Example: "pb analyze stream <stream-name>",
	Args:    cobra.ExactArgs(1), // Ensure exactly one argument is passed
	RunE: func(cmd *cobra.Command, args []string) error {

		// Check and install DuckDB if necessary
		checkAndInstallDuckDB()

		// Validate LLM environment variables
		validateLLMConfig()

		name := args[0]
		fmt.Printf(yellow+"Analyzing stream: %s\n"+reset, name)

		detectSchema(name)

		// Initialize spinner
		s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)

		// Step 1: Query Data
		s.Suffix = fmt.Sprintf(yellow+" Querying data from Parseable server...(%s)", DefaultProfile.URL)
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

		// Step 7: Analyze with LLM
		s.Suffix = " Analyzing events with LLM..."
		s.Start()
		gptResponse, err := openai.AnalyzeEventsWithGPT(pod, namespace, result)
		s.Stop()
		if err != nil {
			fmt.Printf(red+"Failed to analyze events with LLM: %v\n"+reset, err)
			return fmt.Errorf("failed to analyze events with LLM: %w", err)
		}

		// Display Analysis Result
		fmt.Println(green + "\nLLM Analysis:\n" + reset + gptResponse)

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
