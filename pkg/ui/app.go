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

package ui

import (
	"pb/pkg/config"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ViewID identifies one of the seven top-level views described in the
// spec (§5). Each is rendered by exactly one View implementation under
// pkg/ui/views/. Modals (picker, time, help) are also reachable as
// top-level views per the mock breadcrumb row — the same code renders
// both as a centered modal (overlay) and as a full-bleed view.
type ViewID int

// ViewQuery through ViewHelp enumerate the top-level views reachable from the navigation bar.
const (
	ViewQuery ViewID = iota
	ViewResults
	ViewMetrics
	ViewPicker
	ViewTime
	ViewSaved
	ViewHelp
)

// Label returns the breadcrumb label for a view.
func (v ViewID) Label() string {
	return []string{"query", "results", "metrics", "picker", "time", "saved", "help"}[v]
}

// View is the minimal contract every page implements. Each implementation
// lives in pkg/ui/views/<name>.go.
//
// Width/height are the inner body cells (chrome already subtracted).
type View interface {
	Init() tea.Cmd
	Update(msg tea.Msg, ctx *AppCtx) tea.Cmd
	Render(width, height int, ctx *AppCtx) string
	// HeaderKeys returns the keybind hints shown in the top HeaderStrip
	// while this view is active. Empty slice = no hints (fall back to
	// the global set).
	HeaderKeys() []KeyHint
}

// AppCtx is the shared mutable state passed to every view's Update.
// Views read from it (current dataset, profile) and write to it via
// the helper setters (e.g. SelectDataset switches the active view).
type AppCtx struct {
	Profile   config.Profile
	Datasets  []string
	Dataset   string
	Latency   string
	RowsLabel string

	// internal: the App writes here to request a view switch from a
	// view's Update. Read once per tea cycle by App.Update and cleared.
	nextView    *ViewID
	statusError string
}

// SwitchTo asks the App to display the named view on the next tick.
func (c *AppCtx) SwitchTo(v ViewID) {
	c.nextView = &v
}

// SelectDataset records the dataset and switches to the query view.
func (c *AppCtx) SelectDataset(name string) {
	c.Dataset = name
	c.SwitchTo(ViewQuery)
}

// SetError records a status-bar error (cleared on next view switch).
func (c *AppCtx) SetError(s string) {
	c.statusError = s
}

// App is the root bubbletea model. It owns the View map, the active
// view ID, and the AppCtx shared across views.
type App struct {
	width   int
	height  int
	current ViewID
	views   map[ViewID]View
	ctx     *AppCtx
}

// NewApp wires the App with the given views map.
func NewApp(profile config.Profile, views map[ViewID]View) App {
	return App{
		current: ViewPicker, // start on picker — pick a dataset first
		views:   views,
		ctx: &AppCtx{
			Profile: profile,
		},
	}
}

func (a App) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, v := range a.views {
		if c := v.Init(); c != nil {
			cmds = append(cmds, c)
		}
	}
	return tea.Batch(cmds...)
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = m.Width
		a.height = m.Height
		return a, nil
	case tea.KeyMsg:
		// Global keys win over view keys. Order matters: ctrl-c, then
		// view-switch digits, then `?` help, then forward to active view.
		switch m.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "1":
			a.current = ViewQuery
			a.ctx.statusError = ""
			return a, nil
		case "2":
			a.current = ViewResults
			a.ctx.statusError = ""
			return a, nil
		case "3":
			a.current = ViewMetrics
			a.ctx.statusError = ""
			return a, nil
		case "4":
			a.current = ViewPicker
			a.ctx.statusError = ""
			return a, nil
		case "5":
			a.current = ViewTime
			a.ctx.statusError = ""
			return a, nil
		case "6":
			a.current = ViewSaved
			a.ctx.statusError = ""
			return a, nil
		case "?", "7":
			a.current = ViewHelp
			a.ctx.statusError = ""
			return a, nil
		case "esc":
			if a.current == ViewHelp {
				a.current = ViewQuery
				a.ctx.statusError = ""
				return a, nil
			}
		}
	}

	// Forward to active view.
	view := a.views[a.current]
	if view == nil {
		return a, nil
	}
	cmd := view.Update(msg, a.ctx)
	if a.ctx.nextView != nil {
		a.current = *a.ctx.nextView
		a.ctx.nextView = nil
		a.ctx.statusError = ""
	}
	return a, cmd
}

