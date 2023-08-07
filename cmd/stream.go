// Copyright (c) 2023 Cloudnatively Services Pvt Ltd
//
// This file is part of MinIO Object Storage stack
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
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

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

type StreamRetentionData []struct {
	Description string `json:"description"`
	Action      string `json:"action"`
	Duration    string `json:"duration"`
}

type StreamAlertData struct {
	Alerts []struct {
		Message string `json:"message"`
		Name    string `json:"name"`
		Rule    struct {
			Config struct {
				Column   string `json:"column"`
				Operator string `json:"operator"`
				Repeats  int    `json:"repeats"`
				Value    int    `json:"value"`
			} `json:"config"`
			Type string `json:"type"`
		} `json:"rule"`
		Targets []struct {
			Endpoint string `json:"endpoint"`
			Password string `json:"password,omitempty"`
			Repeat   struct {
				Interval string `json:"interval"`
				Times    int    `json:"times"`
			} `json:"repeat"`
			SkipTLSCheck bool   `json:"skip_tls_check,omitempty"`
			Type         string `json:"type"`
			Username     string `json:"username,omitempty"`
			Headers      struct {
				Authorization string `json:"Authorization"`
			} `json:"headers,omitempty"`
		} `json:"targets"`
	} `json:"alerts"`
	Version string `json:"version"`
}

var AddStreamCmd = &cobra.Command{
	Use:     "add stream-name",
	Example: "  pb stream add backend_logs",
	Short:   "Create a new stream",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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
			fmt.Printf("Created stream %s\n", styleBold.Render(name))
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

var StatStreamCmd = &cobra.Command{
	Use:     "info stream-name",
	Example: "  pb stream info backend_logs",
	Short:   "Get statistics for a stream",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		client := DefaultClient()

		stats, err := fetchStats(&client, name)
		if err != nil {
			return err
		}

		ingestion_count := stats.Ingestion.Count
		ingestion_size, _ := strconv.Atoi(strings.TrimRight(stats.Ingestion.Size, " Bytes"))
		storage_size, _ := strconv.Atoi(strings.TrimRight(stats.Storage.Size, " Bytes"))

		retention, err := fetchRetention(&client, name)
		if err != nil {
			return err
		}

		is_rentention_set := len(retention) > 0

		fmt.Println(styleBold.Render("Info:"))
		fmt.Printf("  Event Count:     %d\n", ingestion_count)
		fmt.Printf("  Ingestion Size:  %s\n", humanize.Bytes(uint64(ingestion_size)))
		fmt.Printf("  Storage Size:    %s\n", humanize.Bytes(uint64(storage_size)))
		fmt.Println()

		if is_rentention_set {
			fmt.Println(styleBold.Render("Retention:"))
			for _, item := range retention {
				fmt.Printf("  Action:    %s\n", styleBold.Render(item.Action))
				fmt.Printf("  Duration:  %s\n", styleBold.Render(item.Duration))
				fmt.Println()
			}
		} else {
			fmt.Println(styleBold.Render("No retention period set on stream\n"))
		}

		alerts_data, err := fetchAlerts(&client, name)
		if err != nil {
			return err
		}
		alerts := alerts_data.Alerts

		is_alerts_set := len(alerts) > 0

		if is_alerts_set {
			fmt.Println(styleBold.Render("Alerts:"))
			for _, alert := range alerts {
				fmt.Printf("  Alert:   %s\n", styleBold.Render(alert.Name))
				rule_fmt := fmt.Sprintf(
					"%s %s %s repeated %d times",
					alert.Rule.Config.Column,
					alert.Rule.Config.Operator,
					fmt.Sprint(alert.Rule.Config.Value),
					alert.Rule.Config.Repeats,
				)
				fmt.Printf("  Rule:    %s\n", rule_fmt)
				fmt.Printf("  Targets: ")
				for _, target := range alert.Targets {
					fmt.Printf("%s, ", target.Type)
				}
				fmt.Print("\n\n")
			}
		} else {
			fmt.Println(styleBold.Render("No alerts set on stream\n"))
		}

		return nil
	},
}

var RemoveStreamCmd = &cobra.Command{
	Use:     "remove stream-name",
	Aliases: []string{"rm"},
	Example: " pb stream remove backend_logs",
	Short:   "Delete a stream",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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
			fmt.Printf("Removed stream %s", styleBold.Render(name))
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
	Example: " pb stream list",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := DefaultClient()
		req, err := client.NewRequest("GET", "logstream", nil)
		if err != nil {
			return err
		}

		resp, err := client.client.Do(req)
		if err != nil {
			return err
		}

		if resp.StatusCode == 200 {
			items := []map[string]string{}
			err = json.NewDecoder(resp.Body).Decode(&items)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			for _, item := range items {
				fmt.Println(item["name"])
			}
		} else {
			bytes, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			body := string(bytes)
			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		}

		return nil
	},
}

func fetchStats(client *HttpClient, name string) (data StreamStatsData, err error) {
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
		return
	} else {
		body := string(bytes)
		body = fmt.Sprintf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		err = errors.New(body)
	}
	return
}

func fetchRetention(client *HttpClient, name string) (data StreamRetentionData, err error) {
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
		return
	} else {
		body := string(bytes)
		body = fmt.Sprintf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		err = errors.New(body)
	}
	return
}

func fetchAlerts(client *HttpClient, name string) (data StreamAlertData, err error) {
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
		return
	} else {
		body := string(bytes)
		body = fmt.Sprintf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		err = errors.New(body)
	}
	return
}
