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

package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	pb "github.com/parseablehq/pb/cmd"
	"github.com/parseablehq/pb/pkg/analytics"
	"github.com/parseablehq/pb/pkg/ui"

	"github.com/spf13/cobra"
)

// populated at build time
var (
	Version string
	Commit  string
)

var (
	versionFlag      = "version"
	versionFlagShort = "v"
)

// Root command
var cli = &cobra.Command{
	Use:               "pb",
	Short:             "\nParseable command line interface",
	Long:              "\npb is the command line interface for Parseable",
	PersistentPreRunE: analytics.CheckAndCreateULID,
	RunE: func(command *cobra.Command, _ []string) error {
		if p, _ := command.Flags().GetBool(versionFlag); p {
			pb.PrintVersion(Version, Commit)
			return nil
		}
		return command.Help()
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if os.Getenv("PB_ANALYTICS") == "disable" {
			return
		}
		go func() {
			analytics.PostRunAnalytics(cmd, "cli", args)
		}()
	},
}

func postRunAnalytics(name string) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		if os.Getenv("PB_ANALYTICS") == "disable" {
			return
		}
		go analytics.PostRunAnalytics(cmd, name, args)
	}
}

type rootHelpCommand struct {
	name string
	desc string
}

type rootHelpGroup struct {
	title    string
	commands []rootHelpCommand
}

var rootHelpGroups = []rootHelpGroup{
	{
		title: "Core Commands (Query & Stream):",
		commands: []rootHelpCommand{
			{name: "sql", desc: "Run SQL query on a dataset"},
			{name: "promql", desc: "PromQL queries and metrics exploration"},
			{name: "tail", desc: "Stream live events from a dataset"},
		},
	},
	{
		title: "Data Management:",
		commands: []rootHelpCommand{
			{name: "dataset", desc: "Manage datasets"},
		},
	},
	{
		title: "Identity & Access:",
		commands: []rootHelpCommand{
			{name: "login", desc: "Login to Parseable"},
			{name: "logout", desc: "Logout from the current Parseable profile"},
			{name: "profile", desc: "Manage different Parseable targets"},
			{name: "user", desc: "Manage users"},
			{name: "role", desc: "Manage roles"},
		},
	},
	{
		title: "System Commands:",
		commands: []rootHelpCommand{
			{name: "status", desc: "Check connection status for the active profile"},
			{name: "version", desc: "Print version"},
			{name: "help", desc: "Help about any command"},
		},
	},
}

const parseableHelpASCIIArt = ` ____   _    ____  ____  _____    _    ____  _     _____
|  _ \ / \  |  _ \/ ___|| ____|  / \  | __ )| |   | ____|
| |_) / _ \ | |_) \___ \|  _|   / _ \ |  _ \| |   |  _|
|  __/ ___ \|  _ < ___) | |___ / ___ \| |_) | |___| |___
|_| /_/   \_\_| \_\____/|_____/_/   \_\____/|_____|_____|`

var profile = &cobra.Command{
	Use:               "profile",
	Short:             "Manage different Parseable targets",
	Long:              "\nuse profile command to configure different Parseable instances. Each profile takes a URL and credentials.",
	PersistentPreRunE: analytics.CheckAndCreateULID,
	PersistentPostRun: postRunAnalytics("profile"),
}

var schema = &cobra.Command{
	Use:   "schema",
	Short: "Generate or create schemas for JSON data or Parseable datasets",
	Long: `The "schema" command allows you to either:
  - Generate a schema automatically from a JSON file for analysis or integration.
	- Create a custom schema for Parseable datasets to structure and process your data.

Examples:
  - To generate a schema from a JSON file:
      pb schema generate --file=data.json
  - To create a schema for a dataset:
      pb schema create --dataset=my_dataset --config=data.json
`,
	PersistentPreRunE: combinedPreRun,
	PersistentPostRun: postRunAnalytics("generate"),
}

var user = &cobra.Command{
	Use:               "user",
	Short:             "Manage users",
	Long:              "\nuser command is used to manage users.",
	PersistentPreRunE: combinedPreRun,
	PersistentPostRun: postRunAnalytics("user"),
}

var role = &cobra.Command{
	Use:               "role",
	Short:             "Manage roles",
	Long:              "\nrole command is used to manage roles.",
	PersistentPreRunE: combinedPreRun,
	PersistentPostRun: postRunAnalytics("role"),
}

var dataset = &cobra.Command{
	Use:               "dataset",
	Short:             "Manage datasets",
	Long:              "\ndataset command is used to manage datasets.",
	PersistentPreRunE: combinedPreRun,
	PersistentPostRun: postRunAnalytics("dataset"),
}

