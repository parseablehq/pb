package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	internalHTTP "pb/pkg/http"

	"github.com/spf13/cobra"
)

var GenerateSchemaCmd = &cobra.Command{
	Use:     "schema",
	Short:   "Generate Schema for JSON",
	Example: "pb generate schema --file=test.json",
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Get the file path from the `--file` flag
		filePath, err := cmd.Flags().GetString("file")
		if err != nil {
			return fmt.Errorf("failed to read file flag: %w", err)
		}

		if filePath == "" {
			return fmt.Errorf("file flag is required")
		}

		// Read the file content
		fileContent, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", filePath, err)
		}

		// Initialize HTTP client
		client := internalHTTP.DefaultClient(&DefaultProfile)

		// Create the HTTP request
		req, err := client.NewRequest("POST", "/logstream/schema/detect", bytes.NewBuffer(fileContent))
		if err != nil {
			return fmt.Errorf("failed to create new request: %w", err)
		}

		// Set Content-Type header
		req.Header.Set("Content-Type", "application/json")

		// Execute the request
		resp, err := client.Client.Do(req)
		if err != nil {
			return fmt.Errorf("request execution failed: %w", err)
		}
		defer resp.Body.Close()

		// Check for non-200 status codes
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			fmt.Printf("Error response: %s\n", string(body))
			return fmt.Errorf("non-200 status code received: %s", resp.Status)
		}

		// Parse and print the response
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
		}

		var prettyJSON bytes.Buffer
		if err := json.Indent(&prettyJSON, respBody, "", "  "); err != nil {
			return fmt.Errorf("failed to format response as JSON: %w", err)
		}

		fmt.Println(prettyJSON.String())
		return nil
	},
}

func init() {
	// Add the `--file` flag to the command
	GenerateSchemaCmd.Flags().StringP("file", "f", "", "Path to the JSON file to generate schema")
}
