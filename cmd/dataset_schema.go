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
	"net/http"

	internalHTTP "github.com/parseablehq/pb/pkg/http"
	"github.com/spf13/cobra"
)

// SchemaDatasetCmd prints the fields and types in a dataset's current schema.
var SchemaDatasetCmd = &cobra.Command{
	Use:     "schema <dataset>",
	Short:   "Show dataset field names and types",
	Example: "  pb dataset schema backend_logs\n  pb dataset schema backend_logs -o json",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := commandOutputFormat(cmd)
		if err != nil {
			return err
		}

		client := internalHTTP.DefaultClient(&DefaultProfile)
		req, err := client.NewRequest(http.MethodGet, "logstream/"+args[0]+"/schema", nil)
		if err != nil {
			return err
		}
		resp, err := client.Client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return responseStatusError("get dataset schema", resp.StatusCode, resp.Status, body)
		}

		var schema struct {
			Fields []struct {
				Name     string          `json:"name"`
				DataType json.RawMessage `json:"data_type"`
			} `json:"fields"`
		}
		if err := json.Unmarshal(body, &schema); err != nil {
			return newInvalidResponseError(fmt.Errorf("invalid dataset schema: %w", err))
		}
		if schema.Fields == nil {
			return newInvalidResponseError(fmt.Errorf("dataset schema has no fields array"))
		}

		if format == outputJSON {
			return writeRawJSON(cmd.OutOrStdout(), body)
		}

		out := cmd.OutOrStdout()
		for _, field := range schema.Fields {
			var typeName string
			if err := json.Unmarshal(field.DataType, &typeName); err != nil {
				var compact bytes.Buffer
				if err := json.Compact(&compact, field.DataType); err != nil {
					return newInvalidResponseError(fmt.Errorf("invalid type for field %q: %w", field.Name, err))
				}
				typeName = compact.String()
			}
			if _, err := fmt.Fprintln(out, (&DatasetListItem{Name: field.Name, Type: typeName}).Render()); err != nil {
				return err
			}
		}
		return nil
	},
}

func init() {
	SchemaDatasetCmd.Flags().StringP("output", "o", "text", "Output format (text|json)")
}
