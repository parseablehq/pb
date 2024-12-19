// Copyright (c) 2024 Parseable, Inc
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

package installer

import (
	"fmt"
	"time"

	"pb/pkg/common"

	"github.com/briandowns/spinner"
)

func createDeploymentSpinner(namespace, infoMsg string) *spinner.Spinner {
	// Custom spinner with multiple character sets for dynamic effect
	spinnerChars := []string{
		"●", "○", "◉", "○", "◉", "○", "◉", "○", "◉",
	}

	s := spinner.New(
		spinnerChars,
		120*time.Millisecond,
		spinner.WithColor(common.Yellow),
		spinner.WithSuffix(" ..."),
	)

	s.Prefix = fmt.Sprintf(common.Yellow + infoMsg)

	return s
}
