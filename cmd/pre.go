// Copyright (c) 2023 Cloudnatively Services Pvt Ltd
//
// This file is part of MinIO Object Storage stack
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
