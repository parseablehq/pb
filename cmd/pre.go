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

package cmd

import (
	"errors"
	"os"
	"pb/pkg/config"

	"github.com/spf13/cobra"
)

var DefaultProfile config.Profile

// PreRunDefaultProfile if a profile exists.
// This is required by mostly all commands except profile
func PreRunDefaultProfile(_ *cobra.Command, _ []string) error {
	return PreRun()
}

func PreRun() error {
	conf, err := config.ReadConfigFromFile()
	if os.IsNotExist(err) {
		return errors.New("no config found to run this command. add a profile using pb profile command")
	} else if err != nil {
		return err
	}

	if conf.Profiles == nil || conf.DefaultProfile == "" {
		return errors.New("no profile is configured to run this command. please create one using profile command")
	}

	DefaultProfile = conf.Profiles[conf.DefaultProfile]
	return nil
}