var sql = &cobra.Command{
	Use:               "sql",
	Short:             "Run SQL query on a dataset",
	Long:              "\nRun SQL query on a dataset. Default output format is json. Use -i flag to open interactive table view.",
	Example:           "  pb sql run -i\n  pb sql run \"SELECT * FROM backend\" --from=1h -i",
	PersistentPreRunE: combinedPreRun,
	PersistentPostRun: postRunAnalytics("sql"),
}

var list = &cobra.Command{
	Use:               "list",
	Short:             "List parseable on kubernetes cluster",
	Long:              "\nlist command is used to list Parseable oss installations.",
	PersistentPreRunE: combinedPreRun,
	PersistentPostRun: postRunAnalytics("install"),
}

var show = &cobra.Command{
	Use:               "show",
	Short:             "Show outputs values defined when installing Parseable on kubernetes cluster",
	Long:              "\nshow command is used to get values in Parseable.",
	PersistentPreRunE: combinedPreRun,
	PersistentPostRun: postRunAnalytics("install"),
}

var uninstall = &cobra.Command{
	Use:               "uninstall",
	Short:             "Uninstall Parseable on kubernetes cluster",
	Long:              "\nuninstall command is used to uninstall Parseable oss/enterprise on k8s cluster.",
	PersistentPreRunE: combinedPreRun,
	PersistentPostRun: postRunAnalytics("uninstall"),
}

