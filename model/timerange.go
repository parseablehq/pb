package model

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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

	listItemRender         = lipgloss.NewStyle().Foreground(lipgloss.Color("#a21"))
	listSelectedItemRender = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFF"))
)

var rangeNavigationMap = [][]string{
	{"list", "start"},
	{"list", "end"},
}

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

type timeRangeModel struct {
	start       time.Time
	end         time.Time
	list_model  list.Model
	start_model textinput.Model
	end_model   textinput.Model
	mode        Mode
	focus       struct {
		x uint
		y uint
	}
}

func (m *timeRangeModel) StartValue() string {
	return m.start.Format(time.RFC3339)
}

func (m *timeRangeModel) EndValue() string {
	return m.end.Format(time.RFC3339)
}

func (m *timeRangeModel) StartValueUtc() string {
	return m.start.UTC().Format(time.RFC3339)
}

func (m *timeRangeModel) EndValueUtc() string {
	return m.end.UTC().Format(time.RFC3339)
}

func (m *timeRangeModel) SetStart(t time.Time) {
	m.start = t
	m.start_model.SetValue(m.StartValue())
}

func (m *timeRangeModel) SetEnd(t time.Time) {
	m.end = t
	m.end_model.SetValue(m.EndValue())
}

func (m *timeRangeModel) Focus() {
	if m.mode == inactive {
		m.mode = navigation
	}
}

func (m *timeRangeModel) Blur() {
	m.mode = inactive
}

func (m *timeRangeModel) focusSelected() {
	m.mode = active
	switch m.currentFocus() {
	case "list":
		return
	case "start":
		m.start_model.Focus()
	case "end":
		m.end_model.Focus()
	}
}

func (m *timeRangeModel) blurSelected() {
	switch m.currentFocus() {
	case "list":
		return
	case "start":
		m.start_model.Blur()
	case "end":
		m.end_model.Blur()
	}
}

func (m *timeRangeModel) Navigate(key tea.KeyMsg) {
	switch key.String() {
	case "up", "w":
		if m.focus.y > 0 {
			m.focus.y -= 1
		}
	case "down", "s":
		if m.focus.y < uint(len(rangeNavigationMap))-1 {
			m.focus.y += 1
		}
	case "left", "a":
		if m.focus.x > 0 {
			m.focus.x -= 1
		}
	case "right", "d":
		if m.focus.x < uint(len(rangeNavigationMap[m.focus.y]))-1 {
			m.focus.x += 1
		}
	default:
		return
	}
}

func (m *timeRangeModel) currentFocus() string {
	return rangeNavigationMap[m.focus.y][m.focus.x]
}

func NewTimeRangeModel() timeRangeModel {
	end_time := time.Now()
	start_time := end_time.Add(TenMinute)

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

	input_style := lipgloss.NewStyle().Bold(true).Faint(true).Width(6).Align(lipgloss.Center)

	start := textinput.New()
	start.Width = datetime_width
	start.Prompt = input_style.Render("start")
	start.SetValue(start_time.Format(time.RFC3339))

	end := textinput.New()
	end.Width = datetime_width
	end.Prompt = input_style.Render("end")
	end.SetValue(end_time.Format(time.RFC3339))

	return timeRangeModel{
		start:       start_time,
		end:         end_time,
		list_model:  list,
		start_model: start,
		end_model:   end,
		mode:        inactive,
		focus: struct {
			x uint
			y uint
		}{0, 0},
	}
}

func (m timeRangeModel) Init() tea.Cmd {
	return nil
}

func (m timeRangeModel) Update(msg tea.Msg) (timeRangeModel, tea.Cmd) {
	var cmd tea.Cmd

	switch key := msg.(type) {
	case tea.KeyMsg:
		if key.Type == tea.KeyEsc {
			m.mode = navigation
			m.blurSelected()
			return m, nil
		}

		if m.mode == navigation {
			if key.Type == tea.KeyEnter {
				m.mode = active
				m.focusSelected()
			} else {
				m.Navigate(key)
			}
		} else if m.mode == active {
			switch m.currentFocus() {
			case "list":
				m.list_model, cmd = m.list_model.Update(key)
				duration := m.list_model.SelectedItem().(timeDurationItem).duration
				m.SetEnd(time.Now())
				m.SetStart(m.end.Add(duration))
			case "start":
				m.start_model, cmd = m.start_model.Update(key)
			case "end":
				m.end_model, cmd = m.end_model.Update(key)
			}
		}
	}

	return m, cmd
}

func (m timeRangeModel) View() string {
	var input_style = lipgloss.NewStyle().
		Inherit(baseStyle).
		Border(lipgloss.RoundedBorder(), true).
		Margin(0)

	var list_style = input_style.Copy()
	var start_style = input_style.Copy()
	var end_style = input_style.Copy()

	focused := m.currentFocus()

	switch focused {
	case "list":
		list_style.BorderStyle(lipgloss.ThickBorder())
	case "start":
		start_style.BorderStyle(lipgloss.ThickBorder())
	case "end":
		end_style.BorderStyle(lipgloss.ThickBorder())
	}

	right := lipgloss.JoinVertical(lipgloss.Bottom, start_style.Render(m.start_model.View()), end_style.Render(m.end_model.View()))
	page := lipgloss.JoinHorizontal(lipgloss.Left, list_style.Render(m.list_model.View()), right)

	return page
}
