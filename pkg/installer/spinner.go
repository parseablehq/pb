package installer

import (
	"fmt"
	"pb/pkg/common"
	"time"

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

	s.Prefix = fmt.Sprintf(common.Yellow+infoMsg+" %s ", namespace)

	return s
}
