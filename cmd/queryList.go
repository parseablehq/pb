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
	"os"
	"pb/pkg/config"
	internalHTTP "pb/pkg/http"
	"pb/pkg/model"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var SavedQueryList = &cobra.Command{
	Use:     "list",
	Example: "pb query list [-o | --output]",
	Short:   "List of saved queries",
	Long:    "\nShow the list of saved queries for active user",
	PreRunE: PreRunDefaultProfile,
	Run: func(_ *cobra.Command, _ []string) {
		client := internalHTTP.DefaultClient(&DefaultProfile)

		// Check if the output flag is set
		if outputFlag != "" {
			// Display all filters if output flag is set
			userConfig, err := config.ReadConfigFromFile()
			if err != nil {
				fmt.Println("Error reading Default Profile")
			}
			var userProfile config.Profile
			if profile, ok := userConfig.Profiles[userConfig.DefaultProfile]; ok {
				userProfile = profile
			}

			client := &http.Client{
				Timeout: time.Second * 60,
			}
			userSavedQueries := fetchFilters(client, &userProfile)
			// Collect all filter titles in a slice and join with commas
			var filterDetails []string

			if outputFlag == "json" {
				// If JSON output is requested, marshal the saved queries to JSON
				jsonOutput, err := json.MarshalIndent(userSavedQueries, "", "  ")
				if err != nil {
					fmt.Println("Error converting saved queries to JSON:", err)
					return
				}
				fmt.Println(string(jsonOutput))
			} else {
				for _, query := range userSavedQueries {
					// Build the line conditionally
					var parts []string
					if query.Title != "" {
						parts = append(parts, query.Title)
					}
					if query.Stream != "" {
						parts = append(parts, query.Stream)
					}
					if query.Desc != "" {
						parts = append(parts, query.Desc)
					}
					if query.From != "" {
						parts = append(parts, query.From)
					}
					if query.To != "" {
						parts = append(parts, query.To)
					}

					// Join parts with commas and print each query on a new line
					fmt.Println(strings.Join(parts, ", "))
				}
			}
			// Print all titles as a single line, comma-separated
			fmt.Println(strings.Join(filterDetails, " "))
			return

		}

		// Normal Saved Queries Menu if output flag not set
		p := model.SavedQueriesMenu()
		if _, err := p.Run(); err != nil {
			os.Exit(1)
		}

		a := model.QueryToApply()
		d := model.QueryToDelete()
		if a.Stream() != "" {
			savedQueryToPbQuery(a.Stream(), a.StartTime(), a.EndTime())
		}
		if d.SavedQueryID() != "" {
			deleteSavedQuery(&client, d.SavedQueryID(), d.Title())
		}
	},
}

// Delete a saved query from the list.
func deleteSavedQuery(client *internalHTTP.HTTPClient, savedQueryID, title string) {
	fmt.Printf("\nAttempting to delete '%s'", title)
	deleteURL := `filters/` + savedQueryID
	req, err := client.NewRequest("DELETE", deleteURL, nil)
	if err != nil {
		fmt.Println("Failed to delete the saved query with error: ", err)
	}

	resp, err := client.Client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		fmt.Printf("\nSaved Query deleted\n\n")
	}
}

// Convert a saved query to executable pb query
func savedQueryToPbQuery(query string, start string, end string) {
	var timeStamps string
	if start == "" || end == "" {
		timeStamps = ``
	} else {
		startFormatted := formatToRFC3339(start)
		endFormatted := formatToRFC3339(end)
		timeStamps = ` --from=` + startFormatted + ` --to=` + endFormatted
	}
	_ = `pb query run ` + query + timeStamps
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

func init() {
	// Add the output flag to the SavedQueryList command
	SavedQueryList.Flags().StringVarP(&outputFlag, "output", "o", "", "Output format (text or json)")
}

type Item struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Stream string `json:"stream"`
	Desc   string `json:"desc"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
}

func fetchFilters(client *http.Client, profile *config.Profile) []Item {
	endpoint := fmt.Sprintf("%s/%s", profile.URL, "api/v1/filters")
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return nil
	}

	req.SetBasicAuth(profile.Username, profile.Password)
	req.Header.Add("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error making request:", err)
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response body:", err)
		return nil
	}

	var filters []model.Filter
	err = json.Unmarshal(body, &filters)
	if err != nil {
		fmt.Println("Error unmarshalling response:", err)
		return nil
	}

	// This returns only the SQL type filters
	var userSavedQueries []Item
	for _, filter := range filters {

		queryBytes, _ := json.Marshal(filter.Query.FilterQuery)

		userSavedQuery := Item{
			ID:     filter.FilterID,
			Title:  filter.FilterName,
			Stream: filter.StreamName,
			Desc:   string(queryBytes),
			From:   filter.TimeFilter.From,
			To:     filter.TimeFilter.To,
		}
		userSavedQueries = append(userSavedQueries, userSavedQuery)

	}
	return userSavedQueries
}
