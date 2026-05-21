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

package cmd

import (
	"fmt"
	"pb/pkg/ui"
	"pb/pkg/ui/views"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// TuiCmd is the greenfield TUI entrypoint. It does NOT replace the
// existing `pb query -i` interactive mode; both coexist while the new
// pkg/ui/views/* implementations reach parity.
//
//	pb tui                  → start on picker
//	PB_THEME=light pb tui   → light palette
var TuiCmd = &cobra.Command{
	Use:               "tui",
	Short:             "Open the redesigned interactive TUI (greenfield)",
	Long:              "Open the redesigned TUI (pkg/ui/*). Picker is the entry view; use 1-7 or breadcrumbs to switch.",
	PersistentPreRunE: PreRunDefaultProfile,
	RunE:              runTUI,
}

func runTUI(_ *cobra.Command, _ []string) error {
	// Active profile + theme. PreRunDefaultProfile already populated
	// the package-level DefaultProfile var.
	profile := DefaultProfile
	if profile.URL == "" {
		return fmt.Errorf("no default profile — run `pb profile add` and `pb profile default <name>` first")
	}
	ui.SetActive(ui.LoadTheme())
	ui.SetActiveIcons(ui.LoadIcons())

	// Register one view per ViewID. Picker is real; others are empty
	// placeholders until they get ported in subsequent PRs.
	vmap := map[ui.ViewID]ui.View{
		ui.ViewQuery:   views.EmptyView{Title: "QUERY", Hint: "SQL editor — coming next. Use `pb query -i` for now."},
		ui.ViewResults: views.EmptyView{Title: "RESULTS", Hint: "Run a query to see results here."},
		ui.ViewMetrics: views.EmptyView{Title: "METRICS", Hint: "PromQL editor — coming next. Use `pb query --promql -i` for now."},
		ui.ViewPicker:  views.NewPicker(),
		ui.ViewTime:    views.EmptyView{Title: "TIME RANGE", Hint: "Time picker modal — coming next."},
		ui.ViewSaved:   views.EmptyView{Title: "SAVED", Hint: "Saved queries — coming next."},
		ui.ViewHelp:    views.EmptyView{Title: "HELP", Hint: "Press 1-7 to switch views. Esc returns to query. Ctrl-c quits."},
	}
	app := ui.NewApp(profile, vmap)

	_, err := tea.NewProgram(app, tea.WithAltScreen()).Run()
	return err
}

