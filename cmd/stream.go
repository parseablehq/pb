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
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

// StreamStatsData is the data structure for stream stats
type StreamStatsData struct {
	Ingestion struct {
		Count  int    `json:"count"`
		Format string `json:"format"`
		Size   string `json:"size"`
	} `json:"ingestion"`
	Storage struct {
		Format string `json:"format"`
		Size   string `json:"size"`
	} `json:"storage"`
	Stream string    `json:"stream"`
	Time   time.Time `json:"time"`
}

type StreamListItem struct {
	Name string
}

func (item *StreamListItem) Render() string {
	render := StandardStyle.Render(item.Name)
	return ItemOuter.Render(render)
}

// StreamRetentionData is the data structure for stream retention
type StreamRetentionData []struct {
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

// AddStreamCmd is the parent command for stream
var AddStreamCmd = &cobra.Command{
	Use:     "add stream-name",
	Example: "  pb stream add backend_logs",
	Short:   "Create a new stream",
	Args:    cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		name := args[0]
		client := DefaultClient()
		req, err := client.NewRequest("PUT", "logstream/"+name, nil)
		if err != nil {
			return err
		}

		resp, err := client.client.Do(req)
		if err != nil {
			return err
		}

		if resp.StatusCode == 200 {
			fmt.Printf("Created stream %s\n", StyleBold.Render(name))
		} else {
			bytes, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			body := string(bytes)
			defer resp.Body.Close()
			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		}

		return nil
	},
}

// StatStreamCmd is the stat command for stream
var StatStreamCmd = &cobra.Command{
	Use:     "info stream-name",
	Example: "  pb stream info backend_logs",
	Short:   "Get statistics for a stream",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		client := DefaultClient()

		// Fetch stats data
		stats, err := fetchStats(&client, name)
		if err != nil {
			return err
		}

		ingestionCount := stats.Ingestion.Count
		ingestionSize, _ := strconv.Atoi(strings.TrimRight(stats.Ingestion.Size, " Bytes"))
		storageSize, _ := strconv.Atoi(strings.TrimRight(stats.Storage.Size, " Bytes"))
		compressionRatio := 100 - (float64(storageSize) / float64(ingestionSize) * 100)

		// Fetch retention data
		retention, err := fetchRetention(&client, name)
		if err != nil {
			return err
		}

		// Fetch alerts data
		alertsData, err := fetchAlerts(&client, name)
		if err != nil {
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
				"retention": retention,
				"alerts":    alertsData.Alerts,
			}

			jsonData, err := json.MarshalIndent(data, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(jsonData))
		} else {
			// Default text output
			isRetentionSet := len(retention) > 0
			isAlertsSet := len(alertsData.Alerts) > 0

			fmt.Println(StyleBold.Render("\nInfo:"))
			fmt.Printf("  Event Count:     %d\n", ingestionCount)
			fmt.Printf("  Ingestion Size:  %s\n", humanize.Bytes(uint64(ingestionSize)))
			fmt.Printf("  Storage Size:    %s\n", humanize.Bytes(uint64(storageSize)))
			fmt.Printf(
				"  Compression Ratio:    %.2f%s\n",
				compressionRatio, "%")
			fmt.Println()

			if isRetentionSet {
				fmt.Println(StyleBold.Render("Retention:"))
				for _, item := range retention {
					fmt.Printf("  Action:    %s\n", StyleBold.Render(item.Action))
					fmt.Printf("  Duration:  %s\n", StyleBold.Render(item.Duration))
					fmt.Println()
				}
			} else {
				fmt.Println(StyleBold.Render("No retention period set on stream\n"))
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
				fmt.Println(StyleBold.Render("No alerts set on stream\n"))
			}
		}

		return nil
	},
}

func init() {
	StatStreamCmd.Flags().String("output", "text", "Output format: text or json")
}

var RemoveStreamCmd = &cobra.Command{
	Use:     "remove stream-name",
	Aliases: []string{"rm"},
	Example: " pb stream remove backend_logs",
	Short:   "Delete a stream",
	Args:    cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		name := args[0]
		client := DefaultClient()
		req, err := client.NewRequest("DELETE", "logstream/"+name, nil)
		if err != nil {
			return err
		}

		resp, err := client.client.Do(req)
		if err != nil {
			return err
		}

		if resp.StatusCode == 200 {
			fmt.Printf("Removed stream %s\n", StyleBold.Render(name))
		} else {
			bytes, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			body := string(bytes)
			defer resp.Body.Close()

			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		}

		return nil
	},
}

var ListStreamCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all streams",
	Example: "  pb stream list",
	RunE: func(cmd *cobra.Command, _ []string) error {
		client := DefaultClient()
		req, err := client.NewRequest("GET", "logstream", nil)
		if err != nil {
			return err
		}

		resp, err := client.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			// Read response body for error message
			bytes, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			body := string(bytes)
			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
			return nil
		}

		var items []map[string]string
		err = json.NewDecoder(resp.Body).Decode(&items)
		if err != nil {
			return err
		}

		// Get output flag value
		outputFormat, err := cmd.Flags().GetString("output")
		if err != nil {
			return err
		}

		// Handle JSON output format
		if outputFormat == "json" {
			// Collect stream names for JSON output
			streams := make([]string, len(items))
			for i, item := range items {
				streams[i] = item["name"]
			}
			jsonOutput, err := json.MarshalIndent(streams, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON output: %w", err)
			}
			fmt.Println(string(jsonOutput))
			return nil
		}

		// Default to text output
		if len(items) == 0 {
			fmt.Println("No streams found")
			return nil
		}

		fmt.Println()
		for _, item := range items {
			streamItem := StreamListItem{Name: item["name"]}
			fmt.Print("• ")
			fmt.Println(streamItem.Render())
		}
		fmt.Println()
		return nil
	},
}

func init() {
	// Add the --output flag with default value "text"
	ListStreamCmd.Flags().StringP("output", "o", "text", "Output format: 'text' or 'json'")
}

func fetchStats(client *HTTPClient, name string) (data StreamStatsData, err error) {
	req, err := client.NewRequest("GET", fmt.Sprintf("logstream/%s/stats", name), nil)
	if err != nil {
		return
	}

	resp, err := client.client.Do(req)
	if err != nil {
		return
	}

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		err = json.Unmarshal(bytes, &data)
	} else {
		body := string(bytes)
		body = fmt.Sprintf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		err = errors.New(body)
	}
	return
}

func fetchRetention(client *HTTPClient, name string) (data StreamRetentionData, err error) {
	req, err := client.NewRequest("GET", fmt.Sprintf("logstream/%s/retention", name), nil)
	if err != nil {
		return
	}

	resp, err := client.client.Do(req)
	if err != nil {
		return
	}

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		err = json.Unmarshal(bytes, &data)
	} else {
		body := string(bytes)
		body = fmt.Sprintf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		err = errors.New(body)
	}
	return
}

func fetchAlerts(client *HTTPClient, name string) (data AlertConfig, err error) {
	req, err := client.NewRequest("GET", fmt.Sprintf("logstream/%s/alert", name), nil)
	if err != nil {
		return
	}

	resp, err := client.client.Do(req)
	if err != nil {
		return
	}

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		err = json.Unmarshal(bytes, &data)
	} else {
		body := string(bytes)
		body = fmt.Sprintf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		err = errors.New(body)
	}
	return
}
