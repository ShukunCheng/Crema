package ui

import "github.com/charmbracelet/lipgloss"

// Theme is the crema palette. Colors are read from the package-level T at
// render time, so switching modes is a matter of reassigning it and asking the
// cached views to re-render.
type Theme struct {
	Pink, Magenta, Purple, Lilac, Muted, Fg lipgloss.Color
	Green, Red, Yellow                      lipgloss.Color
	// Bg is painted behind every cell. Without it the terminal's own
	// background shows through and switching modes only recolors text.
	Bg      lipgloss.Color
	Surface lipgloss.Color // status bar, set slightly off Bg
}

// base is the starting point for every style in the package: it paints the
// theme background so no cell is left showing the terminal's own.
func base() lipgloss.Style { return lipgloss.NewStyle().Background(T.Bg) }

// fg is base plus a foreground color.
func fg(c lipgloss.Color) lipgloss.Style { return base().Foreground(c) }

// pane is a rounded box in theme colors, border included.
func pane(border lipgloss.Color) lipgloss.Style {
	return base().Border(lipgloss.RoundedBorder()).
		BorderForeground(border).BorderBackground(T.Bg)
}

type Mode int

const (
	ModeDark Mode = iota
	ModeLight
)

func (m Mode) String() string {
	if m == ModeLight {
		return "light"
	}
	return "dark"
}

// DarkTheme is the original pink/purple night palette.
var DarkTheme = Theme{
	Pink:    lipgloss.Color("#f5a9d0"),
	Magenta: lipgloss.Color("#e06bb8"),
	Purple:  lipgloss.Color("#b47cf0"),
	Lilac:   lipgloss.Color("#d9c7f0"),
	Muted:   lipgloss.Color("#8b7fa0"),
	Fg:      lipgloss.Color("#efe6f7"),
	Green:   lipgloss.Color("#8fe0a8"),
	Red:     lipgloss.Color("#ff8f9e"),
	Yellow:  lipgloss.Color("#ffd9a0"),
	Bg:      lipgloss.Color("#17121f"),
	Surface: lipgloss.Color("#2a2136"),
}

// LightTheme keeps the same hues but darkens them enough to stay legible on a
// pale terminal background.
var LightTheme = Theme{
	Pink:    lipgloss.Color("#b3186b"),
	Magenta: lipgloss.Color("#8f1568"),
	Purple:  lipgloss.Color("#6135b0"),
	Lilac:   lipgloss.Color("#4a3c60"),
	Muted:   lipgloss.Color("#6d6480"),
	Fg:      lipgloss.Color("#241d2e"),
	Green:   lipgloss.Color("#1a6c39"),
	Red:     lipgloss.Color("#b02b40"),
	Yellow:  lipgloss.Color("#7a5400"),
	Bg:      lipgloss.Color("#faf7fd"),
	Surface: lipgloss.Color("#e7dff0"),
}

// T is the active palette.
var T = DarkTheme

var mode = ModeDark

func CurrentMode() Mode { return mode }

func SetMode(m Mode) {
	mode = m
	if m == ModeLight {
		T = LightTheme
		return
	}
	T = DarkTheme
}

// ToggleMode flips between light and dark and returns the new mode. Callers
// must re-render anything that cached styled text.
func ToggleMode() Mode {
	if mode == ModeLight {
		SetMode(ModeDark)
	} else {
		SetMode(ModeLight)
	}
	return mode
}

// DetectMode asks the terminal whether its background is dark. Falls back to
// dark, which is the safer guess for a terminal that won't answer.
func DetectMode() Mode {
	if lipgloss.HasDarkBackground() {
		return ModeDark
	}
	return ModeLight
}
