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
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
	"github.com/parseablehq/pb/pkg/datasets"
	internalHTTP "github.com/parseablehq/pb/pkg/http"
	"github.com/parseablehq/pb/pkg/ui"
	"github.com/spf13/cobra"
)

const datasetTypeFlag = "type"

var errDatasetTypeSelectionCanceled = errors.New("dataset type selection canceled")

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
	Name         string
	Type         string
	LastIngested time.Time
}

func (item *DatasetListItem) Render() string {
	bullet := SelectedStyle.Render("•")
	name := StandardStyle.Render(item.Name)
	datasetType := StandardStyle.Render(item.Type)
	if datasetType == "" {
		return ItemOuter.Render(fmt.Sprintf("%s %s", bullet, name))
	}
	return ItemOuter.Render(fmt.Sprintf("%s %s [%s]", bullet, name, datasetType))
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
	Use:          "add dataset-name",
	Example:      "  pb dataset add backend_logs\n  pb dataset add frontend_metrics --type metrics\n  pb dataset add checkout_traces --type traces",
	Short:        "Create a new dataset",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Capture start time
		startTime := time.Now()
		cmd.Annotations = make(map[string]string)
		defer func() {
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		name := args[0]
		datasetType, err := cmd.Flags().GetString(datasetTypeFlag)
		if err != nil {
			cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
			return err
		}
		datasetType, err = resolveDatasetType(datasetType)
		if err != nil {
			if errors.Is(err, errDatasetTypeSelectionCanceled) {
				fmt.Println("Dataset creation canceled")
				return nil
			}
			cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
			return err
		}

		client := internalHTTP.DefaultClient(&DefaultProfile)
		req, err := client.NewRequest("PUT", "logstream/"+name, nil)
		if err != nil {
			// Capture error
			cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
			return err
		}
		req.Header.Set("X-P-Telemetry-Type", datasetType)
		if datasetType == datasets.TypeMetrics || datasetType == datasets.TypeTraces {
			req.Header.Set("X-P-Log-Source", "otel-"+datasetType)
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
			fmt.Printf("Created %s dataset %s\n", SelectedStyle.Render(datasetType), StyleBold.Render(name))
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

func init() {
	AddDatasetCmd.Flags().String(datasetTypeFlag, "", "Dataset type (logs|metrics|traces)")
}

func resolveDatasetType(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return promptDatasetType()
	}
	return validateDatasetType(value)
}

func validateDatasetType(value string) (string, error) {
	switch value {
	case datasets.TypeLogs, datasets.TypeMetrics, datasets.TypeTraces:
		return value, nil
	default:
		return "", fmt.Errorf("invalid dataset type %q (use: logs, metrics, traces)", value)
	}
}

func promptDatasetType() (string, error) {
	_m, err := tea.NewProgram(newDatasetTypePicker()).Run()
	if err != nil {
		return "", err
	}

	m := _m.(datasetTypePicker)
	if !m.success {
		return "", errDatasetTypeSelectionCanceled
	}
	return m.choice(), nil
}

type datasetTypeOption struct {
	value string
}

type datasetTypePicker struct {
	options []datasetTypeOption
	cursor  int
	success bool
}

func newDatasetTypePicker() datasetTypePicker {
	return datasetTypePicker{
		options: []datasetTypeOption{
			{value: datasets.TypeLogs},
			{value: datasets.TypeMetrics},
			{value: datasets.TypeTraces},
		},
	}
}

func (m datasetTypePicker) Init() tea.Cmd {
	return nil
}

func (m datasetTypePicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case "enter":
			m.success = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m datasetTypePicker) View() string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Accent }))
	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Mute }))
	selectedStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Active }))
	normalStyle := lipgloss.NewStyle().
		Foreground(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Body }))
	dimStyle := lipgloss.NewStyle().
		Foreground(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Faint }))
	keyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Accent }))
	railStyle := lipgloss.NewStyle().
		Background(ui.Adaptive(func(p ui.Palette) lipgloss.Color { return p.Active }))

	b.WriteString(titleStyle.Render("CREATE DATASET"))
	b.WriteString("\n\n")
	b.WriteString(labelStyle.Render("DATASET TYPE"))
	b.WriteString("\n\n")

	for idx, option := range m.options {
		if idx == m.cursor {
			b.WriteString(railStyle.Render(" "))
			b.WriteString(" ")
			b.WriteString(selectedStyle.Render("❯ " + option.value))
		} else {
			b.WriteString("    ")
			b.WriteString(normalStyle.Render(option.value))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(keyStyle.Render("<↑↓>"))
	b.WriteString(dimStyle.Render(" navigate    "))
	b.WriteString(keyStyle.Render("<enter>"))
	b.WriteString(dimStyle.Render(" select    "))
	b.WriteString(keyStyle.Render("<esc>"))
	b.WriteString(dimStyle.Render(" cancel"))
	return b.String()
}

func (m datasetTypePicker) choice() string {
	if len(m.options) == 0 {
		return ""
	}
	return m.options[m.cursor].value
}

// StatDatasetCmd is the stat command for dataset
var StatDatasetCmd = &cobra.Command{
	Use:          "info dataset-name",
	Aliases:      []string{"stat"},
	Example:      "  pb dataset info backend_logs",
	Short:        "Get statistics for a dataset",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Capture start time
		startTime := time.Now()
		cmd.Annotations = make(map[string]string)
		defer func() {
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		name := args[0]
		client := internalHTTP.DefaultClient(&DefaultProfile)

		// Fetch dataset type first so a missing dataset fails clearly instead
		// of being rendered as zero stats with an unknown type.
		datasetType, err := fetchInfo(&client, name)
		if err != nil {
			cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
			return err
		}

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
			fmt.Println(SelectedStyle.Render("\nInfo:"))
			fmt.Printf("  %-18s %d\n", "Event Count:", ingestionCount)
			fmt.Printf("  %-18s %s\n", "Ingestion Size:", humanize.Bytes(uint64(ingestionSize)))
			fmt.Printf("  %-18s %s\n", "Storage Size:", humanize.Bytes(uint64(storageSize)))
			fmt.Printf("  %-18s %.2f%s\n", "Compression Ratio:", compressionRatio, "%")
			fmt.Printf("  %-18s %s\n", "Dataset Type:", SelectedStyle.Render(datasetType))
			fmt.Println()

			if isRetentionSet {
				fmt.Println(SelectedStyle.Render("Retention:"))
				for _, item := range retention {
					fmt.Printf("  Action:    %s\n", StyleBold.Render(item.Action))
					fmt.Printf("  Duration:  %s\n", StyleBold.Render(item.Duration))
					fmt.Println()
				}
			} else {
				fmt.Println(SelectedStyle.Render("No retention period set on dataset\n"))
			}

			if isAlertsSet {
				fmt.Println(SelectedStyle.Render("Alerts:"))
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
				fmt.Println(SelectedStyle.Render("No alerts set on dataset\n"))
			}
		}

		return nil
	},
}

