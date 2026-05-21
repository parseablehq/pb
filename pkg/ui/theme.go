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

// ─────────────────────────────────────────────────────────────────────────────
// pb — Color + Typography System
// Source of truth for every color and text style in the TUI.
//
// Rules:
//   - Never hardcode a hex value outside this file.
//   - Every surface, text, and semantic color comes from Palette.
//   - Typography is expressed as lipgloss.Style — use the named styles below,
//     don't construct ad-hoc styles in view files.
//   - Background hierarchy (dark):  Bg < Panel < PanelAlt < EditorBg
//   - Background hierarchy (light): Bg > Panel > PanelAlt > EditorBg  (inverted — lighter = more surfaced)
// ─────────────────────────────────────────────────────────────────────────────

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ── Palette ───────────────────────────────────────────────────────────────────

type Palette struct {
	// ── Surfaces ────────────────────────────────────────────────────────────
	Bg       lipgloss.Color // outermost terminal background
	Panel    lipgloss.Color // header strip, statusbar, pane titles
	PanelAlt lipgloss.Color // cards, sidebar alt rows
	EditorBg lipgloss.Color // SQL / PromQL textarea surface

	// ── Borders ─────────────────────────────────────────────────────────────
	Border     lipgloss.Color // standard divider — between panels, table rows
	BorderSoft lipgloss.Color // subtle — table row separator, inner dividers
	BorderHi   lipgloss.Color // focus ring, active pane outline

	// ── Text ramp ───────────────────────────────────────────────────────────
	Text  lipgloss.Color
	Body  lipgloss.Color
	Mute  lipgloss.Color
	Dim   lipgloss.Color
	Faint lipgloss.Color
	Ghost lipgloss.Color

	// ── Brand / Accent ───────────────────────────────────────────────────────
	Accent     lipgloss.Color
	Accent2    lipgloss.Color
	AccentSoft lipgloss.Color

	// ── Semantic ─────────────────────────────────────────────────────────────
	Ok       lipgloss.Color
	OkSoft   lipgloss.Color
	OkSoftBg lipgloss.Color
	Warn     lipgloss.Color
	Err      lipgloss.Color

	// ── Syntax ───────────────────────────────────────────────────────────────
	String lipgloss.Color
	Number lipgloss.Color

	// ── Interaction ──────────────────────────────────────────────────────────
	SelRow       lipgloss.Color
	Cursor       lipgloss.Color
	EditorActive lipgloss.Color
	InvertText   lipgloss.Color

	// ── Overlay / Badge ──────────────────────────────────────────────────────
	Overlay lipgloss.Color
	BadgeBg lipgloss.Color
}

// ── Dark theme ────────────────────────────────────────────────────────────────

var Dark = Palette{
	Bg:       "#0B0B12",
	Panel:    "#13131F",
	PanelAlt: "#1A1A2A",
	EditorBg: "#1B1B1F",

	Border:     "#2A2A3D",
	BorderSoft: "#1F1F2E",
	BorderHi:   "#5050A8",

	Text:  "#F4F4F5",
	Body:  "#E4E4E7",
	Mute:  "#B5B5BE",
	Dim:   "#9595A0",
	Faint: "#7E7E8A",
	Ghost: "#525260",

	Accent:     "#9E9EF0",
	Accent2:    "#8484C8",
	AccentSoft: "#2A2A4E",

	Ok:       "#34D399",
	OkSoft:   "#6EE7B7",
	OkSoftBg: "#1A3D3A",
	Warn:     "#FB923C",
	Err:      "#F87171",

	String: "#5EEAD4",
	Number: "#C084FC",

	SelRow:       "#25253A",
	Cursor:       "#9E9EF0",
	EditorActive: "#26262C",
	InvertText:   "#0B0B12",

	Overlay: "#070710",
	BadgeBg: "#1E1E28",
}

// ── Light theme ───────────────────────────────────────────────────────────────

var Light = Palette{
	Bg:       "#FBFBFD",
	Panel:    "#FFFFFF",
	PanelAlt: "#F1F1F5",
	EditorBg: "#F4F4F5",

	Border:     "#D8D8DC",
	BorderSoft: "#E9E9EC",
	BorderHi:   "#B2B2FF",

	Text:  "#09090B",
	Body:  "#18181B",
	Mute:  "#3F3F46",
	Dim:   "#52525B",
	Faint: "#71717A",
	Ghost: "#A8A8AE",

	Accent:     "#3A3A8C",
	Accent2:    "#5151A0",
	AccentSoft: "#E5E5FF",

	Ok:       "#047857",
	OkSoft:   "#047857",
	OkSoftBg: "#DCEEE7",
	Warn:     "#C2410C",
	Err:      "#B91C1C",

	String: "#047857",
	Number: "#7E22CE",

	SelRow:       "#EEEEF9",
	Cursor:       "#3A3A8C",
	EditorActive: "#EAEAEC",
	InvertText:   "#FFFFFF",

	Overlay: "#F1F1F5",
	BadgeBg: "#EDEDEF",
}

