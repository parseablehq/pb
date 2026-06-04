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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	internalHTTP "pb/pkg/http"
	"pb/pkg/model"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

const defaultMetricsStream = "select-dataset"

// PromqlCmd is the parent command for all PromQL operations.
var PromqlCmd = &cobra.Command{
	Use:     "promql",
	Short:   "PromQL queries and metrics exploration",
	Long:    "\nRun PromQL queries and explore metrics stored in a Parseable metrics stream.",
	Example: "  pb promql run -i\n  pb promql run \"http_requests_total\" --dataset otel_metrics --from=1h -i",
}

func init() {
	PromqlCmd.SetHelpFunc(renderPromqlHelp)

	// query execution
	PromqlCmd.AddCommand(promqlRunCmd)

	// metadata / exploration
	PromqlCmd.AddCommand(promqlLabelsCmd)
	PromqlCmd.AddCommand(promqlLabelValuesCmd)
	PromqlCmd.AddCommand(promqlSeriesCmd)

	// cardinality group
	PromqlCmd.AddCommand(promqlCardinalityCmd)
	promqlCardinalityCmd.AddCommand(promqlCardinalityLabelNamesCmd)
	promqlCardinalityCmd.AddCommand(promqlCardinalityLabelValuesCmd)
	promqlCardinalityCmd.AddCommand(promqlCardinalityActiveSeriesCmd)

	// ops / debug
	PromqlCmd.AddCommand(promqlActiveQueriesCmd)
	PromqlCmd.AddCommand(promqlTSDBCmd)

	// flags: run
	promqlRunCmd.Flags().StringP("dataset", "d", defaultMetricsStream, "Metrics dataset to query")
	promqlRunCmd.Flags().StringP("from", "f", "5m", "Start time (e.g. 5m, 1h, 2024-01-01T00:00:00Z)")
	promqlRunCmd.Flags().StringP("to", "t", "now", "End time")
	promqlRunCmd.Flags().String("step", "auto", "Resolution step for range queries (auto, 15s, 1m)")
	promqlRunCmd.Flags().StringP("output", "o", "text", "Output format: text or json")
	promqlRunCmd.Flags().Bool("instant", false, "Instant query — evaluate at --to time only")
	promqlRunCmd.Flags().BoolP("interactive", "i", false, "Open interactive TUI")

	// flags: labels
	promqlLabelsCmd.Flags().StringP("dataset", "d", defaultMetricsStream, "Metrics dataset")
	promqlLabelsCmd.Flags().StringP("from", "f", "", "Start time filter (optional)")
	promqlLabelsCmd.Flags().StringP("to", "t", "", "End time filter (optional)")
	promqlLabelsCmd.Flags().StringP("output", "o", "text", "Output format: text or json")

	// flags: label-values
	promqlLabelValuesCmd.Flags().StringP("dataset", "d", defaultMetricsStream, "Metrics dataset")
	promqlLabelValuesCmd.Flags().StringP("from", "f", "", "Start time filter (optional)")
	promqlLabelValuesCmd.Flags().StringP("to", "t", "", "End time filter (optional)")
	promqlLabelValuesCmd.Flags().StringP("output", "o", "text", "Output format: text or json")

	// flags: series
	promqlSeriesCmd.Flags().StringP("dataset", "d", defaultMetricsStream, "Metrics dataset")
	promqlSeriesCmd.Flags().StringArrayP("match", "m", nil, "Series selector (repeatable, e.g. '{job=\"api\"}')")
	promqlSeriesCmd.Flags().StringP("from", "f", "", "Start time filter (optional)")
	promqlSeriesCmd.Flags().StringP("to", "t", "", "End time filter (optional)")
	promqlSeriesCmd.Flags().StringP("output", "o", "text", "Output format: text or json")

	// flags: cardinality label-names
	promqlCardinalityLabelNamesCmd.Flags().StringP("dataset", "d", defaultMetricsStream, "Metrics dataset")
	promqlCardinalityLabelNamesCmd.Flags().Int("lookback", 3600, "Seconds to look back from now")
	promqlCardinalityLabelNamesCmd.Flags().Int("limit", 20, "Maximum number of labels to return")
	promqlCardinalityLabelNamesCmd.Flags().String("selector", "", "Label selector to filter series")
	promqlCardinalityLabelNamesCmd.Flags().StringP("output", "o", "text", "Output format: text or json")

	// flags: cardinality label-values
	promqlCardinalityLabelValuesCmd.Flags().StringP("dataset", "d", defaultMetricsStream, "Metrics dataset")
	promqlCardinalityLabelValuesCmd.Flags().StringP("label", "l", "", "Label name to analyze")
	promqlCardinalityLabelValuesCmd.Flags().Int("lookback", 3600, "Seconds to look back from now")
	promqlCardinalityLabelValuesCmd.Flags().Int("limit", 20, "Maximum number of values to return")
	promqlCardinalityLabelValuesCmd.Flags().StringP("output", "o", "text", "Output format: text or json")

	// flags: cardinality active-series
	promqlCardinalityActiveSeriesCmd.Flags().StringP("dataset", "d", defaultMetricsStream, "Metrics dataset")
	promqlCardinalityActiveSeriesCmd.Flags().Int("lookback", 3600, "Seconds to look back from now")
	promqlCardinalityActiveSeriesCmd.Flags().Int("limit", 20, "Maximum number of series to return")
	promqlCardinalityActiveSeriesCmd.Flags().String("selector", "", "Label selector to filter series")
	promqlCardinalityActiveSeriesCmd.Flags().StringP("output", "o", "text", "Output format: text or json")

	// flags: tsdb
	promqlTSDBCmd.Flags().StringP("dataset", "d", defaultMetricsStream, "Metrics dataset")
	promqlTSDBCmd.Flags().Int("top", 10, "Max entries per category")
	promqlTSDBCmd.Flags().String("date", "", "Date to analyze (YYYY-MM-DD, defaults to today)")
	promqlTSDBCmd.Flags().String("focus-label", "", "Label to break down series counts by")
	promqlTSDBCmd.Flags().StringP("output", "o", "text", "Output format: text or json")
}

