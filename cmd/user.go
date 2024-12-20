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
	internalHTTP "pb/pkg/http"
	"strings"
	"sync"
	"time"

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
		startTime := time.Now()
		cmd.Annotations = make(map[string]string) // Initialize Annotations map
		defer func() {
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		name := args[0]

		client := internalHTTP.DefaultClient(&DefaultProfile)
		users, err := fetchUsers(&client)
		if err != nil {
			cmd.Annotations["error"] = err.Error()
			return err
		}

		if slices.ContainsFunc(users, func(user UserData) bool {
			return user.ID == name
		}) {
			fmt.Println("user already exists")
			cmd.Annotations["error"] = "user already exists"
			return nil
		}

		// fetch all the roles to be applied to this user
		rolesToSet := cmd.Flag(roleFlag).Value.String()
		rolesToSetArr := strings.Split(rolesToSet, ",")

		// fetch the role names on the server
		var rolesOnServer []string
		if err := fetchRoles(&client, &rolesOnServer); err != nil {
			cmd.Annotations["error"] = err.Error()
			return err
		}
		rolesOnServerArr := strings.Join(rolesOnServer, " ")

		// validate if roles to be applied are actually present on the server
		for idx, role := range rolesToSetArr {
			rolesToSetArr[idx] = strings.TrimSpace(role)
			if !strings.Contains(rolesOnServerArr, rolesToSetArr[idx]) {
				fmt.Printf("role %s doesn't exist, please create a role using pb role add %s\n", rolesToSetArr[idx], rolesToSetArr[idx])
				cmd.Annotations["error"] = fmt.Sprintf("role %s doesn't exist", rolesToSetArr[idx])
				return nil
			}
		}

		var putBody io.Reader
		putBodyJSON, _ := json.Marshal(rolesToSetArr)
		putBody = bytes.NewBuffer([]byte(putBodyJSON))
		req, err := client.NewRequest("POST", "user/"+name, putBody)
		if err != nil {
			cmd.Annotations["error"] = err.Error()
			return err
		}

		resp, err := client.Client.Do(req)
		if err != nil {
			cmd.Annotations["error"] = err.Error()
			return err
		}

		bytes, err := io.ReadAll(resp.Body)
		if err != nil {
			cmd.Annotations["error"] = err.Error()
			return err
		}
		body := string(bytes)
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			fmt.Printf("Added user: %s \nPassword is: %s\nRole(s) assigned: %s\n", name, body, rolesToSet)
			cmd.Annotations["error"] = "none"
		} else {
			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
			cmd.Annotations["error"] = fmt.Sprintf("request failed with status code %s", resp.Status)
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
	RunE: func(cmd *cobra.Command, args []string) error {
		startTime := time.Now()
		cmd.Annotations = make(map[string]string)
		defer func() {
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		name := args[0]
		client := internalHTTP.DefaultClient(&DefaultProfile)
		req, err := client.NewRequest("DELETE", "user/"+name, nil)
		if err != nil {
			cmd.Annotations["error"] = err.Error()
			return err
		}

		resp, err := client.Client.Do(req)
		if err != nil {
			cmd.Annotations["error"] = err.Error()
			return err
		}

		if resp.StatusCode == 200 {
			fmt.Printf("Removed user %s\n", StyleBold.Render(name))
			cmd.Annotations["error"] = "none"
		} else {
			body, _ := io.ReadAll(resp.Body)
			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, string(body))
			cmd.Annotations["error"] = fmt.Sprintf("request failed with status code %s", resp.Status)
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
	RunE: func(cmd *cobra.Command, args []string) error {
		startTime := time.Now()
		cmd.Annotations = make(map[string]string)
		defer func() {
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		name := args[0]
		client := internalHTTP.DefaultClient(&DefaultProfile)
		users, err := fetchUsers(&client)
		if err != nil {
			cmd.Annotations["error"] = err.Error()
			return err
		}

		if !slices.ContainsFunc(users, func(user UserData) bool {
			return user.ID == name
		}) {
			fmt.Printf("user doesn't exist. Please create the user with `pb user add %s`\n", name)
			cmd.Annotations["error"] = "user does not exist"
			return nil
		}

		rolesToSet := args[1]
		rolesToSetArr := strings.Split(rolesToSet, ",")
		var rolesOnServer []string
		if err := fetchRoles(&client, &rolesOnServer); err != nil {
			cmd.Annotations["error"] = err.Error()
			return err
		}
		rolesOnServerArr := strings.Join(rolesOnServer, " ")

		for idx, role := range rolesToSetArr {
			rolesToSetArr[idx] = strings.TrimSpace(role)
			if !strings.Contains(rolesOnServerArr, rolesToSetArr[idx]) {
				fmt.Printf("role %s doesn't exist, please create a role using `pb role add %s`\n", rolesToSetArr[idx], rolesToSetArr[idx])
				cmd.Annotations["error"] = fmt.Sprintf("role %s doesn't exist", rolesToSetArr[idx])
				return nil
			}
		}

		var putBody io.Reader
		putBodyJSON, _ := json.Marshal(rolesToSetArr)
		putBody = bytes.NewBuffer([]byte(putBodyJSON))
		req, err := client.NewRequest("PUT", "user/"+name+"/role", putBody)
		if err != nil {
			cmd.Annotations["error"] = err.Error()
			return err
		}

		resp, err := client.Client.Do(req)
		if err != nil {
			cmd.Annotations["error"] = err.Error()
			return err
		}

		bytes, err := io.ReadAll(resp.Body)
		if err != nil {
			cmd.Annotations["error"] = err.Error()
			return err
		}
		body := string(bytes)
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			fmt.Printf("Added role(s) %s to user %s\n", rolesToSet, name)
			cmd.Annotations["error"] = "none"
		} else {
			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
			cmd.Annotations["error"] = fmt.Sprintf("request failed with status code %s", resp.Status)
		}

		return nil
	},
}

var ListUserCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all users",
	Example: "  pb user list",
	RunE: func(cmd *cobra.Command, _ []string) error {
		startTime := time.Now()
		cmd.Annotations = make(map[string]string)
		defer func() {
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		client := internalHTTP.DefaultClient(&DefaultProfile)
		users, err := fetchUsers(&client)
		if err != nil {
			cmd.Annotations["error"] = err.Error()
			return err
		}

		roleResponses := make([]struct {
			data []string
			err  error
		}, len(users))

		wsg := sync.WaitGroup{}
		for idx, user := range users {
			wsg.Add(1)
			out := &roleResponses[idx]
			userID := user.ID
			client := &client
			go func() {
				var userRolesData UserRoleData
				userRolesData, out.err = fetchUserRoles(client, userID)
				if out.err == nil {
					for role := range userRolesData {
						out.data = append(out.data, role)
					}
				}
				wsg.Done()
			}()
		}

		wsg.Wait()

		outputFormat, err := cmd.Flags().GetString("output")
		if err != nil {
			cmd.Annotations["error"] = err.Error()
			return err
		}

		if outputFormat == "json" {
			usersWithRoles := make([]map[string]interface{}, len(users))
			for idx, user := range users {
				usersWithRoles[idx] = map[string]interface{}{
					"id":    user.ID,
					"roles": roleResponses[idx].data,
				}
			}
			jsonOutput, err := json.MarshalIndent(usersWithRoles, "", "  ")
			if err != nil {
				cmd.Annotations["error"] = err.Error()
				return fmt.Errorf("failed to marshal JSON output: %w", err)
			}
			fmt.Println(string(jsonOutput))
			cmd.Annotations["error"] = "none"
			return nil
		}

		if outputFormat == "text" {
			fmt.Println()
			for idx, user := range users {
				roles := roleResponses[idx]
				if roles.err == nil {
					roleList := strings.Join(roles.data, ", ")
					fmt.Printf("%s, %s\n", user.ID, roleList)
				} else {
					fmt.Printf("%s, error: %v\n", user.ID, roles.err)
				}
			}
			fmt.Println()
			cmd.Annotations["error"] = "none"
			return nil
		}

		fmt.Println()
		for idx, user := range users {
			roles := roleResponses[idx]
			fmt.Print("• ")
			fmt.Println(StandardStyleBold.Bold(true).Render(user.ID))
			if roles.err == nil {
				for _, role := range roles.data {
					fmt.Println(lipgloss.NewStyle().PaddingLeft(3).Render(role))
				}
			} else {
				fmt.Println(roles.err)
			}
		}
		fmt.Println()

		cmd.Annotations["error"] = "none"
		return nil
	},
}

func fetchUsers(client *internalHTTP.HTTPClient) (res []UserData, err error) {
	req, err := client.NewRequest("GET", "user", nil)
	if err != nil {
		return
	}

	resp, err := client.Client.Do(req)
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

func fetchUserRoles(client *internalHTTP.HTTPClient, user string) (res UserRoleData, err error) {
	req, err := client.NewRequest("GET", fmt.Sprintf("user/%s/role", user), nil)
	if err != nil {
		return
	}
	resp, err := client.Client.Do(req)
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

func init() {
	// Add the --output flag with shorthand -o, defaulting to empty for default layout
	ListUserCmd.Flags().StringP("output", "o", "", "Output format: 'text' or 'json'")
}
