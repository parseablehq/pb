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
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Chrome primitives — reusable across SQL, PromQL, results, picker.
// Each returns a fully styled string ready to JoinVertical into a view.
//
// All sizing is in cells. Caller passes total width; primitives distribute
// among internal columns.

// KV is one row in the HeaderStrip context block.
type KV struct {
	Key     string
	Value   string
	Variant KVVariant
}

type KVVariant int

// KVNormal, KVAccent, KVMute control the color variant of a KV row in HeaderStrip.
const (
	KVNormal KVVariant = iota
	KVAccent
	KVMute
)

// KeyHint is one entry in the HeaderStrip keybind grid.
type KeyHint struct {
	Key   string
	Label string
}

// HeaderStrip renders the top context strip:
//
//	[ KV context | keybind grid | PB logo ]
//
// All three columns are visible at >=90 cols. Below that the logo is
// dropped first, then the keybind grid collapses to two columns.
func HeaderStrip(width int, ctx []KV, keys []KeyHint) string {
	p := Active

	// Reserve the logo column only when there is room. Logo block is
	// 16 cells wide including padding.
	const logoCol = 18
	const minLogoTotal = 92

	leftW := 32
	if width < 76 {
		leftW = max(22, width/3)
	}

	var rightW, midW int
	showLogo := width >= minLogoTotal
	if showLogo {
		rightW = logoCol
		midW = width - leftW - rightW
	} else {
		rightW = 0
		midW = width - leftW
	}
	if midW < 20 {
		midW = 20
	}

	// ── Left column: KV context ──
	leftCol := renderKVBlock(ctx, leftW-4)
	leftPane := lipgloss.NewStyle().
		Width(leftW).
		Padding(0, 2).
		Background(p.Panel).
		BorderStyle(lipgloss.NormalBorder()).
		BorderRight(true).
		BorderForeground(p.Border).
		Render(leftCol)

	// ── Middle column: keybind grid (2 or 3 cols based on width) ──
	cols := 3
	if midW < 56 {
		cols = 2
	}
	gridRows := buildKeyGrid(keys, cols, midW-4)
	midStyle := lipgloss.NewStyle().
		Width(midW).
		Padding(0, 2).
		Background(p.Panel)
	if showLogo {
		midStyle = midStyle.
			BorderStyle(lipgloss.NormalBorder()).
			BorderRight(true).
			BorderForeground(p.Border)
	}
	midPane := midStyle.Render(gridRows)

	// ── Right column: PB logo ──
	row := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, midPane)
	if showLogo {
		rightPane := lipgloss.NewStyle().
			Width(rightW).
			Padding(0, 1).
			Background(p.Panel).
			Render(RenderLogo())
		row = lipgloss.JoinHorizontal(lipgloss.Top, leftPane, midPane, rightPane)
	}

	return lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(p.Border).
		Render(row)
}

// renderKVBlock paints one `KEY  value` line per context entry.
// Keys are forced uppercase + Ghost color — the project-wide "label"
// type style. Values use the variant color (Body / Accent / Mute) so
// readers can scan the bold/colored values without label noise.
func renderKVBlock(ctx []KV, valW int) string {
	p := Active
	keyW := 9
	if valW < 6 {
		valW = 6
	}
	var lines []string
	for _, kv := range ctx {
		keyStyle := lipgloss.NewStyle().Foreground(p.Faint).Width(keyW)
		valStyle := lipgloss.NewStyle().Foreground(p.Body)
		switch kv.Variant {
		case KVAccent:
			valStyle = valStyle.Foreground(p.Accent).Bold(true)
		case KVMute:
			valStyle = valStyle.Foreground(p.Mute)
		}
		lines = append(lines,
			keyStyle.Render(strings.ToUpper(kv.Key))+
				valStyle.Render(truncate(kv.Value, valW)))
	}
	return strings.Join(lines, "\n")
}

func buildKeyGrid(keys []KeyHint, cols, availW int) string {
	p := Active
	if len(keys) == 0 {
		return ""
	}
	rows := (len(keys) + cols - 1) / cols
	colW := availW / cols
	if colW < 14 {
		colW = 14
	}
	out := make([]string, rows)
	for ri := 0; ri < rows; ri++ {
		var row []string
		for ci := 0; ci < cols; ci++ {
			idx := ri*cols + ci
			if idx >= len(keys) {
				row = append(row, lipgloss.NewStyle().Width(colW).Render(""))
				continue
			}
			k := keys[idx]
			cell := lipgloss.JoinHorizontal(
				lipgloss.Top,
				lipgloss.NewStyle().Foreground(p.Accent).Render(k.Key),
				" ",
				lipgloss.NewStyle().Foreground(p.Mute).Render(truncate(k.Label, colW-lipgloss.Width(k.Key)-2)),
			)
			row = append(row, lipgloss.NewStyle().Width(colW).Render(cell))
		}
		out[ri] = lipgloss.JoinHorizontal(lipgloss.Top, row...)
	}
	return strings.Join(out, "\n")
}

// Breadcrumb is one tab/crumb in the bottom navigation.
type Breadcrumb struct {
	ID     string
	Label  string
	Active bool
}

