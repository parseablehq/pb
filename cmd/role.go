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
	"pb/pkg/model/role"
	"strings"
	"sync"
	"time"

	internalHTTP "pb/pkg/http"

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
		startTime := time.Now()
		cmd.Annotations = make(map[string]string)
		defer func() {
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		name := args[0]

		var roles []string
		client := internalHTTP.DefaultClient(&DefaultProfile)
		if err := fetchRoles(&client, &roles); err != nil {
			cmd.Annotations["errors"] = fmt.Sprintf("Error fetching roles: %s", err.Error())
			return err
		}

		if strings.Contains(strings.Join(roles, " "), name) {
			fmt.Println("role already exists, please use a different name")
			return nil
		}

		_m, err := tea.NewProgram(role.New()).Run()
		if err != nil {
			cmd.Annotations["errors"] = fmt.Sprintf("Error initializing program: %s", err.Error())
			return err
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
		if privilege != "none" {
			roleData := RoleData{Privilege: privilege}
			switch privilege {
			case "writer", "ingestor":
				roleData.Resource = &RoleResource{Stream: stream}
			case "reader":
				roleData.Resource = &RoleResource{Stream: stream, Tag: tag}
			}
			roleDataJSON, _ := json.Marshal([]RoleData{roleData})
			putBody = bytes.NewBuffer(roleDataJSON)
		}

		req, err := client.NewRequest("PUT", "role/"+name, putBody)
		if err != nil {
			cmd.Annotations["errors"] = fmt.Sprintf("Error creating request: %s", err.Error())
			return err
		}

		resp, err := client.Client.Do(req)
		if err != nil {
			cmd.Annotations["errors"] = fmt.Sprintf("Error performing request: %s", err.Error())
			return err
		}
		defer resp.Body.Close()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			cmd.Annotations["errors"] = fmt.Sprintf("Error reading response: %s", err.Error())
			return err
		}
		body := string(bodyBytes)

		if resp.StatusCode == 200 {
			fmt.Printf("Added role %s", name)
		} else {
			cmd.Annotations["errors"] = fmt.Sprintf("Request failed - Status: %s, Response: %s", resp.Status, body)
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
		startTime := time.Now()
		cmd.Annotations = make(map[string]string)
		defer func() {
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		name := args[0]
		client := internalHTTP.DefaultClient(&DefaultProfile)
		req, err := client.NewRequest("DELETE", "role/"+name, nil)
		if err != nil {
			cmd.Annotations["errors"] = fmt.Sprintf("Error creating delete request: %s", err.Error())
			return err
		}

		resp, err := client.Client.Do(req)
		if err != nil {
			cmd.Annotations["errors"] = fmt.Sprintf("Error performing delete request: %s", err.Error())
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			fmt.Printf("Removed role %s\n", StyleBold.Render(name))
		} else {
			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				cmd.Annotations["errors"] = fmt.Sprintf("Error reading response: %s", err.Error())
				return err
			}
			body := string(bodyBytes)
			cmd.Annotations["errors"] = fmt.Sprintf("Request failed - Status: %s, Response: %s", resp.Status, body)
			fmt.Printf("Request Failed\nStatus Code: %s\nResponse: %s\n", resp.Status, body)
		}

		return nil
	},
}

var ListRoleCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all roles",
	Example: "  pb role list",
	RunE: func(cmd *cobra.Command, _ []string) error {
		startTime := time.Now()
		cmd.Annotations = make(map[string]string)
		defer func() {
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		var roles []string
		client := internalHTTP.DefaultClient(&DefaultProfile)
		err := fetchRoles(&client, &roles)
		if err != nil {
			cmd.Annotations["errors"] = fmt.Sprintf("Error fetching roles: %s", err.Error())
			return err
		}

		outputFormat, err := cmd.Flags().GetString("output")
		if err != nil {
			cmd.Annotations["errors"] = fmt.Sprintf("Error retrieving output flag: %s", err.Error())
			return err
		}

		roleResponses := make([]struct {
			data []RoleData
			err  error
		}, len(roles))

		var wg sync.WaitGroup
		for idx, role := range roles {
			wg.Add(1)
			go func(idx int, role string) {
				defer wg.Done()
				roleResponses[idx].data, roleResponses[idx].err = fetchSpecificRole(&client, role)
			}(idx, role)
		}
		wg.Wait()

		if outputFormat == "json" {
			allRoles := map[string][]RoleData{}
			for idx, roleName := range roles {
				if roleResponses[idx].err == nil {
					allRoles[roleName] = roleResponses[idx].data
				}
			}
			jsonOutput, err := json.MarshalIndent(allRoles, "", "  ")
			if err != nil {
				cmd.Annotations["errors"] = fmt.Sprintf("Error marshaling JSON output: %s", err.Error())
				return fmt.Errorf("failed to marshal JSON output: %w", err)
			}
			fmt.Println(string(jsonOutput))
			return nil
		}

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
				fmt.Printf("Error fetching role data for %s: %v\n", roleName, fetchRes.err)
				cmd.Annotations["errors"] += fmt.Sprintf("Error fetching role data for %s: %v\n", roleName, fetchRes.err)
			}
		}

		return nil
	},
}

func fetchRoles(client *internalHTTP.HTTPClient, data *[]string) error {
	req, err := client.NewRequest("GET", "role", nil)
	if err != nil {
		return err
	}

	resp, err := client.Client.Do(req)
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

func fetchSpecificRole(client *internalHTTP.HTTPClient, role string) (res []RoleData, err error) {
	req, err := client.NewRequest("GET", fmt.Sprintf("role/%s", role), nil)
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

func init() {
	// Add the --output flag with default value "text"
	ListRoleCmd.Flags().StringP("output", "o", "text", "Output format: 'text' or 'json'")
}
