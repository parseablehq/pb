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
	"os"
	"strings"
	"time"

	// "pb/pkg/model"

	//! This dependency is required by the interactive flag Do not remove
	// tea "github.com/charmbracelet/bubbletea"
	internalHTTP "pb/pkg/http"

	"github.com/spf13/cobra"
)

var (
	startFlag      = "from"
	startFlagShort = "f"
	defaultStart   = "1m"

	endFlag      = "to"
	endFlagShort = "t"
	defaultEnd   = "now"

	outputFlag = "output"
)

var query = &cobra.Command{
	Use:     "run [query] [flags]",
	Example: "  pb query run \"select * from frontend\" --from=10m --to=now",
	Short:   "Run SQL query on a log stream",
	Long:    "\nRun SQL query on a log stream. Default output format is text. Use --output flag to set output format to json.",
	Args:    cobra.MaximumNArgs(1),
	PreRunE: PreRunDefaultProfile,
	RunE: func(command *cobra.Command, args []string) error {
		startTime := time.Now()
		command.Annotations = map[string]string{
			"startTime": startTime.Format(time.RFC3339),
		}

		defer func() {
			duration := time.Since(startTime)
			command.Annotations["executionTime"] = duration.String()
		}()

		if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
			fmt.Println("Please enter your query")
			fmt.Printf("Example:\n  pb query run \"select * from frontend\" --from=10m --to=now\n")
			return nil
		}

		query := args[0]
		start, err := command.Flags().GetString(startFlag)
		if err != nil {
			command.Annotations["error"] = err.Error()
			return err
		}
		if start == "" {
			start = defaultStart
		}

		end, err := command.Flags().GetString(endFlag)
		if err != nil {
			command.Annotations["error"] = err.Error()
			return err
		}
		if end == "" {
			end = defaultEnd
		}

		outputFormat, err := command.Flags().GetString("output")
		if err != nil {
			command.Annotations["error"] = err.Error()
			return fmt.Errorf("failed to get 'output' flag: %w", err)
		}

		client := internalHTTP.DefaultClient(&DefaultProfile)
		err = fetchData(&client, query, start, end, outputFormat)
		if err != nil {
			command.Annotations["error"] = err.Error()
		}
		return err
	},
}

func init() {
	query.Flags().StringP(startFlag, startFlagShort, defaultStart, "Start time for query.")
	query.Flags().StringP(endFlag, endFlagShort, defaultEnd, "End time for query.")
	query.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (text|json)")
}

var QueryCmd = query

func fetchData(client *internalHTTP.HTTPClient, query string, startTime, endTime, outputFormat string) error {
	queryTemplate := `{
		"query": "%s",
		"startTime": "%s",
		"endTime": "%s"
	}`
	finalQuery := fmt.Sprintf(queryTemplate, query, startTime, endTime)

	req, err := client.NewRequest("POST", "query", bytes.NewBuffer([]byte(finalQuery)))
	if err != nil {
		return fmt.Errorf("failed to create new request: %w", err)
	}

	resp, err := client.Client.Do(req)
	if err != nil {
		return fmt.Errorf("request execution failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		fmt.Println(string(body))
		return fmt.Errorf("non-200 status code received: %s", resp.Status)
	}

	if outputFormat == "json" {
		var jsonResponse []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&jsonResponse); err != nil {
			return fmt.Errorf("error decoding JSON response: %w", err)
		}
		encodedResponse, _ := json.MarshalIndent(jsonResponse, "", "  ")
		fmt.Println(string(encodedResponse))
	} else {
		io.Copy(os.Stdout, resp.Body)
	}
	return nil
}

// Returns start and end time for query in RFC3339 format
// func parseTime(start, end string) (time.Time, time.Time, error) {
// 	if start == defaultStart && end == defaultEnd {
// 		return time.Now().Add(-1 * time.Minute), time.Now(), nil
// 	}

// 	startTime, err := time.Parse(time.RFC3339, start)
// 	if err != nil {
// 		// try parsing as duration
// 		duration, err := time.ParseDuration(start)
// 		if err != nil {
// 			return time.Time{}, time.Time{}, err
// 		}
// 		startTime = time.Now().Add(-1 * duration)
// 	}

// 	endTime, err := time.Parse(time.RFC3339, end)
// 	if err != nil {
// 		if end == "now" {
// 			endTime = time.Now()
// 		} else {
// 			return time.Time{}, time.Time{}, err
// 		}
// 	}