func main() {
	profile.AddCommand(pb.AddProfileCmd)
	profile.AddCommand(pb.RemoveProfileCmd)
	profile.AddCommand(pb.UpdateProfileCmd)
	profile.AddCommand(pb.ListProfileCmd)
	profile.AddCommand(pb.DefaultProfileCmd)

	user.AddCommand(pb.AddUserCmd)
	user.AddCommand(pb.RemoveUserCmd)
	user.AddCommand(pb.ListUserCmd)
	user.AddCommand(pb.SetUserRoleCmd)

	role.AddCommand(pb.AddRoleCmd)
	role.AddCommand(pb.RemoveRoleCmd)
	role.AddCommand(pb.ListRoleCmd)

	dataset.AddCommand(pb.AddDatasetCmd)
	dataset.AddCommand(pb.RemoveDatasetCmd)
	dataset.AddCommand(pb.ListDatasetCmd)
	dataset.AddCommand(pb.StatDatasetCmd)

	sql.AddCommand(pb.QueryCmd)
	sql.AddCommand(pb.SaveSQLCmd)
	sql.AddCommand(pb.SavedQueryList)

	pb.PromqlCmd.PersistentPreRunE = combinedPreRun
	pb.PromqlCmd.PersistentPostRun = postRunAnalytics("promql")

	schema.AddCommand(pb.GenerateSchemaCmd)
	schema.AddCommand(pb.CreateSchemaCmd)

	// cluster.AddCommand(pb.InstallOssCmd)
	// cluster.AddCommand(pb.ListOssCmd)
	// cluster.AddCommand(pb.ShowValuesCmd)
	// cluster.AddCommand(pb.UninstallOssCmd)

	list.AddCommand(pb.ListOssCmd)

	uninstall.AddCommand(pb.UninstallOssCmd)

	show.AddCommand(pb.ShowValuesCmd)

	cli.AddCommand(profile)
	cli.AddCommand(sql)
	cli.AddCommand(pb.PromqlCmd)
	cli.AddCommand(dataset)
	cli.AddCommand(user)
	cli.AddCommand(role)
	cli.AddCommand(pb.TailCmd)
	// cli.AddCommand(cluster)

	cli.AddCommand(pb.LoginCmd)
	cli.AddCommand(pb.LogoutCmd)
	cli.AddCommand(pb.StatusCmd)

	// Set as command
	pb.VersionCmd.Run = func(_ *cobra.Command, _ []string) {
		pb.PrintVersion(Version, Commit)
	}

	cli.AddCommand(pb.VersionCmd)
	// set as flag
	cli.Flags().BoolP(versionFlag, versionFlagShort, false, "Print version")
	cli.SetHelpFunc(renderRootHelp)

	cli.CompletionOptions.HiddenDefaultCmd = true

	err := cli.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func renderRootHelp(cmd *cobra.Command, _ []string) {
	if cmd != cli {
		renderCommandHelp(cmd)
		return
	}

	out := cmd.OutOrStdout()
	palette := ui.Active
	logo := lipgloss.NewStyle().
		Foreground(palette.Accent).
		Bold(true).
		Render(parseableHelpASCIIArt)
	tagline := lipgloss.NewStyle().
		Foreground(palette.Faint).
		Render("Parseable CLI")
	body := lipgloss.NewStyle().Foreground(palette.Body)
	section := lipgloss.NewStyle().Foreground(palette.Accent).Bold(true)
	commandStyle := lipgloss.NewStyle().Foreground(palette.Body).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(palette.Faint)

	fmt.Fprintln(out, logo)
	fmt.Fprintln(out, tagline)
	fmt.Fprintln(out)
	fmt.Fprintln(out, body.Render("pb is the command line interface for Parseable."))
	fmt.Fprintln(out, body.Render("Query logs with SQL, explore metrics with PromQL, tail live events,"))
	fmt.Fprintln(out, body.Render("and manage datasets, users, roles, and profiles from your terminal."))
	fmt.Fprintln(out)

	fmt.Fprintln(out, section.Render("Usage:"))
	fmt.Fprintln(out, "  pb [command] [flags]")
	fmt.Fprintln(out)

	fmt.Fprintln(out, section.Render("Available Commands:"))
	for _, command := range rootHelpCommands(cmd) {
		fmt.Fprintln(out, rootHelpCommandRow(command, commandStyle, descStyle))
	}
	fmt.Fprintln(out)

	if cmd.HasAvailableFlags() {
		fmt.Fprintln(out, section.Render("Flags:"))
		var flags bytes.Buffer
		cmd.Flags().SetOutput(&flags)
		cmd.Flags().PrintDefaults()
		fmt.Fprint(out, flags.String())
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, descStyle.Render(`Use "pb [command] --help" for more information about a command.`))
}

func rootHelpCommands(cmd *cobra.Command) []rootHelpCommand {
	var commands []rootHelpCommand
	for _, group := range rootHelpGroups {
		for _, command := range group.commands {
			if command.name != "help" && commandByName(cmd, command.name) == nil {
				continue
			}
			commands = append(commands, command)
		}
	}
	return commands
}

func rootHelpCommandRow(command rootHelpCommand, commandStyle, descStyle lipgloss.Style) string {
	const commandWidth = 10
	padding := commandWidth - lipgloss.Width(command.name)
	if padding < 1 {
		padding = 1
	}
	return "  " + commandStyle.Render(command.name) + strings.Repeat(" ", padding) + descStyle.Render(command.desc)
}

func commandByName(cmd *cobra.Command, name string) *cobra.Command {
	for _, child := range cmd.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}

func renderCommandHelp(cmd *cobra.Command) {
	out := cmd.OutOrStdout()
	desc := cmd.Long
	if desc == "" {
		desc = cmd.Short
	}
	if desc != "" {
		fmt.Fprintln(out, desc)
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, "Usage:")
	fmt.Fprintf(out, "  %s\n", cmd.UseLine())
	fmt.Fprintln(out)

	if cmd.Example != "" {
		fmt.Fprintln(out, "Examples:")
		fmt.Fprintln(out, cmd.Example)
		fmt.Fprintln(out)
	}

	if cmd.HasAvailableSubCommands() {
		fmt.Fprintln(out, "Available Commands:")
		for _, child := range cmd.Commands() {
			if !child.IsAvailableCommand() && child.Name() != "help" {
				continue
			}
			fmt.Fprintf(out, "  %-16s %s\n", commandDisplayName(child), child.Short)
		}
		fmt.Fprintln(out)
	}

	if cmd.HasAvailableLocalFlags() {
		fmt.Fprintln(out, "Flags:")
		var flags bytes.Buffer
		cmd.LocalFlags().SetOutput(&flags)
		cmd.LocalFlags().PrintDefaults()
		fmt.Fprint(out, flags.String())
		fmt.Fprintln(out)
	}

	if cmd.HasAvailableInheritedFlags() {
		fmt.Fprintln(out, "Global Flags:")
		var flags bytes.Buffer
		cmd.InheritedFlags().SetOutput(&flags)
		cmd.InheritedFlags().PrintDefaults()
		fmt.Fprint(out, flags.String())
		fmt.Fprintln(out)
	}

	if cmd.HasAvailableSubCommands() {
		fmt.Fprintf(out, "Use \"%s [command] --help\" for more information about a command.\n", cmd.CommandPath())
	}
}

func commandDisplayName(cmd *cobra.Command) string {
	name := cmd.Name()
	aliases := cmd.Aliases
	if len(aliases) == 0 {
		return name
	}
	return name + "|" + strings.Join(aliases, "|")
}

// Wrapper to combine existing pre-run logic and ULID check
func combinedPreRun(cmd *cobra.Command, args []string) error {
	err := pb.PreRunDefaultProfile(cmd, args)
	if err != nil {
		return fmt.Errorf("error initializing default profile: %w", err)
	}

	if err := analytics.CheckAndCreateULID(cmd, args); err != nil {
		return fmt.Errorf("error while creating ulid: %v", err)
	}

	return nil
}
