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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"pb/pkg/model/role"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"golang.org/x/exp/slices"
)

type UserData struct {
	ID     string `json:"id"`
	Method string `json:"method"`
}

type UserRoleData map[string][]RoleData

var AddUserCmd = &cobra.Command{
	Use:     "add user-name",
	Example: "  pb user add bob",
	Short:   "Add a new user",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		client := DefaultClient()
		users, err := fetchUsers(&client)
		if err != nil {
			return err
		}

		if slices.ContainsFunc(users, func(user UserData) bool {
			return user.ID == name
		}) {
			fmt.Println("user already exists")
			return nil
		}

		_m, err := tea.NewProgram(role.New()).Run()
		if err != nil {
			fmt.Printf("Alas, there's been an error: %v", err)
			os.Exit(1)
		}
		m := _m.(role.Model)

		roles := m.Selection.Value()

		if !m.Success {
			fmt.Println("aborted by user")
			return nil
		}

		var putBody io.Reader
		roleDataJSON, _ := json.Marshal([]string{roles})
		putBody = bytes.NewBuffer(roleDataJSON)

		req, err := client.NewRequest("PUT", "user/"+name, putBody)
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

var RemoveUserCmd = &cobra.Command{
	Use:     "remove user-name",
	Aliases: []string{"rm"},
	Example: "  pb user remove bob",
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
			fmt.Printf("Removed user %s\n", styleBold.Render(name))
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
	Use:     "list",
	Short:   "List all users",
	Example: "  pb user list",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := DefaultClient()
		users, err := fetchUsers(&client)
		if err != nil {
			return err
		}

		roleResponses := make([]struct {
			data UserRoleData
			err  error
		}, len(users))

		wsg := sync.WaitGroup{}

		for idx, user := range users {
			wsg.Add(1)
			out := &roleResponses[idx]
			user := user.ID
			client := &client
			go func() {
				out.data, out.err = fetchUserRoles(client, user)
				wsg.Done()
			}()
		}

		wsg.Wait()
		fmt.Println()
		for idx, user := range users {
			roles := roleResponses[idx]
			fmt.Print("• ")
			fmt.Println(standardStyleBold.Bold(true).Render(user.ID))
			if roles.err == nil {
				for role := range roles.data {
					fmt.Println(lipgloss.NewStyle().PaddingLeft(3).Render(role))
				}
			} else {
				fmt.Println(roles.err)
			}
		}

		return nil
	},
}

func fetchUsers(client *HTTPClient) (res []UserData, err error) {
	req, err := client.NewRequest("GET", "user", nil)
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
		err = json.Unmarshal(bytes, &res)
		if err != nil {
			return
		}
	} else {
		body := string(bytes)
		err = fmt.Errorf("request failed\nstatus code: %s\nresponse: %s", resp.Status, body)
		return
	}

	return
}

func fetchUserRoles(client *HTTPClient, user string) (res UserRoleData, err error) {
	req, err := client.NewRequest("GET", fmt.Sprintf("user/%s/role", user), nil)
	if err != nil {
		return
	}
	resp, err := client.client.Do(req)
	if err != nil {
		return
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	err = json.Unmarshal(body, &res)
	return
}
