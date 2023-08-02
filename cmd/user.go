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
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

var AddUserCmd = &cobra.Command{
	Use:     "add name",
	Example: "add bob",
	Short:   "Add a new user",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		client := DefaultClient()
		req, err := client.NewRequest("PUT", "user/"+name, nil)
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
		body := string(bytes)
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			fmt.Printf("Added user %s \nPassword is: %s\n", name, body)
		} else {
			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		}

		return nil
	},
}

var DeleteUserCmd = &cobra.Command{
	Use:     "remove name",
	Example: "remove bob",
	Short:   "Delete a user",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		client := DefaultClient()
		req, err := client.NewRequest("DELETE", "user/"+name, nil)
		if err != nil {
			return err
		}

		resp, err := client.client.Do(req)
		if err != nil {
			return err
		}

		if resp.StatusCode == 200 {
			fmt.Printf("Removed user %s", styleBold.Render(name))
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

var ListUserCmd = &cobra.Command{
	Use:   "list",
	Short: "list users",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := DefaultClient()
		req, err := client.NewRequest("GET", "user", nil)
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
			items := []string{}
			err = json.Unmarshal(bytes, &items)
			if err != nil {
				return err
			}
			for _, item := range items {
				fmt.Println(item)
			}
		} else {
			body := string(bytes)
			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		}

		return nil
	},
}