func (a App) View() string {
	if a.width == 0 || a.height == 0 {
		return ""
	}
	p := Active

	// ── Top chrome: HeaderStrip with context KVs + view-aware keybinds + logo
	view := a.views[a.current]
	if view == nil {
		view = a.views[ViewQuery]
	}
	ctxKVs := []KV{
		{Key: "Cluster", Value: a.ctx.Profile.URL, Variant: KVMute},
		{Key: "User", Value: orDash(a.ctx.Profile.Username)},
		{Key: "Dataset", Value: orDash(a.ctx.Dataset), Variant: KVAccent},
		{Key: "Latency", Value: orDash(a.ctx.Latency), Variant: KVMute},
		{Key: "Rows", Value: orDash(a.ctx.RowsLabel), Variant: KVMute},
	}
	keys := view.HeaderKeys()
	if len(keys) == 0 {
		keys = defaultKeyHints()
	}
	header := HeaderStrip(a.width, ctxKVs, keys)

	// ── Bottom chrome: Breadcrumbs + StatusBar
	crumbs := Breadcrumbs(a.width, []Breadcrumb{
		{ID: "query", Label: "query", Active: a.current == ViewQuery},
		{ID: "results", Label: "results", Active: a.current == ViewResults},
		{ID: "metrics", Label: "metrics", Active: a.current == ViewMetrics},
		{ID: "picker", Label: "picker", Active: a.current == ViewPicker},
		{ID: "time", Label: "time", Active: a.current == ViewTime},
		{ID: "saved", Label: "saved", Active: a.current == ViewSaved},
		{ID: "help", Label: "help", Active: a.current == ViewHelp},
	})
	status := renderStatusBar(a.width, a.ctx, a.current)

	bodyHeight := a.height - lipgloss.Height(header) - lipgloss.Height(crumbs) - lipgloss.Height(status)
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	body := view.Render(a.width, bodyHeight, a.ctx)
	// Pad body so chrome stays pinned to top/bottom.
	body = lipgloss.NewStyle().
		Width(a.width).
		Height(bodyHeight).
		Background(p.Bg).
		Render(body)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, crumbs, status)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func defaultKeyHints() []KeyHint {
	return []KeyHint{
		{Key: "<1-7>", Label: "Switch view"},
		{Key: "<?>", Label: "Help"},
		{Key: "<ctrl-c>", Label: "Quit"},
	}
}

// renderStatusBar paints the bottom MODE/CLUSTER/ENV/LIVE/t segments
// per mock §4. Lives here (not pkg/model/status.go) so the greenfield
// path has no legacy dep.
func renderStatusBar(width int, ctx *AppCtx, v ViewID) string {
	p := Active
	sep := lipgloss.NewStyle().Foreground(p.Border).Background(p.Panel).Render(" │ ")
	label := func(s string) string {
		return lipgloss.NewStyle().Foreground(p.Faint).Background(p.Panel).Render(s)
	}
	val := func(s string, fg lipgloss.Color, bold bool) string {
		st := lipgloss.NewStyle().Foreground(fg).Background(p.Panel)
		if bold {
			st = st.Bold(true)
		}
		return st.Render(s)
	}

	mode := "—"
	switch v {
	case ViewMetrics:
		mode = "PromQL"
	case ViewQuery, ViewResults, ViewSaved:
		mode = "SQL"
	}

	left := lipgloss.JoinHorizontal(lipgloss.Bottom,
		" ", label("MODE"), " ", val(mode, p.Accent, true),
		sep, label("CLUSTER"), " ", val(orDash(ctx.Profile.URL), p.Dim, false),
		sep, label("ENV"), " ", val("prod", p.Body, false),
	)
	right := lipgloss.JoinHorizontal(lipgloss.Bottom,
		label("LIVE"), " ", val("●", p.Ok, true),
		sep, label("t"), " ", val(orDash(ctx.Latency), p.OkSoft, false),
		sep, label("?"), " ", val("help", p.Dim, false), " ",
	)
	if ctx.statusError != "" {
		right = lipgloss.JoinHorizontal(lipgloss.Bottom,
			label("ERR"), " ", val(ctx.statusError, p.Err, true),
			sep,
			label("?"), " ", val("help", p.Dim, false), " ",
		)
	}

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	pad := lipgloss.NewStyle().Width(gap).Background(p.Panel).Render("")
	row := lipgloss.JoinHorizontal(lipgloss.Bottom, left, pad, right)

	return lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(p.Border).
		Width(width).
		Render(row)
}
