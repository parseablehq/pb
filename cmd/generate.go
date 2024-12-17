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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"pb/pkg/common"
	internalHTTP "pb/pkg/http"

	"github.com/spf13/cobra"
)

const (
	generateStaticSchemaPath = "/logstream/schema/detect"
)

var GenerateSchemaCmd = &cobra.Command{
	Use:     "generate",
	Short:   "Generate Schema for JSON",
	Example: "pb schema generate --file=test.json",
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Get the file path from the `--file` flag
		filePath, err := cmd.Flags().GetString("file")
		if err != nil {
			return fmt.Errorf(common.Red+"failed to read file flag: %w"+common.Reset, err)
		}

		if filePath == "" {
			return fmt.Errorf(common.Red + "file flag is required" + common.Reset)
		}

		// Read the file content
		fileContent, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf(common.Red+"failed to read file %s: %w"+common.Reset, filePath, err)
		}

		// Initialize HTTP client
		client := internalHTTP.DefaultClient(&DefaultProfile)

		// Create the HTTP request
		req, err := client.NewRequest(http.MethodPost, generateStaticSchemaPath, bytes.NewBuffer(fileContent))
		if err != nil {
			return fmt.Errorf(common.Red+"failed to create new request: %w"+common.Reset, err)
		}

		// Set Content-Type header
		req.Header.Set("Content-Type", "application/json")

		// Execute the request
		resp, err := client.Client.Do(req)
		if err != nil {
			return fmt.Errorf(common.Red+"request execution failed: %w"+common.Reset, err)
		}
		defer resp.Body.Close()

		// Check for non-200 status codes
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			fmt.Printf(common.Red+"Error response: %s\n"+common.Reset, string(body))
			return fmt.Errorf(common.Red+"non-200 status code received: %s"+common.Reset, resp.Status)
		}

		// Parse and print the response
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf(common.Red+"failed to read response body: %w"+common.Reset, err)
		}

		var prettyJSON bytes.Buffer
		if err := json.Indent(&prettyJSON, respBody, "", "  "); err != nil {
			return fmt.Errorf(common.Red+"failed to format response as JSON: %w"+common.Reset, err)
		}

		fmt.Println(common.Green + prettyJSON.String() + common.Reset)
		return nil
	},
}

var CreateSchemaCmd = &cobra.Command{
	Use:     "create",
	Short:   "Create Schema for a Parseable stream",
	Example: "pb schema create --stream=my_stream --file=schema.json",
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Get the stream name from the `--stream` flag
		streamName, err := cmd.Flags().GetString("stream")
		if err != nil {
			return fmt.Errorf(common.Red+"failed to read stream flag: %w"+common.Reset, err)
		}

		if streamName == "" {
			return fmt.Errorf(common.Red + "stream flag is required" + common.Reset)
		}

		// Get the file path from the `--file` flag
		filePath, err := cmd.Flags().GetString("file")
		if err != nil {
			return fmt.Errorf(common.Red+"failed to read config flag: %w"+common.Reset, err)
		}

		if filePath == "" {
			return fmt.Errorf(common.Red + "file path flag is required" + common.Reset)
		}

		// Read the JSON schema file
		schemaContent, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf(common.Red+"failed to read schema file %s: %w"+common.Reset, filePath, err)
		}

		// Initialize HTTP client
		client := internalHTTP.DefaultClient(&DefaultProfile)

		// Construct the API path
		apiPath := fmt.Sprintf("/logstream/%s", streamName)

		// Create the HTTP PUT request
		req, err := client.NewRequest(http.MethodPut, apiPath, bytes.NewBuffer(schemaContent))
		if err != nil {
			return fmt.Errorf(common.Red+"failed to create new request: %w"+common.Reset, err)
		}

		// Set custom headers
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-P-Static-Schema-Flag", "true")

		// Execute the request
		resp, err := client.Client.Do(req)
		if err != nil {
			return fmt.Errorf(common.Red+"request execution failed: %w"+common.Reset, err)
		}
		defer resp.Body.Close()

		// Check for non-200 status codes
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			fmt.Printf(common.Red+"Error response: %s\n"+common.Reset, string(body))
			return fmt.Errorf(common.Red+"non-200 status code received: %s"+common.Reset, resp.Status)
		}

		// Parse and print the response
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf(common.Red+"failed to read response body: %w"+common.Reset, err)
		}

		fmt.Println(common.Green + string(respBody) + common.Reset)
		return nil
	},
}

func init() {
	// Add the `--file` flag to the command
	GenerateSchemaCmd.Flags().StringP("file", "f", "", "Path to the JSON file to generate schema")
	CreateSchemaCmd.Flags().StringP("stream", "s", "", "Name of the stream to associate with the schema")
	CreateSchemaCmd.Flags().StringP("file", "f", "", "Path to the JSON file to create schema")
}
