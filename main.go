package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Root command
var cli = &cobra.Command{
	Use:   "pb",
	Short: "cli tool to connect with Parseable",
}

// Profile subcommand
var profile = &cobra.Command{
	Use: "profile",
}

func main() {
	profile.AddCommand(AddProfileCmd)
	profile.AddCommand(DeleteProfileCmd)
	profile.AddCommand(ListProfileCmd)
	profile.AddCommand(DefaultProfileCmd)

	cli.AddCommand(profile)
	cli.AddCommand(QueryProfileCmd)
	cli.CompletionOptions.HiddenDefaultCmd = true

	err := cli.Execute()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
