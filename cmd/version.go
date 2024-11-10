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
	"encoding/json"
	"fmt"
	"pb/pkg/analytics"
	internalHTTP "pb/pkg/http"
	"time"

	"github.com/spf13/cobra"
)

// VersionCmd is the command for printing version information
var VersionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Print version",
	Long:    "Print version and commit information",
	Example: "  pb version",
	Run: func(cmd *cobra.Command, args []string) {
		if cmd.Annotations == nil {
			cmd.Annotations = make(map[string]string)
		}

		startTime := time.Now()
		defer func() {
			// Capture the execution time in annotations
			cmd.Annotations["executionTime"] = time.Since(startTime).String()
		}()

		err := PrintVersion("1.0.0", "abc123") // Replace with actual version and commit values
		if err != nil {
			cmd.Annotations["error"] = err.Error()
		}
	},
}

func init() {
	VersionCmd.Flags().StringVarP(&outputFormat, "output", "o", "text", "Output format (text|json)")
}

// PrintVersion prints version information
func PrintVersion(version, commit string) error {
	client := internalHTTP.DefaultClient(&DefaultProfile)

	// Fetch server information
	if err := PreRun(); err != nil {
		return fmt.Errorf("error in PreRun: %w", err)
	}

	about, err := analytics.FetchAbout(&client)
	if err != nil {
		return fmt.Errorf("error fetching server information: %w", err)
	}

	// Output as JSON if specified
	if outputFormat == "json" {
		versionInfo := map[string]interface{}{
			"client": map[string]string{
				"version": version,
				"commit":  commit,
			},
			"server": map[string]string{
				"url":     DefaultProfile.URL,
				"version": about.Version,
				"commit":  about.Commit,
			},
		}
		jsonData, err := json.MarshalIndent(versionInfo, "", "  ")
		if err != nil {
			return fmt.Errorf("error generating JSON output: %w", err)
		}
		fmt.Println(string(jsonData))
		return nil
	}

	// Default: Output as text
	fmt.Printf("\n%s \n", StandardStyleAlt.Render("pb version"))
	fmt.Printf("- %s %s\n", StandardStyleBold.Render("version: "), version)
	fmt.Printf("- %s %s\n\n", StandardStyleBold.Render("commit:  "), commit)

	fmt.Printf("%s %s \n", StandardStyleAlt.Render("Connected to"), StandardStyleBold.Render(DefaultProfile.URL))
	fmt.Printf("- %s %s\n", StandardStyleBold.Render("version: "), about.Version)
	fmt.Printf("- %s %s\n\n", StandardStyleBold.Render("commit:  "), about.Commit)

	return nil
}
