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
	"pb/pkg/installer"

	"github.com/spf13/cobra"
)

var verbose bool

var InstallOssCmd = &cobra.Command{
	Use:     "oss",
	Short:   "Deploy Parseable OSS",
	Example: "pb install oss",
	Run: func(cmd *cobra.Command, _ []string) {
		// Add verbose flag
		cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
		installer.Installer(verbose)
	},
}