// Breadcrumbs renders the bottom navigation row. Active item gets an
// accent fill with inverted text; the rest are accent-fg on transparent.
func Breadcrumbs(width int, items []Breadcrumb) string {
	p := Active
	var rendered []string
	for _, it := range items {
		style := lipgloss.NewStyle().Padding(0, 2)
		if it.Active {
			style = style.Background(p.Accent).Foreground(p.InvertText).Bold(true)
		} else {
			style = style.Foreground(p.Accent).Background(p.Bg)
		}
		rendered = append(rendered, style.Render("<"+it.Label+">"))
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	usedW := lipgloss.Width(row)
	if usedW < width {
		row = row + lipgloss.NewStyle().
			Width(width-usedW).
			Background(p.Bg).
			Render("")
	}
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(p.Border).
		Render(row)
}

// Card renders a bordered surface with an uppercase title strip.
// Focused = BorderHi outline + accent title; idle = Border + dim title.
// Width is total cells including borders; Height is rows inside body.
//
// Use Card for every pane in a view — editor, preview, time, results —
// so the TUI reads as one visual system.
func Card(title string, width, height int, focused bool, body string) string {
	return CardWithMeta(title, "", width, height, focused, body)
}

// CardWithMeta is Card with an optional meta string rendered right of
// the title (e.g. "FRONTEND · SQL · UNSAVED"). Meta is always Faint.
//
//	┌─ EDITOR ────────────────  FRONTEND · SQL · UNSAVED ┐
//	│ ...body...                                          │
//	└─────────────────────────────────────────────────────┘
func CardWithMeta(title, meta string, width, height int, focused bool, body string) string {
	p := Active

	// Border always BorderSoft — subtle chrome, no bright outlines.
	// Focus signal lives only on the title color (Ghost → Accent bold).
	borderColor := p.BorderSoft
	titleFg := p.Ghost
	if focused {
		titleFg = p.Accent
	}

	innerW := width - 2
	if innerW < 4 {
		innerW = 4
	}

	// Title row — left "— TITLE —" in accent/dim, optional meta right
	// in Faint. Matches mock SQL view header.
	// No dashes — plain uppercase, letter-spaced via single space
	// between glyphs. Reads as a real label, not vim splash text.
	spaced := strings.Join(strings.Split(strings.ToUpper(title), ""), " ")
	left := lipgloss.NewStyle().
		Foreground(titleFg).
		Background(p.Panel).
		Bold(focused).
		Render(spaced)
	var titleRow string
	if meta != "" {
		right := lipgloss.NewStyle().
			Foreground(p.Faint).
			Background(p.Panel).
			Render(strings.ToUpper(meta))
		gap := innerW - lipgloss.Width(left) - lipgloss.Width(right) - 2
		if gap < 1 {
			gap = 1
		}
		titleRow = lipgloss.NewStyle().
			Width(innerW).
			Padding(0, 1).
			Background(p.Panel).
			Render(left + strings.Repeat(" ", gap) + right)
	} else {
		titleRow = lipgloss.NewStyle().
			Width(innerW).
			Padding(0, 1).
			Background(p.Panel).
			Render(left)
	}

	separator := lipgloss.NewStyle().
		Width(innerW).
		Foreground(borderColor).
		Background(p.Panel).
		Render(strings.Repeat("─", innerW))

	bodyStyled := lipgloss.NewStyle().
		Width(innerW).
		Height(height).
		Padding(0, 1).
		Background(p.Panel).
		Foreground(p.Body).
		Render(body)

	stack := lipgloss.JoinVertical(lipgloss.Left, titleRow, separator, bodyStyled)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(p.Panel).
		Render(stack)
}

// PaneTitle is a small uppercase title strip used at the top of result
// panes (preview, detail, etc).
func PaneTitle(width int, title, right string) string {
	p := Active
	left := lipgloss.NewStyle().
		Foreground(p.Accent).
		Render("─ " + title + " ─")
	if right == "" {
		return lipgloss.NewStyle().
			Width(width).
			Padding(0, 2).
			Background(p.Panel).
			BorderStyle(lipgloss.NormalBorder()).
			BorderBottom(true).
			BorderForeground(p.Border).
			Render(left)
	}
	rt := lipgloss.NewStyle().Foreground(p.Faint).Render(right)
	gap := width - lipgloss.Width(left) - lipgloss.Width(rt) - 4
	if gap < 1 {
		gap = 1
	}
	row := lipgloss.JoinHorizontal(
		lipgloss.Top,
		left,
		strings.Repeat(" ", gap),
		rt,
	)
	return lipgloss.NewStyle().
		Width(width).
		Padding(0, 2).
		Background(p.Panel).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(p.Border).
		Render(row)
}

// Pill renders a small toggle pill — used by PromQL toolbar.
func Pill(label string, active bool) string {
	p := Active
	if active {
		return lipgloss.NewStyle().
			Foreground(p.InvertText).
			Background(p.Accent).
			Bold(true).
			Padding(0, 1).
			Render(label)
	}
	return lipgloss.NewStyle().
		Foreground(p.Mute).
		Border(lipgloss.NormalBorder(), true).
		BorderForeground(p.Border).
		Padding(0, 1).
		Render(label)
}

// SelectionRail returns a row prefix consisting of a 1-cell rail in the
// active palette's accent color when selected, transparent otherwise.
func SelectionRail(selected bool) string {
	p := Active
	if selected {
		return lipgloss.NewStyle().
			Background(p.Accent).
			Render(" ")
	}
	return " "
}

// FormatStat renders a small label/value stat tile.
func FormatStat(label, value string) string {
	p := Active
	box := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true).
		BorderForeground(p.Border).
		Background(p.PanelAlt).
		Padding(0, 2)
	inner := lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.NewStyle().Foreground(p.Dim).Render(label),
		lipgloss.NewStyle().Foreground(p.Body).Bold(true).Render(value),
	)
	return box.Render(inner)
}

// Truncate clips s to n visible cells, appending "…" if needed.
func Truncate(s string, n int) string {
	return truncate(s, n)
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return s[:n-1] + "…"
}
