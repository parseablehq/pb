package main

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

var (
	styleBold = lipgloss.NewStyle().Bold(true)
)

func listingTableStyle() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		Border(lipgloss.NormalBorder(), false).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	return s
}
