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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	internalHTTP "pb/pkg/http"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

// DatasetStatsData is the data structure for dataset stats
type DatasetStatsData struct {
	Ingestion struct {
		Count  int    `json:"count"`
		Format string `json:"format"`
		Size   uint64 `json:"size"`
	} `json:"ingestion"`
	Storage struct {
		Format string `json:"format"`
		Size   uint64 `json:"size"`
	} `json:"storage"`
	Stream string    `json:"stream"`
	Time   time.Time `json:"time"`
}

type DatasetListItem struct {
	Name string
}

func (item *DatasetListItem) Render() string {
	render := StandardStyle.Render(item.Name)
	return ItemOuter.Render(render)
}

// DatasetRetentionData is the data structure for dataset retention
type DatasetRetentionData []struct {
	Description string `json:"description"`
	Action      string `json:"action"`
	Duration    string `json:"duration"`
}

// AlertConfig structure
type AlertConfig struct {
	Version string  `json:"version"`
	Alerts  []Alert `json:"alerts"`
}

// Alert structure
type Alert struct {
	Targets []Target `json:"targets"`
	Name    string   `json:"name"`
	Message string   `json:"message"`
	Rule    Rule     `json:"rule"`
}

// Target structure
type Target struct {
	Type         string            `json:"type"`
	Endpoint     string            `json:"endpoint"`
	Headers      map[string]string `json:"headers"`
	SkipTLSCheck bool              `json:"skip_tls_check"`
	Repeat       Repeat            `json:"repeat"`
}

// Repeat structure
type Repeat struct {
	Interval string `json:"interval"`
	Times    int    `json:"times"`
}

// Rule structure
type Rule struct {
	Type   string     `json:"type"`
	Config RuleConfig `json:"config"`
}

// RuleConfig structure
type RuleConfig struct {
	Column     string      `json:"column"`
	Operator   string      `json:"operator"`
	IgnoreCase bool        `json:"ignoreCase"`
	Value      interface{} `json:"value"`
	Repeats    int         `json:"repeats"`
}

// AddDatasetCmd is the parent command for dataset
var AddDatasetCmd = &cobra.Command{
	Use:     "add dataset-name",
	Example: "  pb dataset add backend_logs",
	Short:   "Create a new dataset",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Capture start time
		startTime := time.Now()
		cmd.Annotations = make(map[string]string)
		defer func() {
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		name := args[0]
		client := internalHTTP.DefaultClient(&DefaultProfile)
		req, err := client.NewRequest("PUT", "logstream/"+name, nil)
		if err != nil {
			// Capture error
			cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
			return err
		}

		resp, err := client.Client.Do(req)
		if err != nil {
			// Capture error
			cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
			return err
		}

		// Capture execution time
		cmd.Annotations["executionTime"] = time.Since(startTime).String()

		if resp.StatusCode == 200 {
			fmt.Printf("Created dataset %s\n", StyleBold.Render(name))
		} else {
			bytes, err := io.ReadAll(resp.Body)
			if err != nil {
				cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
				return err
			}
			body := string(bytes)
			defer resp.Body.Close()
			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		}

		return nil
	},
}