func init() {
	StatDatasetCmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (text|json)")
}

var RemoveDatasetCmd = &cobra.Command{
	Use:          "remove dataset-name",
	Aliases:      []string{"rm"},
	Example:      " pb dataset remove backend_logs\n pb dataset remove backend_logs --type logs",
	Short:        "Delete a dataset",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Capture start time
		startTime := time.Now()
		cmd.Annotations = make(map[string]string)
		defer func() {
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		name := args[0]
		client := internalHTTP.DefaultClient(&DefaultProfile)
		expectedType, err := cmd.Flags().GetString(datasetTypeFlag)
		if err != nil {
			cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
			return err
		}
		expectedType = strings.ToLower(strings.TrimSpace(expectedType))
		if expectedType != "" {
			expectedType, err = validateDatasetType(expectedType)
			if err != nil {
				cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
				return err
			}

			actualType, err := fetchInfo(&client, name)
			if err != nil {
				cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
				return err
			}
			if err := ensureDatasetType(name, actualType, expectedType); err != nil {
				cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
				return err
			}
		}

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

func ensureDatasetType(name, actualType, expectedType string) error {
	if actualType != expectedType {
		return fmt.Errorf("dataset %q is %s, not %s", name, actualType, expectedType)
	}
	return nil
}

func init() {
	RemoveDatasetCmd.Flags().String(datasetTypeFlag, "", "Only remove if dataset type matches (logs|metrics|traces)")
}

// ListDatasetCmd is the list command for datasets
var ListDatasetCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Example:      "  pb dataset list",
	Short:        "List all datasets",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Capture start time
		startTime := time.Now()
		cmd.Annotations = make(map[string]string)
		defer func() {
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		items, err := datasets.FetchHomeDatasets(DefaultProfile)
		if err != nil {
			cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
			return err
		}

		output, _ := cmd.Flags().GetString("output")
		if output == "json" {
			jsonData, err := json.MarshalIndent(items, "", "  ")
			if err != nil {
				cmd.Annotations["errors"] = fmt.Sprintf("Error: %s", err.Error())
				return err
			}
			fmt.Println(string(jsonData))
			return nil
		}

		for _, dataset := range items {
			fmt.Println((&DatasetListItem{Name: dataset.Title, Type: dataset.DatasetType}).Render())
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
			StreamType    string `json:"streamType"`
			TelemetryType string `json:"telemetryType"`
		}

		// Unmarshal JSON into the struct
		if err := json.Unmarshal(bytes, &response); err != nil {
			return "", fmt.Errorf("failed to unmarshal response: %w", err)
		}

		if response.TelemetryType != "" {
			return response.TelemetryType, nil
		}
		return response.StreamType, nil
	case http.StatusNotFound:
		return "", fmt.Errorf("dataset %q not found", name)
	default:
		// Handle non-200 responses
		return "", fmt.Errorf("Request Failed\nStatus Code: %d\nResponse: %s\n", resp.StatusCode, string(bytes))
	}
}