// ── Theme loader ─────────────────────────────────────────────────────────────

// LoadTheme reads $PB_THEME (light | dark | auto).
// Auto sniffs $COLORFGBG; falls back to dark.
func LoadTheme() Palette {
	switch strings.ToLower(os.Getenv("PB_THEME")) {
	case "light":
		return Light
	case "dark":
		return Dark
	default:
		cfg := os.Getenv("COLORFGBG")
		if cfg != "" {
			parts := strings.Split(cfg, ";")
			if len(parts) >= 2 && parts[len(parts)-1] == "15" {
				return Light
			}
		}
		return Dark
	}
}

// Adaptive builds a lipgloss.AdaptiveColor where the light/dark sides
// come from Light/Dark via the given picker. Use for vars declared at
// package init time, before SetActive runs.
func Adaptive(pick func(p Palette) lipgloss.Color) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{
		Light: string(pick(Light)),
		Dark:  string(pick(Dark)),
	}
}

// Adapt is an alias for Adaptive — kept for backwards compatibility
// with existing callers.
func Adapt(pick func(p Palette) lipgloss.Color) lipgloss.AdaptiveColor {
	return Adaptive(pick)
}

// Active is the process-wide palette. Set once at TUI startup.
var Active = LoadTheme()

// SetActive replaces the process-wide palette.
func SetActive(p Palette) {
	Active = p
	ActiveType = NewTypography(p)
}

// ─────────────────────────────────────────────────────────────────────────────
// Typography
//
// Named lipgloss styles. View code should use these instead of building
// ad-hoc styles. Refresh via NewTypography() after SetActive().
// ─────────────────────────────────────────────────────────────────────────────

type Typography struct {
	// Structural labels — column headers, pane titles
	Label lipgloss.Style

	// Key chips — <ctrl-r>, <enter>, keymap display
	KeyChip lipgloss.Style

	// Standard text levels
	Body  lipgloss.Style
	Mute  lipgloss.Style
	Dim   lipgloss.Style
	Ghost lipgloss.Style

	// Semantic
	Accent lipgloss.Style
	Err    lipgloss.Style
	Warn   lipgloss.Style
	Ok     lipgloss.Style

	// Monospace values
	Timestamp lipgloss.Style
	TraceID   lipgloss.Style

	// Breadcrumb
	Crumb       lipgloss.Style
	CrumbActive lipgloss.Style

	// Badges
	BadgeSQL    lipgloss.Style
	BadgePromQL lipgloss.Style
	BadgeOk     lipgloss.Style
	BadgeErr    lipgloss.Style
	BadgeWarn   lipgloss.Style

	// Status bar
	StatusMode    lipgloss.Style
	StatusSegment lipgloss.Style
	StatusLive    lipgloss.Style
}

// NewTypography builds all styles from a palette.
func NewTypography(p Palette) Typography {
	base := lipgloss.NewStyle()

	return Typography{
		Label: base.Foreground(p.Dim),

		KeyChip: base.
			Foreground(p.Accent).
			Background(p.PanelAlt).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.Border).
			Padding(0, 1),

		Body:  base.Foreground(p.Body),
		Mute:  base.Foreground(p.Mute),
		Dim:   base.Foreground(p.Dim),
		Ghost: base.Foreground(p.Ghost),

		Accent: base.Foreground(p.Accent),
		Err:    base.Foreground(p.Err),
		Warn:   base.Foreground(p.Warn),
		Ok:     base.Foreground(p.Ok),

		Timestamp: base.Foreground(p.Ghost),
		TraceID:   base.Foreground(p.Dim),

		Crumb: base.
			Foreground(p.Faint).
			Padding(0, 1),
		CrumbActive: base.
			Foreground(p.InvertText).
			Background(p.Accent).
			Padding(0, 1),

		BadgeSQL: base.
			Foreground(p.Accent).
			Background(p.AccentSoft).
			Padding(0, 1),
		BadgePromQL: base.
			Foreground(p.OkSoft).
			Background(p.OkSoftBg).
			Padding(0, 1),
		BadgeOk: base.
			Foreground(p.OkSoft).
			Background(p.OkSoftBg).
			Padding(0, 1),
		BadgeErr: base.
			Foreground(p.Err).
			Background(p.BadgeBg).
			Padding(0, 1),
		BadgeWarn: base.
			Foreground(p.Warn).
			Background(p.BadgeBg).
			Padding(0, 1),

		StatusMode:    base.Foreground(p.Accent).Bold(true),
		StatusSegment: base.Foreground(p.Faint),
		StatusLive:    base.Foreground(p.Ok),
	}
}

// ActiveType is the process-wide Typography. Rebuilt on SetActive.
var ActiveType = NewTypography(Active)

// Type returns the active Typography — sugar for ActiveType.
func Type() Typography { return ActiveType }
