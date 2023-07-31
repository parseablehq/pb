package main

import (
	"cli/config"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var global_profile config.Profile

var cli = &cobra.Command{
	Use:   "pb",
	Short: "pb",
	Long:  "pb",
}

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

	config, e := config.ReadConfigFromFile("config.toml")
	if e == nil {
		profile := config.Profiles[config.Default_profile]
		global_profile = profile
	}
	err := cli.Execute()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
