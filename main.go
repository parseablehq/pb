// Copyright (c) 2023 Cloudnatively Services Pvt Ltd
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

package main

import (
	"fmt"
	"os"
	"strconv"

	"pb/cmd"
	"pb/pkg/config"
	"pb/pkg/model"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	// populated at build time
	PBVersion string
	PBCommit  string
)

var (
	durationFlag      = "duration"
	durationFlagShort = "d"
	versionFlag       = "version"
	versionFlagShort  = "v"
	defaultDuration   = "10"
)

func DefaultInitialProfile() config.Profile {
	return config.Profile{
		URL:      "https://demo.parseable.io",
		Username: "admin",
		Password: "admin",
	}
}

// Root command
var cli = &cobra.Command{
	Use:   "pb",
	Short: "\nParseable command line tool",
	Long:  "\npb is a command line tool for Parseable",
	Run: func(command *cobra.Command, args []string) {
		if p, _ := command.Flags().GetBool(versionFlag); p {
			cmd.PrintVersion(PBVersion, PBCommit)
		}
	},
}

var profile = &cobra.Command{
	Use:   "profile",
	Short: "Manage profiles",
	Long:  "\nprofile command is used to manage multiple instances of Parseable. Each profile can have a different set of credentials and URL",
}

var user = &cobra.Command{
	Use:               "user",
	Short:             "Manage users",
	Long:              "\nuser command is used to manage users. Users can be added, deleted and listed",
	PersistentPreRunE: cmd.PreRunDefaultProfile,
}

var stream = &cobra.Command{
	Use:               "stream",
	Short:             "Manage streams",
	Long:              "\nstream command is used to manage streams. Streams can be created, deleted and listed",
	PersistentPreRunE: cmd.PreRunDefaultProfile,
}

var query = &cobra.Command{
	Use:     "query [stream-name] --duration 10",
	Example: "  pb query frontend --duration 10",
	Short:   "Open SQL query prompt",
	Long:    "\nquery command is used to open a prompt to query a stream. The stream name and duration in minutes are required arguments",
	Args:    cobra.ExactArgs(1),
	PreRunE: cmd.PreRunDefaultProfile,
	RunE: func(command *cobra.Command, args []string) error {
		stream := args[0]
		duration, _ := command.Flags().GetString(durationFlag)
		if duration == "" {
			duration = defaultDuration
		}
		durationInt, err := strconv.Atoi(duration)
		if err != nil {
			return err
		}
		p := tea.NewProgram(model.NewQueryModel(cmd.DefaultProfile, stream, uint(durationInt)), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Printf("there's been an error: %v", err)
			os.Exit(1)
		}
		return nil
	},
}

func main() {
	profile.AddCommand(cmd.AddProfileCmd)
	profile.AddCommand(cmd.RemoveProfileCmd)
	profile.AddCommand(cmd.ListProfileCmd)
	profile.AddCommand(cmd.DefaultProfileCmd)

	user.AddCommand(cmd.AddUserCmd)
	user.AddCommand(cmd.RemoveUserCmd)
	user.AddCommand(cmd.ListUserCmd)

	stream.AddCommand(cmd.AddStreamCmd)
	stream.AddCommand(cmd.RemoveStreamCmd)
	stream.AddCommand(cmd.ListStreamCmd)
	stream.AddCommand(cmd.StatStreamCmd)

	query.PersistentFlags().StringP(durationFlag, durationFlagShort, defaultDuration, "specify the duration in minutes for which queries should be executed. Defaults to 10")

	cli.AddCommand(profile)
	cli.AddCommand(query)
	cli.AddCommand(stream)
	cli.AddCommand(user)

	// Set as command
	cmd.VersionCmd.Run = func(_ *cobra.Command, args []string) {
		cmd.PrintVersion(PBVersion, PBCommit)
	}
	cli.AddCommand(cmd.VersionCmd)
	// set as flag
	cli.Flags().BoolP(versionFlag, versionFlagShort, false, "Print version")

	cli.CompletionOptions.HiddenDefaultCmd = true

	// create a default profile if file does not exist
	if _, err := config.ReadConfigFromFile(); os.IsNotExist(err) {
		conf := config.Config{
			Profiles:       map[string]config.Profile{"demo": DefaultInitialProfile()},
			DefaultProfile: "demo",
		}
		config.WriteConfigToFile(&conf)
	}

	err := cli.Execute()
	if err != nil {
		os.Exit(1)
	}
}