func renderPromqlHelp(cmd *cobra.Command, _ []string) {
	out := cmd.OutOrStdout()

	if cmd.Long != "" {
		fmt.Fprintln(out, cmd.Long)
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, "Usage:")
	fmt.Fprintf(out, "  %s\n", cmd.UseLine())
	fmt.Fprintln(out)

	if cmd.Example != "" {
		fmt.Fprintln(out, "Examples:")
		fmt.Fprintln(out, cmd.Example)
		fmt.Fprintln(out)
	}

	order := []string{"run", "labels", "label-values", "series", "cardinality", "tsdb", "active-queries"}
	commandsByName := make(map[string]*cobra.Command, len(cmd.Commands()))
	for _, child := range cmd.Commands() {
		commandsByName[child.Name()] = child
	}

	fmt.Fprintln(out, "Available Commands:")
	width := 0
	for _, name := range order {
		if len(name) > width {
			width = len(name)
		}
	}
	for _, name := range order {
		child, ok := commandsByName[name]
		if !ok || (!child.IsAvailableCommand() && child.Name() != "help") {
			continue
		}
		fmt.Fprintf(out, "  %-*s %s\n", width, child.Name(), child.Short)
	}
	fmt.Fprintln(out)

	if cmd.HasAvailableLocalFlags() {
		fmt.Fprintln(out, "Flags:")
		var flags bytes.Buffer
		cmd.LocalFlags().SetOutput(&flags)
		cmd.LocalFlags().PrintDefaults()
		fmt.Fprint(out, flags.String())
		fmt.Fprintln(out)
	}

	fmt.Fprintf(out, "Use \"%s [command] --help\" for more information about a command.\n", cmd.CommandPath())
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

func promqlGet(path string, params url.Values) ([]byte, error) {
	client := internalHTTP.DefaultClient(&DefaultProfile)
	client.Client.Timeout = 120 * time.Second
	client.Client.Transport = &http.Transport{
		TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),
	}
	reqURL, err := url.JoinPath(DefaultProfile.URL, path)
	if err != nil {
		return nil, err
	}
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	if DefaultProfile.Token != "" {
		req.Header.Set("Authorization", "Bearer "+DefaultProfile.Token)
	} else {
		req.SetBasicAuth(DefaultProfile.Username, DefaultProfile.Password)
	}
	stopSpinner := startSpinner()
	resp, err := client.Client.Do(req)
	stopSpinner()
	if err != nil {
		if strings.Contains(err.Error(), "connection reset") {
			return nil, fmt.Errorf("server reset the connection — query timed out")
		}
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func printRawJSON(body []byte) {
	var v interface{}
	if json.Unmarshal(body, &v) == nil {
		b, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Println(string(body))
	}
}

func optionalTimeParam(params url.Values, cmd *cobra.Command, flagName, paramName string) {
	val, _ := cmd.Flags().GetString(flagName)
	if val == "" {
		return
	}
	t, err := parseTimeStr(val)
	if err == nil {
		params.Set(paramName, t.UTC().Format(time.RFC3339))
	}
}

// ---------------------------------------------------------------------------
// 1. run — range or instant PromQL query
// ---------------------------------------------------------------------------

var promqlRunCmd = &cobra.Command{
	Use:   "run [expr]",
	Short: "Run a PromQL query (range or instant)",
	Long:  "\nEvaluate a PromQL expression against a Parseable metrics stream.\nDefaults to range query. Use --instant for point-in-time evaluation.",
	Example: "  pb promql run \"http_requests_total\" --dataset otel_metrics --from 1h\n" +
		"  pb promql run \"rate(http_requests_total[5m])\" --dataset otel_metrics --from 1h --step 1m\n" +
		"  pb promql run \"up\" --dataset otel_metrics --instant -o json",
	Args:    cobra.MaximumNArgs(1),
	PreRunE: PreRunDefaultProfile,
	RunE:    runPromqlQuery,
}

type promqlResponse struct {
	Status    string     `json:"status"`
	Data      promqlData `json:"data"`
	Error     string     `json:"error,omitempty"`
	ErrorType string     `json:"errorType,omitempty"`
}

type promqlData struct {
	ResultType string         `json:"resultType"`
	Result     []promqlSeries `json:"result"`
}

type promqlSeries struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value,omitempty"`  // instant: [ts, "val"]
	Values [][]any           `json:"values,omitempty"` // range:   [[ts, "val"], ...]
}

func runPromqlQuery(cmd *cobra.Command, args []string) error {
	var expr string
	if len(args) > 0 {
		expr = args[0]
	}
	stream, _ := cmd.Flags().GetString("dataset")
	fromStr, _ := cmd.Flags().GetString("from")
	toStr, _ := cmd.Flags().GetString("to")
	step, _ := cmd.Flags().GetString("step")
	outputFmt, _ := cmd.Flags().GetString("output")
	instant, _ := cmd.Flags().GetBool("instant")
	interactive, _ := cmd.Flags().GetBool("interactive")
	fromStr = promqlInteractiveFromDefault(fromStr, interactive, cmd.Flags().Changed("from"))

	toTime, err := parseTimeStr(toStr)
	if err != nil {
		return fmt.Errorf("invalid --to: %w", err)
	}

	if interactive {
		startTime, err := parseTimeStr(fromStr)
		if err != nil {
			return fmt.Errorf("invalid --from: %w", err)
		}
		m := model.NewPromqlModel(DefaultProfile, expr, startTime, toTime, step, stream, instant)
		p := tea.NewProgram(m, tea.WithAltScreen())
		_, err = p.Run()
		return err
	}

	if strings.TrimSpace(expr) == "" {
		fmt.Println("Please enter a PromQL expression")
		fmt.Printf("Example:\n  pb promql run \"http_requests_total\" --dataset otel_metrics\n  pb promql run -i\n")
		return nil
	}

	params := url.Values{}
	params.Set("query", expr)
	params.Set("stream", stream)

	var apiPath string
	if instant {
		apiPath = "prometheus/api/v1/query"
		params.Set("time", toTime.UTC().Format(time.RFC3339))
	} else {
		startTime, err := parseTimeStr(fromStr)
		if err != nil {
			return fmt.Errorf("invalid --from: %w", err)
		}
		apiPath = "prometheus/api/v1/query_range"
		params.Set("start", startTime.UTC().Format(time.RFC3339))
		params.Set("end", toTime.UTC().Format(time.RFC3339))
		params.Set("step", model.ResolvePromqlStep(step, startTime, toTime))
	}

	body, err := promqlGet(apiPath, params)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	if outputFmt == "json" {
		printRawJSON(body)
		return nil
	}

	var result promqlResponse
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Println(string(body))
		return nil
	}
	if result.Status == "error" {
		return fmt.Errorf("query error (%s): %s", result.ErrorType, result.Error)
	}
	if len(result.Data.Result) == 0 {
		fmt.Println("No data returned.")
		return nil
	}

	for _, series := range result.Data.Result {
		fmt.Printf("%s\n", formatPromqlLabels(series.Metric))
		switch result.Data.ResultType {
		case "vector":
			if len(series.Value) == 2 {
				fmt.Printf("  %s  %v\n", promqlTS(series.Value[0]), series.Value[1])
			}
		case "matrix":
			for _, pt := range series.Values {
				if len(pt) == 2 {
					fmt.Printf("  %s  %v\n", promqlTS(pt[0]), pt[1])
				}
			}
		}
		fmt.Println()
	}
	fmt.Printf("result_type=%s  series=%d\n", result.Data.ResultType, len(result.Data.Result))
	return nil
}

