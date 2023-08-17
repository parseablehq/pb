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
	fmt.Printf("\n%s \n\n", standardStyleAlt.Render("pb version"))
	fmt.Printf("%s %s\n", standardStyleBold.Render("version: "), version)
	fmt.Printf("%s %s\n", standardStyleBold.Render("commit:  "), commit)
}
