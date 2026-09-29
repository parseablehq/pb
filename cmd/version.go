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
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// VersionCmd is the command for printing version information
var VersionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Print version",
	Long:    "Print the client version. For server and connection details, use pb status.",
	Example: "  pb version",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if cmd.Annotations == nil {
			cmd.Annotations = make(map[string]string)
		}

		startTime := time.Now()
		defer func() {
			// Capture the execution time in annotations
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		err := PrintVersion(cmd, "1.0.0", "abc123") // Replace with actual version and commit values
		if err != nil {
			cmd.Annotations["error"] = err.Error()
		}
		return err
	},
}

func init() {
	VersionCmd.Flags().StringP("output", "o", "text", "Output format (text|json)")
}

// PrintVersion prints version information
func PrintVersion(cmd *cobra.Command, version, _ string) error {
	format, err := commandOutputFormat(cmd)
	if err != nil {
		return err
	}
	version = strings.TrimPrefix(version, "v")
	if format != outputJSON {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "pb version %s\n", version)
		return err
	}

	return writeJSON(cmd.OutOrStdout(), map[string]string{"version": version})
}
