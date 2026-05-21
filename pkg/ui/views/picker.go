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

package views

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"pb/pkg/config"
	"pb/pkg/ui"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PickerView is the fuzzy dataset list (mock §5.4). One column, search
// bar at top doubles as title. Selection rail + SelRow bg on focused row.
type PickerView struct {
	query       string
	items       []dsItem
	filtered    []dsItem
	cursor      int
	loading     bool
	err         string
	loadStarted bool
	pinned      map[string]bool
}

// dsItem is one dataset listed in the picker.
type dsItem struct {
	Name   string
	Kind   string // logs · metrics · events · traces (inferred)
	Fields int
}

// NewPicker returns an empty picker that fetches on first render.
func NewPicker() *PickerView {
	return &PickerView{
		pinned: map[string]bool{},
	}
}

func (v *PickerView) Init() tea.Cmd { return nil }

// datasetListMsg is delivered when the backend dataset list arrives.
type datasetListMsg struct {
	items []dsItem
	err   string
}

func (v *PickerView) Update(msg tea.Msg, ctx *ui.AppCtx) tea.Cmd {
	switch m := msg.(type) {
	case datasetListMsg:
		v.loading = false
		v.err = m.err
		v.items = m.items
		v.filter()
		return nil
	case tea.KeyMsg:
		switch m.String() {
		case "down":
			if v.cursor < len(v.filtered)-1 {
				v.cursor++
			}
			return nil
		case "up":
			if v.cursor > 0 {
				v.cursor--
			}
			return nil
		case "enter":
			if v.cursor >= 0 && v.cursor < len(v.filtered) {
				ctx.SelectDataset(v.filtered[v.cursor].Name)
			}
			return nil
		case "backspace":
			if len(v.query) > 0 {
				v.query = v.query[:len(v.query)-1]
				v.filter()
			}
			return nil
		case "p":
			// pin/unpin the current row (keep `p` as a literal for filter
			// if user is mid-search; here we treat it as command since
			// most names don't include letter p in fuzzy queries — TODO:
			// chord on ctrl+p later)
			if v.cursor < len(v.filtered) {
				name := v.filtered[v.cursor].Name
				v.pinned[name] = !v.pinned[name]
				v.filter()
			}
			return nil
		default:
			// Treat any single printable rune as filter input.
			s := m.String()
			if len(s) == 1 && s[0] >= 0x20 && s[0] < 0x7F {
				v.query += s
				v.filter()
			}
			return nil
		}
	}
	// On first call (no key yet) kick off the fetch.
	if !v.loadStarted {
		v.loadStarted = true
		v.loading = true
		return fetchDatasets(ctx.Profile)
	}
	return nil
}

// filter narrows items by substring match and re-orders pinned first.
func (v *PickerView) filter() {
	q := strings.ToLower(v.query)
	var matched []dsItem
	for _, it := range v.items {
		if q == "" || strings.Contains(strings.ToLower(it.Name), q) {
			matched = append(matched, it)
		}
	}
	// pinned bubble to top, preserve relative order otherwise
	sort.SliceStable(matched, func(i, j int) bool {
		pi, pj := v.pinned[matched[i].Name], v.pinned[matched[j].Name]
		if pi != pj {
			return pi
		}
		return false
	})
	v.filtered = matched
	if v.cursor >= len(v.filtered) {
		v.cursor = max0(len(v.filtered) - 1)
	}
}

func (v *PickerView) HeaderKeys() []ui.KeyHint {
	return []ui.KeyHint{
		{Key: "type", Label: "Filter"},
		{Key: "<↑/↓>", Label: "Navigate"},
		{Key: "<enter>", Label: "Open"},
		{Key: "<p>", Label: "Pin"},
		{Key: "<?>", Label: "Help"},
		{Key: "<ctrl-c>", Label: "Quit"},
	}
}

func (v *PickerView) Render(width, height int, _ *ui.AppCtx) string {
	p := ui.Active

	// Card width — clamp to mock's 460px ≈ 60 cells with a bit of slack.
	cardW := 64
	if width < 70 {
		cardW = width - 4
	}

	// ── Search bar / title ──
	cursor := lipgloss.NewStyle().Background(p.Cursor).Render(" ")
	searchVal := lipgloss.NewStyle().Foreground(p.Body).Render(v.query)
	prompt := lipgloss.NewStyle().Foreground(p.Accent).Bold(true).Render("›")
	count := lipgloss.NewStyle().Foreground(p.Faint).Render(
		fmt.Sprintf("%d/%d", len(v.filtered), len(v.items)),
	)
	searchRow := lipgloss.JoinHorizontal(lipgloss.Top,
		" ", prompt, " ", searchVal, cursor,
	)
	gap := cardW - lipgloss.Width(searchRow) - lipgloss.Width(count) - 2
	if gap < 1 {
		gap = 1
	}
	titleBar := lipgloss.NewStyle().
		Background(p.Panel).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(p.Border).
		Width(cardW).
		Render(searchRow + strings.Repeat(" ", gap) + count + " ")

	// ── List body ──
	maxRows := height - 8 // chrome + footer
	if maxRows < 4 {
		maxRows = 4
	}
	var rows []string
	switch {
	case v.loading:
		rows = []string{lipgloss.NewStyle().Foreground(p.Faint).Padding(1, 2).Render("loading datasets…")}
	case v.err != "":
		rows = []string{lipgloss.NewStyle().Foreground(p.Err).Padding(1, 2).Render(v.err)}
	case len(v.filtered) == 0:
		rows = []string{lipgloss.NewStyle().Foreground(p.Faint).Padding(1, 2).Render("(no matches)")}
	default:
		start := 0
		if v.cursor >= maxRows {
			start = v.cursor - maxRows + 1
		}
		end := start + maxRows
		if end > len(v.filtered) {
			end = len(v.filtered)
		}
		for i := start; i < end; i++ {
			rows = append(rows, renderPickerRow(v.filtered[i], i == v.cursor, v.pinned[v.filtered[i].Name], v.query, cardW))
		}
	}
	listBody := strings.Join(rows, "\n")

	// ── Footer hint row ──
	hint := func(k, lbl string) string {
		return lipgloss.NewStyle().Foreground(p.Accent).Render(k) +
			" " + lipgloss.NewStyle().Foreground(p.Dim).Render(lbl)
	}
	footerLeft := lipgloss.JoinHorizontal(lipgloss.Top,
		hint("↑↓", "nav"), "   ",
		hint("↵", "open"), "   ",
		hint("p", "pin"),
	)
	footerRight := hint("esc", "")
	fgap := cardW - lipgloss.Width(footerLeft) - lipgloss.Width(footerRight) - 4
	if fgap < 1 {
		fgap = 1
	}
	footer := lipgloss.NewStyle().
		Background(p.PanelAlt).
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(p.Border).
		Padding(0, 2).
		Width(cardW).
		Render(footerLeft + strings.Repeat(" ", fgap) + footerRight)

	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.BorderHi).
		Background(p.Panel).
		Render(lipgloss.JoinVertical(lipgloss.Left, titleBar, listBody, footer))

	// Center the card inside body.
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, card,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(p.Bg),
	)
}