// 	return startTime, endTime, nil
// }

// // create a request body for saving filter without time_filter
// func createFilter(query string, filterName string) (err error) {
// 	userConfig, err := config.ReadConfigFromFile()
// 	if err != nil {
// 		return err
// 	}

// 	var userName string
// 	if profile, ok := userConfig.Profiles[userConfig.DefaultProfile]; ok {
// 		userName = profile.Username
// 	} else {
// 		fmt.Println("Default profile not found.")
// 		return
// 	}

// 	index := strings.Index(query, "from")
// 	fromPart := strings.TrimSpace(query[index+len("from"):])
// 	streamName := strings.Fields(fromPart)[0]

// 	queryTemplate := `{
// 		"filter_type":"sql",
// 		"filter_query": "%s"
// 		}`

// 	saveFilterTemplate := `
// 	{
//     "stream_name": "%s",
//     "filter_name": "%s",
//     "user_id": "%s",
//     "query": %s,
//     "time_filter": null
//     }`

// 	queryField := fmt.Sprintf(queryTemplate, query)

// 	finalQuery := fmt.Sprintf(saveFilterTemplate, streamName, filterName, userName, queryField)

// 	saveFilterToServer(finalQuery)

// 	return err
// }

// // create a request body for saving filter with time_filter
// func createFilterWithTime(query string, filterName string, startTime string, endTime string) (err error) {
// 	userConfig, err := config.ReadConfigFromFile()
// 	if err != nil {
// 		return err
// 	}

// 	var userName string
// 	if profile, ok := userConfig.Profiles[userConfig.DefaultProfile]; ok {
// 		userName = profile.Username
// 	} else {
// 		fmt.Println("Default profile not found.")
// 		return
// 	}

// 	index := strings.Index(query, "from")
// 	fromPart := strings.TrimSpace(query[index+len("from"):])
// 	streamName := strings.Fields(fromPart)[0]

// 	start, end, err := parseTimeToUTC(startTime, endTime)
// 	if err != nil {
// 		fmt.Println("Oops something went wrong!!!!")
// 		return err
// 	}

// 	queryTemplate := `{
// 		"filter_type":"sql",
// 		"filter_query": "%s"
// 		}`

// 	timeTemplate := `{
// 			"from": "%s",
// 			"to":  "%s"
// 		}`
// 	timeField := fmt.Sprintf(timeTemplate, start, end)

// 	saveFilterTemplate := `
// 	{
//     "stream_name": "%s",
//     "filter_name": "%s",
//     "user_id": "%s",
//     "query": %s,
//     "time_filter": %s
//     }`

// 	queryField := fmt.Sprintf(queryTemplate, query)

// 	finalQuery := fmt.Sprintf(saveFilterTemplate, streamName, filterName, userName, queryField, timeField)

// 	saveFilterToServer(finalQuery)

// 	return err
// }

// // fires a request to the server to save the filter with the associated user and stream
// func saveFilterToServer(finalQuery string) (err error) {
// 	client := DefaultClient()

// 	req, err := client.NewRequest("POST", "filters", bytes.NewBuffer([]byte(finalQuery)))
// 	if err != nil {
// 		return
// 	}

// 	resp, err := client.client.Do(req)
// 	if err != nil {
// 		return
// 	}

// 	if resp.StatusCode != 200 {
// 		fmt.Printf("\nSomething went wrong")
// 	}

// 	return err
// }

// // parses a time duration to supported utc format
// func parseTimeToUTC(start, end string) (time.Time, time.Time, error) {
// 	if start == defaultStart && end == defaultEnd {
// 		now := time.Now().UTC()
// 		return now.Add(-1 * time.Minute), now, nil
// 	}

// 	startTime, err := time.Parse(time.RFC3339, start)
// 	if err != nil {
// 		duration, err := time.ParseDuration(start)
// 		if err != nil {
// 			return time.Time{}, time.Time{}, err
// 		}
// 		startTime = time.Now().Add(-1 * duration).UTC()
// 	} else {
// 		startTime = startTime.UTC()
// 	}

// 	endTime, err := time.Parse(time.RFC3339, end)
// 	if err != nil {
// 		if end == "now" {
// 			endTime = time.Now().UTC()
// 		} else {
// 			return time.Time{}, time.Time{}, err
// 		}
// 	} else {
// 		endTime = endTime.UTC()
// 	}

// 	return startTime, endTime, nil
// }
