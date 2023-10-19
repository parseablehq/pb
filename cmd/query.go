// Copyright (c) 2023 Cloudnatively Services Pvt Ltd
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
	"pb/pkg/model"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	durationFlag      = "duration"
	durationFlagShort = "d"
	defaultDuration   = "10"

	startFlag      = "from"
	startFlagShort = "f"
	defaultStart   = "1m"

	endFlag      = "to"
	endFlagShort = "t"
	defaultEnd   = "now"
)

var queryInteractive = &cobra.Command{
	Use:     "i [stream-name] --duration 10",
	Example: "  pb query frontend --duration 10",
	Short:   "Interactive query table view",
	Long:    "\n command is used to open a prompt to query a stream.",
	Args:    cobra.ExactArgs(1),
	PreRunE: PreRunDefaultProfile,
	RunE: func(command *cobra.Command, args []string) error {
		stream := args[0]
		duration, _ := command.Flags().GetString(durationFlag)

		if duration == "" {
			duration = defaultDuration
		}
		durationInt, err := strconv.Atoi(duration)
		if err != nil {
			return err
		}

		p := tea.NewProgram(model.NewQueryModel(DefaultProfile, stream, uint(durationInt)), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Printf("there's been an error: %v", err)
			os.Exit(1)
		}

		return nil
	},
}

var queryJSON = &cobra.Command{
	Use:     "query [query] --from=10m --to=now",
	Example: "  pb query \"select * from frontend\" --from=10m --to=now",
	Short:   "Run SQL query",
	Long:    "\nquery command is used to run query. Output format is json string",
	Args:    cobra.ExactArgs(1),
	PreRunE: PreRunDefaultProfile,
	RunE: func(command *cobra.Command, args []string) error {
		query := args[0]
		start, _ := command.Flags().GetString(startFlag)
		end, _ := command.Flags().GetString(endFlag)

		if start == "" {
			start = defaultStart
		}
		if end == "" {
			end = defaultEnd
		}

		client := DefaultClient()
		return fetchData(&client, query, start, end)
	},
}

var QueryInteractiveCmd = func() *cobra.Command {
	queryInteractive.Flags().StringP(durationFlag, durationFlagShort, defaultDuration, "specify the duration in minutes for which queries should be executed. Defaults to 10 minutes")
	return queryInteractive
}()

var QueryCmd = func() *cobra.Command {
	queryJSON.Flags().StringP(startFlag, startFlagShort, defaultStart, "Specify start datetime of query. Supports RFC3999 time format and durations (ex. 10m, 1hr ..) ")
	queryJSON.Flags().StringP(endFlag, endFlagShort, defaultEnd, "Specify end datetime of query. Supports RFC3999 time format and literal - now ")
	queryJSON.AddCommand(queryInteractive)
	return queryJSON
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
