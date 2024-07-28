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
	"fmt"
	"io"
	"os"
	"pb/pkg/config"
	"pb/pkg/model"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	startFlag      = "from"
	startFlagShort = "f"
	defaultStart   = "1m"

	endFlag      = "to"
	endFlagShort = "t"
	defaultEnd   = "now"

	// save filter flags
	saveFilterFlag     = "save-as"
	saveFilterTimeFlag = "keep-time"

	interactiveFlag      = "interactive"
	interactiveFlagShort = "i"
)

var query = &cobra.Command{
	Use:     "run [query] [flags]",
	Example: "  pb query run \"select * from frontend\" --from=10m --to=now",
	Short:   "Run SQL query on a log stream",
	Long:    "\nRun SQL query on a log stream. Default output format is json. Use -i flag to open interactive table view.",
	Args:    cobra.MaximumNArgs(1),
	PreRunE: PreRunDefaultProfile,
	RunE: func(command *cobra.Command, args []string) error {
		var query string

		// if no query is provided set it to default "select * from <steam-name>"
		// <steam-name> here is the first stream that server returns
		if len(args) == 0 || args[0] == "" || args[0] == " " {
			fmt.Println("please enter your query")
			fmt.Printf("Example:\n  pb query run \"select * from frontend\" --from=10m --to=now\n")
			return nil
		} else {
			query = args[0]
		}

		start, err := command.Flags().GetString(startFlag)
		if err != nil {
			return err
		}
		if start == "" {
			start = defaultStart
		}

		end, err := command.Flags().GetString(endFlag)
		if err != nil {
			return err
		}
		if end == "" {
			end = defaultEnd
		}

		interactive, err := command.Flags().GetBool(interactiveFlag)
		if err != nil {
			return err
		}

		startTime, endTime, err := parseTime(start, end)
		if err != nil {
			return err
		}

		keepTime, err := command.Flags().GetBool(saveFilterTimeFlag)
		if err != nil {
			return err
		}

		filterName, err := command.Flags().GetString(saveFilterFlag)
		filterNameTrimmed := strings.Trim(filterName, " ")
		if err != nil {
			return err
		}

		if interactive {
			p := tea.NewProgram(model.NewQueryModel(DefaultProfile, query, startTime, endTime), tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				fmt.Printf("there's been an error: %v", err)
				os.Exit(1)
			}
			return nil
		}

		// Checks if there is filter name which is not empty. Empty filter name wont be allowed
		if len(filterNameTrimmed) == 0 {
			fmt.Println("Enter a filter name")
			return nil
		} else if filterName != "DEFAULT_FILTER_NAME" {
			if keepTime {
				saveFilter(query, filterNameTrimmed, start, end)

			} else {
				saveFilter(query, filterNameTrimmed, "1m", "now")
			}
		}

		client := DefaultClient()
		return fetchData(&client, query, start, end)
	},
}

var QueryCmd = func() *cobra.Command {
	query.Flags().Bool(saveFilterTimeFlag, false, "Save the time range associated in the query to the filter") // save time for a filter flag; default value = false (boolean type)
	query.Flags().BoolP(interactiveFlag, interactiveFlagShort, false, "open the query result in interactive mode")
	query.Flags().StringP(startFlag, startFlagShort, defaultStart, "Start time for query. Takes date as '2024-10-12T07:20:50.52Z' or string like '10m', '1hr'")
	query.Flags().StringP(endFlag, endFlagShort, defaultEnd, "End time for query. Takes date as '2024-10-12T07:20:50.52Z' or 'now'")
	query.Flags().String(saveFilterFlag, "DEFAULT_FILTER_NAME", "Save a query filter") // save filter flag. Default value = DEFAULT_FILTER_NAME (type string)
	return query
}()

func fetchData(client *HTTPClient, query string, startTime string, endTime string) (err error) {
	queryTemplate := `{
    "query": "%s",
    "startTime": "%s",
    "endTime": "%s"
	}
	`

	finalQuery := fmt.Sprintf(queryTemplate, query, startTime, endTime)

	req, err := client.NewRequest("POST", "query", bytes.NewBuffer([]byte(finalQuery)))
	if err != nil {
		return
	}
	resp, err := client.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		fmt.Println(string(body))
	} else {
		io.Copy(os.Stdout, resp.Body)
	}
	return
}



// Returns start and end time for query in RFC3339 format
func parseTime(start, end string) (time.Time, time.Time, error) {
	if start == defaultStart && end == defaultEnd {
		return time.Now().Add(-1 * time.Minute), time.Now(), nil
	}

	startTime, err := time.Parse(time.RFC3339, start)
	if err != nil {
		// try parsing as duration
		duration, err := time.ParseDuration(start)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		startTime = time.Now().Add(-1 * duration)
	}

	endTime, err := time.Parse(time.RFC3339, end)
	if err != nil {
		if end == "now" {
			endTime = time.Now()
		} else {
			return time.Time{}, time.Time{}, err
		}
	}

	return startTime, endTime, nil
}

// fires a request to the server to save the filter with the associated user and stream
func saveFilter(query string, filterName string, startTime string, endTime string) (err error) {
	client := DefaultClient()
	userConfig, err := config.ReadConfigFromFile()
	if err != nil {
		return err
	}
	var userName string
	if profile, ok := userConfig.Profiles[userConfig.DefaultProfile]; ok {
		userName = profile.Username
	} else {
		fmt.Println("Default profile not found.")
		return
	}
	index := strings.Index(query, "from")
	fromPart := strings.TrimSpace(query[index+len("from"):])
	streamName := strings.Fields(fromPart)[0]

	start, end, err := parseTimeToUTC(startTime, endTime)
	if err != nil {
		fmt.Println("Oops something went wrong!!!!")
		return
	}

	queryTemplate := `{
		"filter_type":"sql",
		"filter_query": "%s"
		}`

	timeTemplate := `{
        "from": "%s",
        "to":  "%s"
    }`

	saveFilterTemplate := `
	{
    "stream_name": "%s",
    "filter_name": "%s",
    "user_id": "%s",
    "query": %s,
    "time_filter": %s  
    }`

	queryField := fmt.Sprintf(queryTemplate, query)
	timeField := fmt.Sprintf(timeTemplate, start, end)
	final := fmt.Sprintf(saveFilterTemplate, streamName, filterName, userName, queryField, timeField)

	req, err := client.NewRequest("POST", "filters", bytes.NewBuffer([]byte(final)))
	if err != nil {
		return
	}

	resp, err := client.client.Do(req)
	if err != nil {
		return
	}

	if resp.StatusCode != 200 {
		fmt.Printf("\nSomething went wrong")
	}

	return err
}

// parses a time duration to supported utc format
func parseTimeToUTC(start, end string) (time.Time, time.Time, error) {
	if start == defaultStart && end == defaultEnd {
		now := time.Now().UTC()
		return now.Add(-1 * time.Minute), now, nil
	}

	startTime, err := time.Parse(time.RFC3339, start)
	if err != nil {
		duration, err := time.ParseDuration(start)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		startTime = time.Now().Add(-1 * duration).UTC()
	} else {
		startTime = startTime.UTC()
	}

	endTime, err := time.Parse(time.RFC3339, end)
	if err != nil {
		if end == "now" {
			endTime = time.Now().UTC()
		} else {
			return time.Time{}, time.Time{}, err
		}
	} else {
		endTime = endTime.UTC()
	}

	return startTime, endTime, nil
}