// renderPickerRow draws one dataset row in the picker:
//
//	[rail] [cursor] name (match highlighted) [kind] [Nf]
func renderPickerRow(d dsItem, selected, pinned bool, query string, width int) string {
	p := ui.Active

	rail := " "
	if selected {
		rail = lipgloss.NewStyle().Background(p.Accent).Render(" ")
	}
	bg := p.Bg
	if selected {
		bg = p.SelRow
	}

	cursorGlyph := "·"
	cursorFg := p.Body
	switch {
	case selected:
		cursorGlyph = "▸"
		cursorFg = p.Accent
	case pinned:
		cursorGlyph = "★"
		cursorFg = p.Accent2
	}

	// Match highlight: lowercase substring → wrap span in accent color.
	name := d.Name
	nameFg := p.Body
	if selected {
		nameFg = p.Text
	}
	var nameRendered string
	if query != "" {
		idx := strings.Index(strings.ToLower(name), strings.ToLower(query))
		if idx >= 0 {
			nameRendered = lipgloss.NewStyle().Foreground(nameFg).Background(bg).Render(name[:idx]) +
				lipgloss.NewStyle().Foreground(p.Accent).Background(bg).Bold(true).Render(name[idx:idx+len(query)]) +
				lipgloss.NewStyle().Foreground(nameFg).Background(bg).Render(name[idx+len(query):])
		} else {
			nameRendered = lipgloss.NewStyle().Foreground(nameFg).Background(bg).Render(name)
		}
	} else {
		nameRendered = lipgloss.NewStyle().Foreground(nameFg).Background(bg).Render(name)
	}

	kindFg := kindColor(d.Kind)
	kindCol := lipgloss.NewStyle().
		Foreground(kindFg).Background(bg).
		Width(10).
		Align(lipgloss.Right).
		Render(d.Kind)

	fieldCol := lipgloss.NewStyle().
		Foreground(p.Faint).Background(bg).
		Width(5).
		Align(lipgloss.Right).
		Render(fmt.Sprintf("%df", d.Fields))

	// Cursor column (3 cells) + flex name + kind + fields = total width
	cursorCell := lipgloss.NewStyle().
		Foreground(cursorFg).Background(bg).
		Width(3).
		Align(lipgloss.Center).
		Render(cursorGlyph)

	nameW := width - 1 - 3 - 10 - 5 - 2 // rail + cursor + kind + field + slack
	if nameW < 6 {
		nameW = 6
	}
	nameCell := lipgloss.NewStyle().
		Background(bg).
		Width(nameW).
		Render(nameRendered)

	return rail + cursorCell + nameCell + kindCol + fieldCol
}

func kindColor(kind string) lipgloss.Color {
	p := ui.Active
	switch kind {
	case "metrics":
		return p.OkSoft
	case "events":
		return p.Warn
	case "traces":
		return p.String
	}
	return p.Accent // logs (default)
}

// fetchDatasets calls /api/v1/logstream and infers kind from name.
// (Mirrors fetchMetricDatasets in pkg/model/promql.go but kept local so
// the greenfield UI has no legacy dependency.)
func fetchDatasets(profile config.Profile) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 15 * time.Second}
		req, err := http.NewRequest("GET", strings.TrimRight(profile.URL, "/")+"/api/v1/logstream", nil)
		if err != nil {
			return datasetListMsg{err: err.Error()}
		}
		if profile.Token != "" {
			req.Header.Set("Authorization", "Bearer "+profile.Token)
		} else {
			req.SetBasicAuth(profile.Username, profile.Password)
		}
		resp, err := client.Do(req)
		if err != nil {
			return datasetListMsg{err: err.Error()}
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 400 {
			return datasetListMsg{err: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))}
		}
		var raw []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			return datasetListMsg{err: err.Error()}
		}
		out := make([]dsItem, 0, len(raw))
		for _, r := range raw {
			out = append(out, dsItem{
				Name:   r.Name,
				Kind:   inferKind(r.Name),
				Fields: 0, // populated lazily on selection
			})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return datasetListMsg{items: out}
	}
}

func inferKind(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "metric"):
		return "metrics"
	case strings.Contains(n, "trace"):
		return "traces"
	case strings.Contains(n, "event"):
		return "events"
	default:
		return "logs"
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
