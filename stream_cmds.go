package main

import (
	"encoding/json"
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

var CreateStreamCmd = &cobra.Command{
	Use:     "create name",
	Example: "add backend_logs",
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
			fmt.Printf("Created stream %s\n", name)
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
	Use:     "info name",
	Example: "info backend_logs",
	Short:   "Get statistics for a stream",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		client := DefaultClient()
		req, err := client.NewRequest("GET", fmt.Sprintf("logstream/%s/stats", name), nil)
		if err != nil {
			return err
		}

		resp, err := client.client.Do(req)
		if err != nil {
			return err
		}

		bytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			var stats StreamStatsData
			err = json.Unmarshal(bytes, &stats)
			ingestion_count := stats.Ingestion.Count
			ingestion_size, _ := strconv.Atoi(strings.TrimRight(stats.Ingestion.Size, " Bytes"))
			storage_size, _ := strconv.Atoi(strings.TrimRight(stats.Storage.Size, " Bytes"))

			if err != nil {
				return err
			}

			fmt.Printf("event_count: %d\n", ingestion_count)
			fmt.Printf("ingestion_size: %s\n", humanize.Bytes(uint64(ingestion_size)))
			fmt.Printf("storage_size: %s\n", humanize.Bytes(uint64(storage_size)))

		} else {
			body := string(bytes)
			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		}

		return nil
	},
}

var DeleteStreamCmd = &cobra.Command{
	Use:     "delete name",
	Example: "delete backend_logs",
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
			fmt.Printf("Removed stream %s", name)
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
	Use:   "list",
	Short: "list streams",
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
