// Copyright (c) 2024 Parseable, Inc
//
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

package model

import (
	"fmt"
	"io"
	"pb/pkg/ui"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Time-range presets — match the mock at terminal/page.tsx ViewTime.
// Custom… is a placeholder; selecting it leaves start/end editable.
const (
	FiveMinute = -5 * time.Minute
	TenMinute  = -10 * time.Minute
	OneHour    = -1 * time.Hour
	FiveHour   = -5 * time.Hour
	OneDay     = -24 * time.Hour
	ThreeDay   = -72 * time.Hour
	OneWeek    = -168 * time.Hour
)

var (
	timeDurations = []list.Item{
		timeDurationItem{duration: FiveMinute, repr: "5 Minutes"},
		timeDurationItem{duration: TenMinute, repr: "10 Minutes"},
		timeDurationItem{duration: OneHour, repr: "1 Hour"},
		timeDurationItem{duration: FiveHour, repr: "5 Hours"},
		timeDurationItem{duration: OneDay, repr: "1 Day"},
		timeDurationItem{duration: ThreeDay, repr: "3 Days"},
		timeDurationItem{duration: OneWeek, repr: "7 Days"},
		timeDurationItem{duration: 0, repr: "Custom…"},
	}
)

type timeDurationItem struct {
	duration time.Duration
	repr     string
}

func (i timeDurationItem) FilterValue() string { return i.repr }

type timeDurationItemDelegate struct{}

func (d timeDurationItemDelegate) Height() int                             { return 1 }
func (d timeDurationItemDelegate) Spacing() int                            { return 0 }
func (d timeDurationItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

// Render: selected row gets a 1-cell Accent rail + bold Accent label.
// Idle rows are Body fg with leading 2 spaces for alignment. No bg
// fills — they don't pad cleanly past trailing ANSI resets.
func (d timeDurationItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(timeDurationItem)
	if !ok {
		return
	}
	p := ui.Active

	if index == m.Index() {
		rail := lipgloss.NewStyle().Background(p.Accent).Render(" ")
		name := lipgloss.NewStyle().Foreground(p.Accent).Bold(true).Render(i.repr)
		fmt.Fprint(w, rail+" "+name)
		return
	}
	name := lipgloss.NewStyle().Foreground(p.Body).Render(i.repr)
	fmt.Fprint(w, "  "+name)
}

// NewTimeRangeModel creates new range list — narrow column, no title,
// no pagination, no filter. Title is rendered by the modal wrapper.
func NewTimeRangeModel() list.Model {
	l := list.New(timeDurations, timeDurationItemDelegate{}, 24, 10)
	l.SetShowPagination(false)
	l.SetShowHelp(false)
	l.SetShowFilter(false)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	return l
}
