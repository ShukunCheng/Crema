package ui

import "github.com/charmbracelet/lipgloss"

// Theme is the crema palette. Colors are read from the package-level T at
// render time, so switching modes is a matter of reassigning it and asking the
// cached views to re-render.
type Theme struct {
	Pink, Magenta, Purple, Lilac, Muted, Fg lipgloss.Color
	Green, Red, Yellow                      lipgloss.Color
	Surface                                 lipgloss.Color
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
	Surface: lipgloss.Color("#211a2b"),
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
	Surface: lipgloss.Color("#ebe4f2"),
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
