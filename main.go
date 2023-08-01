package main

import (
	"cli/config"
	"errors"
	"os"

	"github.com/spf13/cobra"
)

var DefaultProfile config.Profile

// Check if a profile exists.
// This is required by mostly all commands except profile
func PreRunDefaultProfile(cmd *cobra.Command, args []string) error {
	conf, err := config.ReadConfigFromFile()
	if err != nil {
		return err
	}
	if conf.Profiles == nil || conf.Default_profile == "" {
		return errors.New("no profile is configured to run this command. please create one using profile command")
	}

	DefaultProfile = conf.Profiles[conf.Default_profile]
	return nil
}

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
	PersistentPreRunE: PreRunDefaultProfile,
}

var stream = &cobra.Command{
	Use:               "stream",
	Short:             "Manage stream",
	PersistentPreRunE: PreRunDefaultProfile,
}

func main() {
	profile.AddCommand(AddProfileCmd)
	profile.AddCommand(DeleteProfileCmd)
	profile.AddCommand(ListProfileCmd)
	profile.AddCommand(DefaultProfileCmd)

	user.AddCommand(AddUserCmd)
	user.AddCommand(DeleteUserCmd)
	user.AddCommand(ListUserCmd)

	stream.AddCommand(CreateStreamCmd)
	stream.AddCommand(ListStreamCmd)
	stream.AddCommand(DeleteStreamCmd)
	stream.AddCommand(StatStreamCmd)

	cli.AddCommand(profile)
	cli.AddCommand(QueryProfileCmd)
	cli.AddCommand(stream)
	cli.AddCommand(user)

	cli.CompletionOptions.HiddenDefaultCmd = true

	err := cli.Execute()
	if err != nil {
		os.Exit(1)
	}
}