func promqlInteractiveFromDefault(from string, interactive, fromChanged bool) string {
	if interactive && !fromChanged {
		return "10m"
	}
	return from
}

// ---------------------------------------------------------------------------
// 2. labels — list all label names
// ---------------------------------------------------------------------------

var promqlLabelsCmd = &cobra.Command{
	Use:     "labels",
	Short:   "List all label names in a metrics stream",
	Example: "  pb promql labels --dataset otel_metrics",
	Args:    cobra.NoArgs,
	PreRunE: PreRunDefaultProfile,
	RunE: func(cmd *cobra.Command, _ []string) error {
		stream, _ := cmd.Flags().GetString("dataset")
		outputFmt, _ := cmd.Flags().GetString("output")

		params := url.Values{}
		params.Set("stream", stream)
		optionalTimeParam(params, cmd, "from", "start")
		optionalTimeParam(params, cmd, "to", "end")

		body, err := promqlGet("prometheus/api/v1/labels", params)
		if err != nil {
			return err
		}
		if outputFmt == "json" {
			printRawJSON(body)
			return nil
		}

		var resp struct {
			Status string   `json:"status"`
			Data   []string `json:"data"`
			Error  string   `json:"error,omitempty"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Println(string(body))
			return nil
		}
		if resp.Status == "error" {
			return fmt.Errorf("%s", resp.Error)
		}
		for _, l := range resp.Data {
			fmt.Println(l)
		}
		fmt.Printf("\ntotal=%d\n", len(resp.Data))
		return nil
	},
}

// ---------------------------------------------------------------------------
// 3. label-values — distinct values for a label
// ---------------------------------------------------------------------------

var promqlLabelValuesCmd = &cobra.Command{
	Use:     "label-values [label_name]",
	Short:   "List distinct values for a label",
	Example: "  pb promql label-values job --dataset otel_metrics\n  pb promql label-values __name__ --dataset otel_metrics",
	Args:    cobra.ExactArgs(1),
	PreRunE: PreRunDefaultProfile,
	RunE: func(cmd *cobra.Command, args []string) error {
		label := args[0]
		stream, _ := cmd.Flags().GetString("dataset")
		outputFmt, _ := cmd.Flags().GetString("output")

		params := url.Values{}
		params.Set("stream", stream)
		optionalTimeParam(params, cmd, "from", "start")
		optionalTimeParam(params, cmd, "to", "end")

		body, err := promqlGet("prometheus/api/v1/label/"+label+"/values", params)
		if err != nil {
			return err
		}
		if outputFmt == "json" {
			printRawJSON(body)
			return nil
		}

		var resp struct {
			Status string   `json:"status"`
			Data   []string `json:"data"`
			Error  string   `json:"error,omitempty"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Println(string(body))
			return nil
		}
		if resp.Status == "error" {
			return fmt.Errorf("%s", resp.Error)
		}
		for _, v := range resp.Data {
			fmt.Println(v)
		}
		fmt.Printf("\nlabel=%s  total=%d\n", label, len(resp.Data))
		return nil
	},
}

