package cmd

import (
	"fmt"
	"pb/pkg/common"
	"pb/pkg/installer"

	"github.com/spf13/cobra"
)

var UnInstallOssCmd = &cobra.Command{
	Use:     "oss",
	Short:   "Uninstall Parseable OSS",
	Example: "pb uninstall oss",
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Add verbose flag
		cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")

		// Print the banner
		printBanner()

		if err := installer.Uninstaller(verbose); err != nil {
			fmt.Println(common.Red + err.Error())
		}

		return nil
	},
}
