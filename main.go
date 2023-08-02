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

package main

import (
	"os"
	"pb/cmd"

	"github.com/spf13/cobra"
)

// Root command
var cli = &cobra.Command{
	Use:   "pb",
	Short: "cli tool to connect with Parseable",
}

// Profile subcommand
var profile = &cobra.Command{
	Use:   "profile",
	Short: "Manage profiles",
}

var user = &cobra.Command{
	Use:               "user",
	Short:             "Manage users",
	PersistentPreRunE: cmd.PreRunDefaultProfile,
}

var stream = &cobra.Command{
	Use:               "stream",
	Short:             "Manage stream",
	PersistentPreRunE: cmd.PreRunDefaultProfile,
}

func main() {
	profile.AddCommand(cmd.AddProfileCmd)
	profile.AddCommand(cmd.DeleteProfileCmd)
	profile.AddCommand(cmd.ListProfileCmd)
	profile.AddCommand(cmd.DefaultProfileCmd)

	user.AddCommand(cmd.AddUserCmd)
	user.AddCommand(cmd.DeleteUserCmd)
	user.AddCommand(cmd.ListUserCmd)

	stream.AddCommand(cmd.CreateStreamCmd)
	stream.AddCommand(cmd.ListStreamCmd)
	stream.AddCommand(cmd.DeleteStreamCmd)
	stream.AddCommand(cmd.StatStreamCmd)

	cli.AddCommand(profile)
	cli.AddCommand(cmd.QueryProfileCmd)
	cli.AddCommand(stream)
	cli.AddCommand(user)

	cli.CompletionOptions.HiddenDefaultCmd = true

	err := cli.Execute()
	if err != nil {
		os.Exit(1)
	}
}