// StatDatasetCmd is the stat command for dataset
var StatDatasetCmd = &cobra.Command{
	Use:     "info dataset-name",
	Example: "  pb dataset info backend_logs",
	Short:   "Get statistics for a dataset",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Capture start time
		startTime := time.Now()
		cmd.Annotations = make(map[string]string)
		defer func() {
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		name := args[0]
		client := internalHTTP.DefaultClient(&DefaultProfile)

		// Fetch stats data
		stats, err := fetchStats(&client, name)
		if err != nil {
			// Capture error
			cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
			return err
		}

		ingestionCount := stats.Ingestion.Count
		ingestionSize := stats.Ingestion.Size
		storageSize := stats.Storage.Size
		var compressionRatio float64
		if ingestionSize > 0 {
			compressionRatio = 100 - (float64(storageSize) / float64(ingestionSize) * 100)
		}

		// Fetch retention data
		retention, err := fetchRetention(&client, name)
		if err != nil {
			// Capture error
			cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
			return err
		}

		// Fetch alerts data
		alertsData, err := fetchAlerts(&client, name)
		if err != nil {
			// Capture error
			cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
			return err
		}

		// Fetch dataset type
		datasetType, err := fetchInfo(&client, name)
		if err != nil {
			// Capture error
			cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
			return err
		}

		// Check output format
		output, _ := cmd.Flags().GetString("output")
		if output == "json" {
			// Prepare JSON response
			data := map[string]interface{}{
				"info": map[string]interface{}{
					"event_count":       ingestionCount,
					"ingestion_size":    humanize.Bytes(uint64(ingestionSize)),
					"storage_size":      humanize.Bytes(uint64(storageSize)),
					"compression_ratio": fmt.Sprintf("%.2f%%", compressionRatio),
				},
				"retention":    retention,
				"alerts":       alertsData.Alerts,
				"dataset_type": datasetType,
			}

			jsonData, err := json.MarshalIndent(data, "", "  ")
			if err != nil {
				// Capture error
				cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
				return err
			}
			fmt.Println(string(jsonData))
		} else {
			// Default text output
			isRetentionSet := len(retention) > 0
			isAlertsSet := len(alertsData.Alerts) > 0

			// Render the info section with consistent alignment
			fmt.Println(StyleBold.Render("\nInfo:"))
			fmt.Printf("  %-18s %d\n", "Event Count:", ingestionCount)
			fmt.Printf("  %-18s %s\n", "Ingestion Size:", humanize.Bytes(uint64(ingestionSize)))
			fmt.Printf("  %-18s %s\n", "Storage Size:", humanize.Bytes(uint64(storageSize)))
			fmt.Printf("  %-18s %.2f%s\n", "Compression Ratio:", compressionRatio, "%")
			fmt.Printf("  %-18s %s\n", "Dataset Type:", datasetType)
			fmt.Println()

			if isRetentionSet {
				fmt.Println(StyleBold.Render("Retention:"))
				for _, item := range retention {
					fmt.Printf("  Action:    %s\n", StyleBold.Render(item.Action))
					fmt.Printf("  Duration:  %s\n", StyleBold.Render(item.Duration))
					fmt.Println()
				}
			} else {
				fmt.Println(StyleBold.Render("No retention period set on dataset\n"))
			}

			if isAlertsSet {
				fmt.Println(StyleBold.Render("Alerts:"))
				for _, alert := range alertsData.Alerts {
					fmt.Printf("  Alert:   %s\n", StyleBold.Render(alert.Name))
					ruleFmt := fmt.Sprintf(
						"%s %s %s repeated %d times",
						alert.Rule.Config.Column,
						alert.Rule.Config.Operator,
						fmt.Sprint(alert.Rule.Config.Value),
						alert.Rule.Config.Repeats,
					)
					fmt.Printf("  Rule:    %s\n", ruleFmt)
					fmt.Printf("  Targets: ")
					for _, target := range alert.Targets {
						fmt.Printf("%s, ", target.Type)
					}
					fmt.Print("\n\n")
				}
			} else {
				fmt.Println(StyleBold.Render("No alerts set on dataset\n"))
			}
		}

		return nil
	},
}

func init() {
	StatDatasetCmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (text|json)")
}

var RemoveDatasetCmd = &cobra.Command{
	Use:     "remove dataset-name",
	Aliases: []string{"rm"},
	Example: " pb dataset remove backend_logs",
	Short:   "Delete a dataset",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Capture start time
		startTime := time.Now()
		cmd.Annotations = make(map[string]string)
		defer func() {
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		name := args[0]
		client := internalHTTP.DefaultClient(&DefaultProfile)
		req, err := client.NewRequest("DELETE", "logstream/"+name, nil)
		if err != nil {
			// Capture error
			cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
			return err
		}

		resp, err := client.Client.Do(req)
		if err != nil {
			// Capture error
			cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
			return err
		}

		// Capture execution time
		cmd.Annotations["executionTime"] = time.Since(startTime).String()

		if resp.StatusCode == 200 {
			fmt.Printf("Successfully deleted dataset %s\n", StyleBold.Render(name))
		} else {
			bytes, err := io.ReadAll(resp.Body)
			if err != nil {
				cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
				return err
			}
			body := string(bytes)
			defer resp.Body.Close()
			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		}

		return nil
	},
}

