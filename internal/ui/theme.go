package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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
	// The three bands crema paints behind whole rows: what you typed, and the
	// lines a change added or removed. A tint reads as a band at a glance,
	// which coloured text alone does not — and a diff is the one thing worth
	// finding without reading.
	UserBg lipgloss.Color
	AddBg  lipgloss.Color
	DelBg  lipgloss.Color
	// Track is the unfilled part of a status-bar gauge: a shade off Surface,
	// so the bar reads as a bar even at 0%.
	Track lipgloss.Color
}

// base is the starting point for every style in the package: it paints the
// theme background so no cell is left showing the terminal's own.
func base() lipgloss.Style { return lipgloss.NewStyle().Background(T.Bg) }

// fg is base plus a foreground color.
func fg(c lipgloss.Color) lipgloss.Style { return base().Foreground(c) }

// addLine and delLine are how a changed line looks wherever it appears — in
// the conversation when an agent edits a file, and in the diff pane. One
// definition, so the two panes cannot drift apart.
func addLine() lipgloss.Style {
	return lipgloss.NewStyle().Background(T.AddBg).Foreground(T.Green)
}

func delLine() lipgloss.Style {
	return lipgloss.NewStyle().Background(T.DelBg).Foreground(T.Red)
}

// pane is a rounded box in theme colors, border included.
func pane(border lipgloss.Color) lipgloss.Style {
	return base().Border(lipgloss.RoundedBorder()).
		BorderForeground(border).BorderBackground(T.Bg)
}

// keepBG re-establishes the theme background after every ANSI reset inside s.
// Needed for output crema doesn't render itself — bubbles' textarea nests
// styles, and lipgloss cannot restore a parent background once a nested style
// resets, so everything after the reset falls back to the terminal's own
// colors. That is what left a bar of terminal background in the input box.
func keepBG(s string) string {
	open := bgOpen()
	if open == "" { // no-color profile: nothing to restore
		return s
	}
	return strings.ReplaceAll(s, ansiReset, ansiReset+open) + ansiReset
}

const ansiReset = "\x1b[0m"

// bgOpen is the escape sequence that switches the theme background on, read
// back from lipgloss so it always matches the active profile and palette.
func bgOpen() string {
	const marker = "\x00"
	r := base().Render(marker)
	if i := strings.Index(r, marker); i > 0 {
		return r[:i]
	}
	return ""
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
	UserBg:  lipgloss.Color("#2b2733"),
	AddBg:   lipgloss.Color("#16341f"),
	DelBg:   lipgloss.Color("#3b1a23"),
	Track:   lipgloss.Color("#3b3049"),
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
	UserBg:  lipgloss.Color("#eceaf2"),
	AddBg:   lipgloss.Color("#d8f5e0"),
	DelBg:   lipgloss.Color("#fbdde3"),
	Track:   lipgloss.Color("#cdc0dd"),
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
