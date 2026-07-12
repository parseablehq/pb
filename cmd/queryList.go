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
	"strings"

	"github.com/parseablehq/pb/pkg/config"
	internalHTTP "github.com/parseablehq/pb/pkg/http"
	"github.com/parseablehq/pb/pkg/model"
	"github.com/spf13/cobra"
)

var SavedQueryList = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Example:      "pb sql list [-o | --output]",
	Short:        "List of saved queries",
	Long:         "\nShow the list of saved queries for active user",
	SilenceUsage: true,
	PreRunE:      PreRunDefaultProfile,
	RunE: func(_ *cobra.Command, _ []string) error {
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

			client := internalHTTP.DefaultClient(&userProfile)
			userSavedQueries := fetchFilters(&client)
			// Collect all filter titles in a slice and join with commas
			var filterDetails []string

			if outputFlag == "json" {
				// If JSON output is requested, marshal the saved queries to JSON
				jsonOutput, err := json.MarshalIndent(userSavedQueries, "", "  ")
				if err != nil {
					fmt.Println("Error converting saved queries to JSON:", err)
					return err
				}
				if string(jsonOutput) == "null" {
					fmt.Println("[]")
					return nil
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
			return nil

		}

		// Normal Saved Queries Menu if output flag not set
		p := model.SavedQueriesMenu()
		if _, err := p.Run(); err != nil {
			return err
		}

		a := model.QueryToApply()
		d := model.QueryToDelete()
		if a.SavedQueryID() != "" || strings.TrimSpace(a.Stream()) != "" {
			if err := savedQueryToPbQuery(a.Stream(), a.StartTime(), a.EndTime()); err != nil {
				return err
			}
		}
		if d.SavedQueryID() != "" {
			deleteSavedQuery(&client, d.SavedQueryID(), d.Title())
		}
		return nil
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

// Convert a saved query to executable SQL query and print results to terminal.
func savedQueryToPbQuery(sqlQuery string, start string, end string) error {
	if strings.TrimSpace(sqlQuery) == "" {
		fmt.Println("Empty query selected.")
		return nil
	}

	if start == "" {
		start = "1h"
	}
	if end == "" {
		end = "now"
	}

	sqlQuery = quoteStreamNames(sqlQuery)
	sqlQuery = quoteFieldsWithDots(sqlQuery)

	fmt.Printf("Query: %s\n", sqlQuery)

	client := internalHTTP.DefaultClient(&DefaultProfile)
	err := fetchData(&client, sqlQuery, start, end, "")
	if err != nil {
		return fmt.Errorf("selected saved query failed: %w", err)
	}
	return nil
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

func fetchFilters(client *internalHTTP.HTTPClient) []Item {
	req, err := client.NewRequest("GET", "filters", nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return nil
	}

	resp, err := client.Client.Do(req)
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
		if filter.Query.FilterQuery == nil {
			continue
		}
		userSavedQuery := Item{
			ID:     filter.FilterID,
			Title:  filter.FilterName,
			Stream: filter.StreamName,
			Desc:   *filter.Query.FilterQuery,
			From:   filter.TimeFilter.From,
			To:     filter.TimeFilter.To,
		}
		userSavedQueries = append(userSavedQueries, userSavedQuery)
	}
	return userSavedQueries
}