// ListDatasetCmd is the list command for datasets
var ListDatasetCmd = &cobra.Command{
	Use:     "list",
	Example: "  pb dataset list",
	Short:   "List all datasets",
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Capture start time
		startTime := time.Now()
		cmd.Annotations = make(map[string]string)
		defer func() {
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		client := internalHTTP.DefaultClient(&DefaultProfile)
		req, err := client.NewRequest("GET", "logstream", nil)
		if err != nil {
			// Capture error
			cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
			return err
		}

		resp, err := client.Client.Do(req)
		if err != nil {
			// Capture error
			cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
			return err
		}

		var datasets []DatasetListItem
		if resp.StatusCode == 200 {
			bytes, err := io.ReadAll(resp.Body)
			if err != nil {
				cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
				return err
			}
			if err := json.Unmarshal(bytes, &datasets); err != nil {
				cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
				return err
			}

			for _, dataset := range datasets {
				fmt.Println(dataset.Render())
			}
		} else {
			fmt.Printf("Failed to fetch datasets. Status Code: %s\n", resp.Status)
		}

		return nil
	},
}

func init() {
	// Add the --output flag with default value "text"
	ListDatasetCmd.Flags().StringP("output", "o", "text", "Output format: 'text' or 'json'")
}

func fetchStats(client *internalHTTP.HTTPClient, name string) (data DatasetStatsData, err error) {
	req, err := client.NewRequest("GET", fmt.Sprintf("logstream/%s/stats", name), nil)
	if err != nil {
		return
	}

	resp, err := client.Client.Do(req)
	if err != nil {
		return
	}

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		err = json.Unmarshal(bytes, &data)
	case http.StatusNotFound:
		// stream exists but has no stats yet (empty stream)
	default:
		err = fmt.Errorf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, string(bytes))
	}
	return
}

func fetchRetention(client *internalHTTP.HTTPClient, name string) (data DatasetRetentionData, err error) {
	req, err := client.NewRequest(http.MethodGet, fmt.Sprintf("logstream/%s/retention", name), nil)
	if err != nil {
		return
	}

	resp, err := client.Client.Do(req)
	if err != nil {
		return
	}

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		err = json.Unmarshal(bytes, &data)
	case http.StatusNotFound:
		// no retention configured
	default:
		err = fmt.Errorf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, string(bytes))
	}
	return
}

func fetchAlerts(client *internalHTTP.HTTPClient, name string) (data AlertConfig, err error) {
	req, err := client.NewRequest(http.MethodGet, fmt.Sprintf("logstream/%s/alert", name), nil)
	if err != nil {
		return
	}

	resp, err := client.Client.Do(req)
	if err != nil {
		return
	}

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		err = json.Unmarshal(bytes, &data)
	case http.StatusNotFound:
		// no alerts configured
	default:
		err = fmt.Errorf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, string(bytes))
	}
	return
}

func fetchInfo(client *internalHTTP.HTTPClient, name string) (datasetType string, err error) {
	// Create a new HTTP GET request
	req, err := client.NewRequest(http.MethodGet, fmt.Sprintf("logstream/%s/info", name), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Execute the request
	resp, err := client.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request execution failed: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body
	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Check for successful status code
	switch resp.StatusCode {
	case http.StatusOK:
		// Define a struct to parse the response
		var response struct {
			StreamType string `json:"stream_type"`
		}

		// Unmarshal JSON into the struct
		if err := json.Unmarshal(bytes, &response); err != nil {
			return "", fmt.Errorf("failed to unmarshal response: %w", err)
		}

		// Return the extracted stream_type
		return response.StreamType, nil
	case http.StatusNotFound:
		// endpoint not available on this server version or stream has no type info
		return "unknown", nil
	default:
		// Handle non-200 responses
		return "", fmt.Errorf("Request Failed\nStatus Code: %d\nResponse: %s\n", resp.StatusCode, string(bytes))
	}
}