// ---------------------------------------------------------------------------
// 4. series — find time series matching a selector
// ---------------------------------------------------------------------------

var promqlSeriesCmd = &cobra.Command{
	Use:     "series",
	Short:   "Find time series matching a label selector",
	Example: "  pb promql series --match 'http_requests_total' --dataset otel_metrics\n  pb promql series --match '{job=\"api\"}' --dataset otel_metrics",
	Args:    cobra.NoArgs,
	PreRunE: PreRunDefaultProfile,
	RunE: func(cmd *cobra.Command, _ []string) error {
		stream, _ := cmd.Flags().GetString("dataset")
		matchers, _ := cmd.Flags().GetStringArray("match")
		outputFmt, _ := cmd.Flags().GetString("output")

		if len(matchers) == 0 {
			return fmt.Errorf("at least one --match selector is required")
		}

		params := url.Values{}
		params.Set("stream", stream)
		for _, m := range matchers {
			params.Add("match[]", m)
		}
		optionalTimeParam(params, cmd, "from", "start")
		optionalTimeParam(params, cmd, "to", "end")

		body, err := promqlGet("prometheus/api/v1/series", params)
		if err != nil {
			return err
		}
		if outputFmt == "json" {
			printRawJSON(body)
			return nil
		}

		var resp struct {
			Status string              `json:"status"`
			Data   []map[string]string `json:"data"`
			Error  string              `json:"error,omitempty"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Println(string(body))
			return nil
		}
		if resp.Status == "error" {
			return fmt.Errorf("%s", resp.Error)
		}
		for _, series := range resp.Data {
			fmt.Println(formatPromqlLabels(series))
		}
		fmt.Printf("\ntotal=%d\n", len(resp.Data))
		return nil
	},
}

// ---------------------------------------------------------------------------
// 5. cardinality (parent) + subcommands
// ---------------------------------------------------------------------------

var promqlCardinalityCmd = &cobra.Command{
	Use:   "cardinality",
	Short: "Cardinality analysis for a metrics stream",
	Long:  "\nAnalyze label cardinality and active series in a Parseable metrics stream.",
}

type cardinalityEntry struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

// cardinality label-names
var promqlCardinalityLabelNamesCmd = &cobra.Command{
	Use:     "label-names",
	Short:   "Labels with the highest number of distinct values",
	Example: "  pb promql cardinality label-names --dataset otel_metrics --limit 20",
	Args:    cobra.NoArgs,
	PreRunE: PreRunDefaultProfile,
	RunE: func(cmd *cobra.Command, _ []string) error {
		stream, _ := cmd.Flags().GetString("dataset")
		lookback, _ := cmd.Flags().GetInt("lookback")
		limit, _ := cmd.Flags().GetInt("limit")
		selector, _ := cmd.Flags().GetString("selector")
		outputFmt, _ := cmd.Flags().GetString("output")

		params := url.Values{}
		params.Set("stream", stream)
		params.Set("lookback", fmt.Sprintf("%d", lookback))
		params.Set("limit", fmt.Sprintf("%d", limit))
		if selector != "" {
			params.Set("selector", selector)
		}

		body, err := promqlGet("prometheus/api/v1/cardinality/label_names", params)
		if err != nil {
			return err
		}
		if outputFmt == "json" {
			printRawJSON(body)
			return nil
		}

		var resp struct {
			Status string             `json:"status"`
			Data   []cardinalityEntry `json:"data"`
			Error  string             `json:"error,omitempty"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Println(string(body))
			return nil
		}
		if resp.Status == "error" {
			return fmt.Errorf("%s", resp.Error)
		}
		fmt.Printf("%-40s  %s\n", "LABEL", "DISTINCT VALUES")
		fmt.Println(strings.Repeat("-", 55))
		for _, e := range resp.Data {
			fmt.Printf("%-40s  %d\n", e.Name, e.Value)
		}
		return nil
	},
}

