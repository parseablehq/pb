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
	"os"
	"pb/pkg/model"

	"github.com/spf13/cobra"
)

var FilterList = &cobra.Command{
	Use:     "list",
	Example: "pb query list ",
	Short:   "List of saved filter for a stream",
	Long:    "\nShow a list of saved filter for a stream ",
	PreRunE: PreRunDefaultProfile,
	Run: func(command *cobra.Command, args []string) {
		// model.FilterListUI()
		p:= model.UiApp()
		_,err := p.Run(); if err != nil {
		os.Exit(1)
		}

},
		
}




