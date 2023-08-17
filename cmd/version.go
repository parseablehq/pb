package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var VersionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Print version",
	Long:    "Print version and commit information",
	Example: "  pb version",
}

func PrintVersion(version string, commit string) {
	fmt.Printf("✨ %s \n\n", standardStyleAlt.Render("Parseable command line tool"))
	fmt.Printf("%s %s\n", standardStyleBold.Render("Version: "), version)
	fmt.Printf("%s %s\n\n", standardStyleBold.Render("Commit:  "), commit)
}
