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
