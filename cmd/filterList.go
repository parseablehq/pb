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
	"fmt"
	"os"
	"strings"
	"time"

	"pb/pkg/model"

	"github.com/spf13/cobra"
)

var FilterList = &cobra.Command{
	Use:     "list",
	Example: "pb query list ",
	Short:   "List of saved filter for a stream",
	Long:    "\nShow a list of saved filter for a stream ",
	PreRunE: PreRunDefaultProfile,
	Run: func(_ *cobra.Command, _ []string) {
		client := DefaultClient()

		p := model.UIApp()
		_, err := p.Run()
		if err != nil {
			os.Exit(1)
		}

		a := model.FilterToApply()
		d := model.FilterToDelete()
		if a.Stream() != "" {
			filterToPbQuery(a.Stream(), a.StartTime(), a.EndTime())
		}
		if d.FilterID() != "" {
			deleteFilter(&client, d.FilterID())
		}
	},
}

// Delete a saved filter from the list of filter
func deleteFilter(client *HTTPClient, filterID string) {
	if filterID == "" {
		return
	}
	deleteURL := `filters/filter/` + filterID
	req, err := client.NewRequest("DELETE", deleteURL, nil)
	if err != nil {
		fmt.Println("Error deleting the filter")
	}

	resp, err := client.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		fmt.Printf("\n\nFilter Deleted")
	}
}

// Convert a filter to executable pb query
func filterToPbQuery(query string, start string, end string) {
	if query == "" {
		return
	}
	var timeStamps string
	if start == "" || end == "" {
		timeStamps = ``
	} else {
		startFormatted := formatToRFC3339(start)
		endFormatted := formatToRFC3339(end)
		timeStamps = ` --from=` + startFormatted + ` --to=` + endFormatted
	}
	queryTemplate := `pb query run ` + query + timeStamps
	fmt.Printf("\nCopy and paste the command")
	fmt.Printf("\n\n%s\n\n", queryTemplate)
}

// Parses all UTC time format from string to time interface
func parseTimeToFormat(input string) (time.Time, error) {
	// List of possible formats
	formats := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
		"01/02/2006 15:04:05",
		"02-Jan-2006 15:04:05 MST",
		"2006-01-02T15:04:05Z",
		"02-Jan-2006",
	}

	var err error
	var t time.Time

	for _, format := range formats {
		t, err = time.Parse(format, input)
		if err == nil {
			return t, nil
		}
	}

	return t, fmt.Errorf("unable to parse time: %s", input)
}

// Converts to RFC3339
func convertTime(input string) (string, error) {
	t, err := parseTimeToFormat(input)
	if err != nil {
		return "", err
	}

	return t.Format(time.RFC3339), nil
}

// Converts User inputted time to string type RFC3339 time
func formatToRFC3339(time string) string {
	var formattedTime string
	if len(strings.Fields(time)) > 1 {
		newTime := strings.Fields(time)[0:2]
		rfc39990time, err := convertTime(strings.Join(newTime, " "))
		if err != nil {
			fmt.Println("error formatting time")
		}
		formattedTime = rfc39990time
	} else {
		rfc39990time, err := convertTime(time)
		if err != nil {
			fmt.Println("error formatting time")
		}
		formattedTime = rfc39990time
	}
	return formattedTime
}
