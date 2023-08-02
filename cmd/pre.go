package cmd

import (
	"errors"
	"os"
	"pb/pkg/config"

	"github.com/spf13/cobra"
)

var DefaultProfile config.Profile

// Check if a profile exists.
// This is required by mostly all commands except profile
func PreRunDefaultProfile(cmd *cobra.Command, args []string) error {
	conf, err := config.ReadConfigFromFile()
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("No config found to run this command. Add a profile using pb profile command")
		} else {
			return err
		}
	}
	if conf.Profiles == nil || conf.Default_profile == "" {
		return errors.New("no profile is configured to run this command. please create one using profile command")
	}

	DefaultProfile = conf.Profiles[conf.Default_profile]
	return nil
}
