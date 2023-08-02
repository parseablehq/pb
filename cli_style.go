package main

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

// styling for cli outputs
var (
	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "4", Dark: "11"}).Bold(true)
	styleBold = lipgloss.NewStyle().Bold(true)
)

func listingTableStyle() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.Border(lipgloss.NormalBorder(), false, false, true, false)
	s.Selected = selectedStyle
	return s
}
