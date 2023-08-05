// Copyright (c) 2023 Cloudnatively Services Pvt Ltd
//
// This file is part of MinIO Object Storage stack
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
	"pb/pkg/model"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var QueryProfileCmd = &cobra.Command{
	Use:     "query name minutes",
	Example: "query local_logs 20",
	Short:   "Open Query TUI",
	Args:    cobra.ExactArgs(2),
	PreRunE: PreRunDefaultProfile,
	RunE: func(cmd *cobra.Command, args []string) error {
		stream := args[0]
		duration, err := strconv.Atoi(args[1])
		if err != nil {
			return err
		}
		p := tea.NewProgram(model.NewQueryModel(DefaultProfile, stream, uint(duration)), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Printf("Alas, there's been an error: %v", err)
			os.Exit(1)
		}
		return nil
	},
}
