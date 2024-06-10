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
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"golang.org/x/exp/slices"
)

type UserData struct {
	ID     string `json:"id"`
	Method string `json:"method"`
}

type UserRoleData map[string][]RoleData

var (
	roleFlag      = "role"
	roleFlagShort = "r"
)

var addUser = &cobra.Command{
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

		// fetch all the roles to be applied to this user
		rolesToSet := cmd.Flag(roleFlag).Value.String()
		rolesToSetArr := strings.Split(rolesToSet, ",")

		// fetch the role names on the server
		var rolesOnServer []string
		if err := fetchRoles(&client, &rolesOnServer); err != nil {
			return err
		}
		rolesOnServerArr := strings.Join(rolesOnServer, " ")

		// validate if roles to be applied are actually present on the server
		for idx, role := range rolesToSetArr {
			rolesToSetArr[idx] = strings.TrimSpace(role)
			if !strings.Contains(rolesOnServerArr, rolesToSetArr[idx]) {
				fmt.Printf("role %s doesn't exist, please create a role using `pb role add %s`\n", rolesToSetArr[idx], rolesToSetArr[idx])
				return nil
			}
		}

		var putBody io.Reader
		putBodyJSON, _ := json.Marshal(rolesToSetArr)
		putBody = bytes.NewBuffer([]byte(putBodyJSON))
		req, err := client.NewRequest("POST", "user/"+name, putBody)
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
			fmt.Printf("Added user: %s \nPassword is: %s\nRole(s) assigned: %s\n", name, body, rolesToSet)
		} else {
			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		}

		return nil
	},
}

var AddUserCmd = func() *cobra.Command {
	addUser.Flags().StringP(roleFlag, roleFlagShort, "", "specify the role(s) to be assigned to the user. Use comma separated values for multiple roles. Example: --role admin,developer")
	return addUser
}()

var RemoveUserCmd = &cobra.Command{
	Use:     "remove user-name",
	Aliases: []string{"rm"},
	Example: "  pb user remove bob",
	Short:   "Delete a user",
	Args:    cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
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
			fmt.Printf("Removed user %s\n", StyleBold.Render(name))
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

var SetUserRoleCmd = &cobra.Command{
	Use:     "set-role user-name roles",
	Short:   "Set roles for a user",
	Example: "  pb user set-role bob admin,developer",
	PreRunE: func(_ *cobra.Command, args []string) error {
		if len(args) < 2 {
			return fmt.Errorf("requires at least 2 arguments")
		}
		return nil
	},
	RunE: func(_ *cobra.Command, args []string) error {
		name := args[0]

		client := DefaultClient()
		users, err := fetchUsers(&client)
		if err != nil {
			return err
		}

		if !slices.ContainsFunc(users, func(user UserData) bool {
			return user.ID == name
		}) {
			fmt.Printf("user doesn't exist. Please create the user with `pb user add %s`\n", name)
			return nil
		}

		// fetch all the roles to be applied to this user
		rolesToSet := args[1]
		rolesToSetArr := strings.Split(rolesToSet, ",")

		// fetch the role names on the server
		var rolesOnServer []string
		if err := fetchRoles(&client, &rolesOnServer); err != nil {
			return err
		}
		rolesOnServerArr := strings.Join(rolesOnServer, " ")

		// validate if roles to be applied are actually present on the server
		for idx, role := range rolesToSetArr {
			rolesToSetArr[idx] = strings.TrimSpace(role)
			if !strings.Contains(rolesOnServerArr, rolesToSetArr[idx]) {
				fmt.Printf("role %s doesn't exist, please create a role using `pb role add %s`\n", rolesToSetArr[idx], rolesToSetArr[idx])
				return nil
			}
		}

		var putBody io.Reader
		putBodyJSON, _ := json.Marshal(rolesToSetArr)
		putBody = bytes.NewBuffer([]byte(putBodyJSON))
		req, err := client.NewRequest("PUT", "user/"+name+"/role", putBody)
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
			fmt.Printf("Added role(s) %s to user %s\n", rolesToSet, name)
		} else {
			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		}

		return nil
	},
}

var ListUserCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all users",
	Example: "  pb user list",
	RunE: func(_ *cobra.Command, _ []string) error {
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
			fmt.Println(StandardStyleBold.Bold(true).Render(user.ID))
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
