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
	"fmt"

	"github.com/spf13/cobra"
)

// VersionCmd is the command for printing version information
var VersionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Print version",
	Long:    "Print version and commit information",
	Example: "  pb version",
}

// PrintVersion prints version information
func PrintVersion(version, commit string) {
	client := DefaultClient()

	fmt.Printf("\n%s \n", StandardStyleAlt.Render("pb version"))
	fmt.Printf("- %s %s\n", StandardStyleBold.Render("version: "), version)
	fmt.Printf("- %s %s\n\n", StandardStyleBold.Render("commit:  "), commit)

	if err := PreRun(); err != nil {
		return
	}
	about, err := FetchAbout(&client)
	if err != nil {
		return
	}

	fmt.Printf("%s %s \n", StandardStyleAlt.Render("Connected to"), StandardStyleBold.Render(DefaultProfile.URL))
	fmt.Printf("- %s %s\n", StandardStyleBold.Render("version: "), about.Version)
	fmt.Printf("- %s %s\n\n", StandardStyleBold.Render("commit:  "), about.Commit)
}
