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

	"pb/pkg/common"
	"pb/pkg/installer"

	"github.com/spf13/cobra"
)

var UnInstallOssCmd = &cobra.Command{
	Use:     "oss",
	Short:   "Uninstall Parseable OSS",
	Example: "pb uninstall oss",
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Add verbose flag
		cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")

		// Print the banner
		printBanner()

		if err := installer.Uninstaller(verbose); err != nil {
			fmt.Println(common.Red + err.Error())
		}

		return nil
	},
}
