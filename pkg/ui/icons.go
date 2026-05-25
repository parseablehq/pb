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
	"os"
	"strings"
)

// IconSet holds every glyph that varies between Nerd Font and plain
// ASCII. View code should reference Icons() — never hardcode a glyph.
//
// Default is ASCII-safe (Unicode box-drawing + common bullets). Users
// who run a Nerd Font–patched terminal can opt in with PB_ICONS=nerd.
type IconSet struct {
	// Selection rail / row markers
	Cursor string // selected row marker — ▸ or
	Pin    string // pinned item — ★ or
	Bullet string // idle row marker — · or empty
	// Status / segments
	Cluster string //  in nerd
	User    string //  in nerd
	Dataset string //  in nerd
	Time    string //  in nerd
	Live    string // ● both sets
	Search  string //  / >
	Help    string //  / ?
	// Separators
	VSep string // │ both sets
	Sep  string // · / ·
}

var (
	asciiIcons = IconSet{
		Cursor:  "▸",
		Pin:     "★",
		Bullet:  "·",
		Cluster: "@",
		User:    "u",
		Dataset: "D",
		Time:    "t",
		Live:    "●",
		Search:  "›",
		Help:    "?",
		VSep:    "│",
		Sep:     "·",
	}

	nerdIcons = IconSet{
		Cursor:  "", //
		Pin:     "", //
		Bullet:  "·",
		Cluster: "", //
		User:    "", //
		Dataset: "", //  reuse
		Time:    "", //
		Live:    "●",
		Search:  "", //
		Help:    "", //
		VSep:    "│",
		Sep:     "·",
	}

	activeIcons = LoadIcons()
)

// LoadIcons reads PB_ICONS and returns the matching set. ASCII by
// default — Nerd is opt-in to avoid broken glyphs on stock terminals.
//
//	PB_ICONS=nerd   → nerdIcons
//	PB_ICONS=ascii  → asciiIcons (default)
func LoadIcons() IconSet {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PB_ICONS"))) {
	case "nerd":
		return nerdIcons
	default:
		return asciiIcons
	}
}

// SetActiveIcons replaces the process-wide icon set.
func SetActiveIcons(s IconSet) { activeIcons = s }

// Icons returns the active set — use this in every view.
func Icons() IconSet { return activeIcons }
