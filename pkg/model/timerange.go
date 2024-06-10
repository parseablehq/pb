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
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Items for time range
const (
	TenMinute    = -10 * time.Minute
	TwentyMinute = -20 * time.Minute
	ThirtyMinute = -30 * time.Minute
	OneHour      = -1 * time.Hour
	ThreeHour    = -3 * time.Hour
	OneDay       = -24 * time.Hour
	ThreeDay     = -72 * time.Hour
	OneWeek      = -168 * time.Hour
)

var (
	timeDurations = []list.Item{
		timeDurationItem{duration: TenMinute, repr: "10 Minutes"},
		timeDurationItem{duration: TwentyMinute, repr: "20 Minutes"},
		timeDurationItem{duration: ThirtyMinute, repr: "30 Minutes"},
		timeDurationItem{duration: OneHour, repr: "1 Hour"},
		timeDurationItem{duration: ThreeHour, repr: "3 Hours"},
		timeDurationItem{duration: OneDay, repr: "1 Day"},
		timeDurationItem{duration: ThreeDay, repr: "3 Days"},
		timeDurationItem{duration: OneWeek, repr: "1 Week"},
	}

	listItemRender         = lipgloss.NewStyle().Foreground(StandardSecondary)
	listSelectedItemRender = lipgloss.NewStyle().Foreground(FocusPrimary)
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
func (d timeDurationItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(timeDurationItem)
	if !ok {
		return
	}

	fn := listItemRender.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return listSelectedItemRender.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(i.repr))
}

// NewTimeRangeModel creates new range model
func NewTimeRangeModel() list.Model {
	list := list.New(timeDurations, timeDurationItemDelegate{}, 20, 10)
	list.SetShowPagination(false)
	list.SetShowHelp(false)
	list.SetShowFilter(false)
	list.SetShowTitle(true)
	list.Styles.TitleBar = baseStyle.Copy()
	list.Styles.Title = baseStyle.Copy().MarginBottom(1)
	list.Styles.TitleBar.Align(lipgloss.Left)
	list.Title = "Select Time Range"
	list.SetShowStatusBar(false)

	return list
}
