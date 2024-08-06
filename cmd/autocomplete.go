// Copyright (c) 2024 Parseable, Inc
//
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
	"os"

	"github.com/spf13/cobra"
)

// AutocompleteCmd represents the autocomplete command
var AutocompleteCmd = &cobra.Command{
	Use:   "autocomplete [bash|zsh|powershell]",
	Short: "Generate autocomplete script",
	Long:  `Generate autocomplete script for bash, zsh, or powershell`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		switch args[0] {
		case "bash":
			err = cmd.Root().GenBashCompletion(os.Stdout)
		case "zsh":
			err = cmd.Root().GenZshCompletion(os.Stdout)
		case "powershell":
			err = cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		default:
			err = fmt.Errorf("unsupported shell type: %s. Only bash, zsh, and powershell are supported", args[0])
		}

		if err != nil {
			return fmt.Errorf("error generating autocomplete script: %w", err)
		}

		return nil
	},
}
