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
	"os"
	"strings"
	"sync"

	"pb/pkg/model/role"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

type RoleResource struct {
	Stream string `json:"stream,omitempty"`
	Tag    string `json:"tag,omitempty"`
}

type RoleData struct {
	Privilege string        `json:"privilege"`
	Resource  *RoleResource `json:"resource,omitempty"`
}

func (user *RoleData) Render() string {
	var s strings.Builder
	s.WriteString(StandardStyle.Render("Privilege: "))
	s.WriteString(StandardStyleAlt.Render(user.Privilege))
	s.WriteString("\n")
	if user.Resource != nil {
		if user.Resource.Stream != "" {
			s.WriteString(StandardStyle.Render("Stream:    "))
			s.WriteString(StandardStyleAlt.Render(user.Resource.Stream))
			s.WriteString("\n")
		}
		if user.Resource.Tag != "" {
			s.WriteString(StandardStyle.Render("Tag:       "))
			s.WriteString(StandardStyleAlt.Render(user.Resource.Tag))
			s.WriteString("\n")
		}
	}

	return s.String()
}

var AddRoleCmd = &cobra.Command{
	Use:     "add role-name",
	Example: "  pb role add ingestors",
	Short:   "Add a new role",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		// check if the role already exists
		var roles []string
		client := DefaultClient()
		if err := fetchRoles(&client, &roles); err != nil {
			return err
		}
		if strings.Contains(strings.Join(roles, " "), name) {
			fmt.Println("role already exists, please use a different name")
			return nil
		}

		_m, err := tea.NewProgram(role.New()).Run()
		if err != nil {
			fmt.Printf("there's been an error: %v", err)
			os.Exit(1)
		}
		m := _m.(role.Model)

		privilege := m.Selection.Value()
		stream := m.Stream.Value()
		tag := m.Tag.Value()

		if !m.Success {
			fmt.Println("aborted by user")
			return nil
		}

		var putBody io.Reader

		// set role
		if privilege != "none" {
			roleData := RoleData{
				Privilege: privilege,
			}
			switch privilege {
			case "writer":
				roleData.Resource = &RoleResource{
					Stream: stream,
				}
			case "reader":
				roleData.Resource = &RoleResource{
					Stream: stream,
				}
				if tag != "" {
					roleData.Resource.Tag = tag
				}
			case "ingestor":
				roleData.Resource = &RoleResource{
					Stream: stream,
				}
			}
			roleDataJSON, _ := json.Marshal([]RoleData{roleData})
			putBody = bytes.NewBuffer(roleDataJSON)
		}

		req, err := client.NewRequest("PUT", "role/"+name, putBody)
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
			fmt.Printf("Added role %s", name)
		} else {
			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		}

		return nil
	},
}

var RemoveRoleCmd = &cobra.Command{
	Use:     "remove role-name",
	Aliases: []string{"rm"},
	Example: "  pb role remove ingestor",
	Short:   "Delete a role",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		client := DefaultClient()
		req, err := client.NewRequest("DELETE", "role/"+name, nil)
		if err != nil {
			return err
		}

		resp, err := client.client.Do(req)
		if err != nil {
			return err
		}

		if resp.StatusCode == 200 {
			fmt.Printf("Removed role %s\n", StyleBold.Render(name))
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

var ListRoleCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all roles",
	Example: "  pb role list",
	RunE: func(cmd *cobra.Command, args []string) error {
		var roles []string
		client := DefaultClient()
		err := fetchRoles(&client, &roles)
		if err != nil {
			return err
		}

		roleResponses := make([]struct {
			data []RoleData
			err  error
		}, len(roles))

		wsg := sync.WaitGroup{}
		for idx, role := range roles {
			wsg.Add(1)
			out := &roleResponses[idx]
			role := role
			client := &client
			go func() {
				out.data, out.err = fetchSpecificRole(client, role)
				wsg.Done()
			}()
		}

		wsg.Wait()
		fmt.Println()
		for idx, roleName := range roles {
			fetchRes := roleResponses[idx]
			fmt.Print("• ")
			fmt.Println(StandardStyleBold.Bold(true).Render(roleName))
			if fetchRes.err == nil {
				for _, role := range fetchRes.data {
					fmt.Println(lipgloss.NewStyle().PaddingLeft(3).Render(role.Render()))
				}
			} else {
				fmt.Println(fetchRes.err)
			}
		}

		return nil
	},
}

func fetchRoles(client *HTTPClient, data *[]string) error {
	req, err := client.NewRequest("GET", "role", nil)
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
		err = json.Unmarshal(bytes, data)
		if err != nil {
			return err
		}
	} else {
		body := string(bytes)
		return fmt.Errorf("request failed\nstatus code: %s\nresponse: %s", resp.Status, body)
	}

	return nil
}

func fetchSpecificRole(client *HTTPClient, role string) (res []RoleData, err error) {
	req, err := client.NewRequest("GET", fmt.Sprintf("role/%s", role), nil)
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
