package cmd

import (
	"os"

	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// setupSQLTextColor enables ANSI colors only for terminal output. It returns
// whether to color the result and a cleanup function that restores the console
// mode if it was changed. Call the cleanup function after writing the result.
func setupSQLTextColor() (bool, func()) {
	noop := func() {}
	_, noColor := os.LookupEnv("NO_COLOR")
	if !term.IsTerminal(int(os.Stdout.Fd())) || noColor || os.Getenv("TERM") == "dumb" {
		return false, noop
	}

	restore, err := termenv.EnableVirtualTerminalProcessing(termenv.NewOutput(os.Stdout, termenv.WithProfile(termenv.TrueColor)))
	if err != nil {
		return false, noop
	}
	return true, func() { _ = restore() }
}