// cardinality label-values
var promqlCardinalityLabelValuesCmd = &cobra.Command{
	Use:     "label-values",
	Short:   "Series count per value for a specific label",
	Example: "  pb promql cardinality label-values --label job --dataset otel_metrics",
	Args:    cobra.NoArgs,
	PreRunE: PreRunDefaultProfile,
	RunE: func(cmd *cobra.Command, _ []string) error {
		stream, _ := cmd.Flags().GetString("dataset")
		labelName, _ := cmd.Flags().GetString("label")
		lookback, _ := cmd.Flags().GetInt("lookback")
		limit, _ := cmd.Flags().GetInt("limit")
		outputFmt, _ := cmd.Flags().GetString("output")

		params := url.Values{}
		params.Set("stream", stream)
		params.Set("lookback", fmt.Sprintf("%d", lookback))
		params.Set("limit", fmt.Sprintf("%d", limit))
		if labelName != "" {
			params.Set("label_name", labelName)
		}

		body, err := promqlGet("prometheus/api/v1/cardinality/label_values", params)
		if err != nil {
			return err
		}
		if outputFmt == "json" {
			printRawJSON(body)
			return nil
		}

		var resp struct {
			Status string             `json:"status"`
			Data   []cardinalityEntry `json:"data"`
			Error  string             `json:"error,omitempty"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Println(string(body))
			return nil
		}
		if resp.Status == "error" {
			return fmt.Errorf("%s", resp.Error)
		}
		fmt.Printf("%-40s  %s\n", "VALUE", "SERIES COUNT")
		fmt.Println(strings.Repeat("-", 55))
		for _, e := range resp.Data {
			fmt.Printf("%-40s  %d\n", e.Name, e.Value)
		}
		return nil
	},
}

// cardinality active-series
var promqlCardinalityActiveSeriesCmd = &cobra.Command{
	Use:     "active-series",
	Short:   "List currently active series",
	Example: "  pb promql cardinality active-series --dataset otel_metrics --selector '{job=\"api\"}'",
	Args:    cobra.NoArgs,
	PreRunE: PreRunDefaultProfile,
	RunE: func(cmd *cobra.Command, _ []string) error {
		stream, _ := cmd.Flags().GetString("dataset")
		lookback, _ := cmd.Flags().GetInt("lookback")
		limit, _ := cmd.Flags().GetInt("limit")
		selector, _ := cmd.Flags().GetString("selector")
		outputFmt, _ := cmd.Flags().GetString("output")

		params := url.Values{}
		params.Set("stream", stream)
		params.Set("lookback", fmt.Sprintf("%d", lookback))
		params.Set("limit", fmt.Sprintf("%d", limit))
		if selector != "" {
			params.Set("selector", selector)
		}

		body, err := promqlGet("prometheus/api/v1/cardinality/active_series", params)
		if err != nil {
			return err
		}
		if outputFmt == "json" {
			printRawJSON(body)
			return nil
		}

		var resp struct {
			Status string `json:"status"`
			Data   struct {
				TotalActiveSeries int                 `json:"total_active_series"`
				Series            []map[string]string `json:"series"`
			} `json:"data"`
			Error string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Println(string(body))
			return nil
		}
		if resp.Status == "error" {
			return fmt.Errorf("%s", resp.Error)
		}
		fmt.Printf("total_active_series=%d\n\n", resp.Data.TotalActiveSeries)
		for _, s := range resp.Data.Series {
			fmt.Println(formatPromqlLabels(s))
		}
		return nil
	},
}

// ---------------------------------------------------------------------------
// 6. active-queries — currently executing queries
// ---------------------------------------------------------------------------

var promqlActiveQueriesCmd = &cobra.Command{
	Use:     "active-queries",
	Aliases: []string{"ps"},
	Short:   "Show currently executing PromQL queries",
	Example: "  pb promql active-queries",
	Args:    cobra.NoArgs,
	PreRunE: PreRunDefaultProfile,
	RunE: func(_ *cobra.Command, _ []string) error {
		body, err := promqlGet("prometheus/api/v1/status/active_queries", nil)
		if err != nil {
			return err
		}

		var resp struct {
			Status string `json:"status"`
			Data   []struct {
				Query     string `json:"query"`
				Stream    string `json:"stream"`
				StartedAt string `json:"started_at"`
				ElapsedMs int    `json:"elapsed_ms"`
			} `json:"data"`
			Error string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			printRawJSON(body)
			return nil
		}
		if resp.Status == "error" {
			return fmt.Errorf("%s", resp.Error)
		}
		if len(resp.Data) == 0 {
			fmt.Println("No active queries.")
			return nil
		}
		fmt.Printf("%-50s  %-15s  %-22s  %s\n", "QUERY", "STREAM", "STARTED", "ELAPSED")
		fmt.Println(strings.Repeat("-", 100))
		for _, q := range resp.Data {
			query := q.Query
			if len(query) > 48 {
				query = query[:45] + "..."
			}
			fmt.Printf("%-50s  %-15s  %-22s  %dms\n", query, q.Stream, q.StartedAt, q.ElapsedMs)
		}
		return nil
	},
}

// ---------------------------------------------------------------------------
// 7. tsdb — TSDB statistics
// ---------------------------------------------------------------------------

var promqlTSDBCmd = &cobra.Command{
	Use:     "tsdb",
	Short:   "Show TSDB statistics for a metrics stream",
	Example: "  pb promql tsdb --dataset otel_metrics\n  pb promql tsdb --dataset otel_metrics --top 20 --focus-label job",
	Args:    cobra.NoArgs,
	PreRunE: PreRunDefaultProfile,
	RunE: func(cmd *cobra.Command, _ []string) error {
		stream, _ := cmd.Flags().GetString("dataset")
		topN, _ := cmd.Flags().GetInt("top")
		date, _ := cmd.Flags().GetString("date")
		focusLabel, _ := cmd.Flags().GetString("focus-label")
		outputFmt, _ := cmd.Flags().GetString("output")

		params := url.Values{}
		params.Set("stream", stream)
		params.Set("topN", fmt.Sprintf("%d", topN))
		if date != "" {
			params.Set("date", date)
		}
		if focusLabel != "" {
			params.Set("focusLabel", focusLabel)
		}

		body, err := promqlGet("prometheus/api/v1/status/tsdb", params)
		if err != nil {
			return err
		}
		if outputFmt == "json" {
			printRawJSON(body)
			return nil
		}

		var resp struct {
			Status string `json:"status"`
			Data   struct {
				TotalSeries          int                `json:"totalSeries"`
				TotalLabelValuePairs int                `json:"totalLabelValuePairs"`
				SeriesByMetric       []cardinalityEntry `json:"seriesCountByMetricName"`
				SeriesByLabel        []cardinalityEntry `json:"seriesCountByLabelName"`
				SeriesByFocusLabel   []cardinalityEntry `json:"seriesCountByFocusLabelValue"`
				LabelValueCount      []cardinalityEntry `json:"labelValueCountByLabelName"`
			} `json:"data"`
			Error string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Println(string(body))
			return nil
		}
		if resp.Status == "error" {
			return fmt.Errorf("%s", resp.Error)
		}

		d := resp.Data
		fmt.Printf("Total Series:       %d\n", d.TotalSeries)
		fmt.Printf("Total Label Pairs:  %d\n\n", d.TotalLabelValuePairs)

		if len(d.SeriesByMetric) > 0 {
			fmt.Println("Top metrics by series count:")
			for _, e := range d.SeriesByMetric {
				fmt.Printf("  %-50s  %d\n", e.Name, e.Value)
			}
			fmt.Println()
		}
		if len(d.SeriesByLabel) > 0 {
			fmt.Println("Top labels by series count:")
			for _, e := range d.SeriesByLabel {
				fmt.Printf("  %-40s  %d\n", e.Name, e.Value)
			}
			fmt.Println()
		}
		if len(d.SeriesByFocusLabel) > 0 {
			fmt.Printf("Series by %s value:\n", focusLabel)
			for _, e := range d.SeriesByFocusLabel {
				fmt.Printf("  %-40s  %d\n", e.Name, e.Value)
			}
			fmt.Println()
		}
		if len(d.LabelValueCount) > 0 {
			fmt.Println("Distinct values per label:")
			for _, e := range d.LabelValueCount {
				fmt.Printf("  %-40s  %d\n", e.Name, e.Value)
			}
		}
		return nil
	},
}

// ---------------------------------------------------------------------------
// Shared formatting helpers
// ---------------------------------------------------------------------------

func formatPromqlLabels(m map[string]string) string {
	name := m["__name__"]
	var labels []string
	for k, v := range m {
		if k != "__name__" {
			labels = append(labels, k+"=\""+v+"\"")
		}
	}
	if len(labels) == 0 {
		return name
	}
	if name == "" {
		return "{" + strings.Join(labels, ", ") + "}"
	}
	return fmt.Sprintf("%s{%s}", name, strings.Join(labels, ", "))
}

func promqlTS(v any) string {
	if f, ok := v.(float64); ok {
		return time.Unix(int64(f), 0).UTC().Format("2006-01-02T15:04:05Z")
	}
	return fmt.Sprintf("%v", v)
}
