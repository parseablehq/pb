package cmd

import (
	"fmt"
	"os"
	"pb/pkg/model"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var QueryProfileCmd = &cobra.Command{
	Use:     "query name",
	Short:   "Open Query TUI",
	Args:    cobra.ExactArgs(1),
	PreRunE: PreRunDefaultProfile,
	RunE: func(cmd *cobra.Command, args []string) error {
		stream := args[0]
		p := tea.NewProgram(model.NewQueryModel(DefaultProfile, stream), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Printf("Alas, there's been an error: %v", err)
			os.Exit(1)
		}
		return nil
	},
}
