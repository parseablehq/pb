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
	"errors"
	"fmt"
	"io"
	"os"
	"pb/pkg/model"
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

	interactiveFlag      = "interactive"
	interactiveFlagShort = "i"
)

var query = &cobra.Command{
	Use:     "query [query] [flags]",
	Example: "  pb query \"select * from frontend\" --from=10m --to=now",
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
			fmt.Printf("Example:\n  pb query \"select * from frontend\" --from=10m --to=now")
			return nil

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

		if interactive {
			p := tea.NewProgram(model.NewQueryModel(DefaultProfile, query, startTime, endTime), tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				fmt.Printf("there's been an error: %v", err)
				os.Exit(1)
			}
			return nil
		}

		client := DefaultClient()
		return fetchData(&client, query, start, end)
	},
}

var QueryCmd = func() *cobra.Command {
	query.Flags().BoolP(interactiveFlag, interactiveFlagShort, false, "open the query result in interactive mode")
	query.Flags().StringP(startFlag, startFlagShort, defaultStart, "Start time for query. Takes date as '2024-10-12T07:20:50.52Z' or string like '10m', '1hr'")
	query.Flags().StringP(endFlag, endFlagShort, defaultEnd, "End time for query. Takes date as '2024-10-12T07:20:50.52Z' or 'now'")
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

func fetchFirstStream() (string, error) {
	client := DefaultClient()
	req, err := client.NewRequest("GET", "logstream", nil)
	if err != nil {
		return "", err
	}

	resp, err := client.client.Do(req)
	if err != nil {
		return "", err
	}

	if resp.StatusCode == 200 {
		items := []map[string]string{}
		if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
			return "", err
		}
		defer resp.Body.Close()

		if len(items) == 0 {
			return "", errors.New("no stream found on the server, please create a stream to proceed")
		}
		// return with the first stream that is present in the list
		for _, v := range items {
			return v["name"], nil
		}
	}
	return "", fmt.Errorf("received error status code %d from server", resp.StatusCode)
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
