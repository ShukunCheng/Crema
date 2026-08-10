package ui

import "github.com/charmbracelet/lipgloss"

// Theme is the pink/purple dark palette. M2 will make this swappable.
type Theme struct {
	Pink, Magenta, Purple, Lilac, Muted, Fg lipgloss.Color
	Green, Red, Yellow                      lipgloss.Color
	Surface                                 lipgloss.Color
}

// T is the active theme. `max` used throughout this package is the Go builtin.
var T = Theme{
	Pink:    lipgloss.Color("#f5a9d0"),
	Magenta: lipgloss.Color("#e06bb8"),
	Purple:  lipgloss.Color("#b47cf0"),
	Lilac:   lipgloss.Color("#d9c7f0"),
	Muted:   lipgloss.Color("#8b7fa0"),
	Fg:      lipgloss.Color("#efe6f7"),
	Green:   lipgloss.Color("#8fe0a8"),
	Red:     lipgloss.Color("#ff8f9e"),
	Yellow:  lipgloss.Color("#ffd9a0"),
	Surface: lipgloss.Color("#211a2b"),
}
