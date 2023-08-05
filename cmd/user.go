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
	"net/http"
	"pb/pkg/config"
	"sync"

	"github.com/spf13/cobra"
)

type UserRoleData struct {
	Privilege string `json:"privilege"`
	Resource  struct {
		Stream string `json:"stream"`
		Tag    string `json:"tag"`
	} `json:"resource"`
}

type FetchUserRoleRes struct {
	data []UserRoleData
	err  error
}

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
			users := []string{}
			err = json.Unmarshal(bytes, &users)
			if err != nil {
				return err
			}

			client = DefaultClient()
			role_responses := make([]FetchUserRoleRes, len(users))

			wsg := sync.WaitGroup{}
			wsg.Add(len(users))
			for idx, user := range users {
				idx := idx
				user := user
				go func() {
					role_responses[idx] = fetchUserRoles(&client.client, &DefaultProfile, user)
					wsg.Done()
				}()
			}
			wsg.Wait()
			for idx, user := range users {
				roles := role_responses[idx]
				fmt.Println(user)
				if roles.err == nil {
					for _, role := range roles.data {
						fmt.Printf("  %s @ %s %s\n", role.Privilege, role.Resource.Stream, role.Resource.Tag)
					}
				}
				println()
			}

		} else {
			body := string(bytes)
			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		}

		return nil
	},
}

func fetchUserRoles(client *http.Client, profile *config.Profile, user string) (res FetchUserRoleRes) {
	endpoint := fmt.Sprintf("%s/%s", profile.Url, fmt.Sprintf("api/v1/user/%s/role", user))
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return
	}
	req.SetBasicAuth(profile.Username, profile.Password)
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	res.err = json.Unmarshal(body, &res.data)
	return
}
