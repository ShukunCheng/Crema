package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// themeToggleWidth is the fixed number of columns the theme chip occupies at
// the right edge of the status bar. Fixed, so hit-testing is exact no matter
// what the rest of the bar says.
const themeToggleWidth = 9

type StatusData struct {
	Agent, Mode, Dir, Spin, Note string
	Busy                         bool
	ElapsedSec                   float64
	Cost                         float64
	Adds, Dels                   int
}

// ThemeToggleRange is the half-open column range [start, end) of the clickable
// theme chip, or 0,0 when the bar is too narrow to show it.
func ThemeToggleRange(w int) (start, end int) {
	if w < themeToggleWidth+2 {
		return 0, 0
	}
	return w - themeToggleWidth, w
}

// themeChip is a button-looking label of exactly themeToggleWidth columns.
func themeChip() string {
	label := CurrentMode().String()
	pad := themeToggleWidth - 2 - len(label)
	if pad < 0 {
		pad = 0
	}
	left := pad / 2
	return "[" + strings.Repeat(" ", left) + label + strings.Repeat(" ", pad-left) + "]"
}

// RenderStatus returns exactly one line of exactly w columns.
func RenderStatus(s StatusData, w int) string {
	if w <= 0 {
		return ""
	}
	left := []string{}
	if s.Busy {
		left = append(left, s.Spin+" "+s.Agent, fmt.Sprintf("%.1fs", s.ElapsedSec))
	} else {
		left = append(left, "● "+s.Agent)
	}
	left = append(left, s.Mode)
	if s.Cost > 0 {
		left = append(left, fmt.Sprintf("$%.4f", s.Cost))
	}
	left = append(left, fmt.Sprintf("+%d −%d", s.Adds, s.Dels))
	if s.Note != "" {
		left = append(left, s.Note)
	}
	line := " " + strings.Join(left, " · ")

	barStyle := lipgloss.NewStyle().Foreground(T.Lilac).Background(T.Surface)
	chipStyle := lipgloss.NewStyle().Foreground(T.Surface).Background(T.Purple).Bold(true)

	start, _ := ThemeToggleRange(w)
	body := w
	if start > 0 {
		body = start // reserve the tail for the chip
	}
	if s.Dir != "" {
		right := s.Dir + " "
		if pad := body - lipgloss.Width(line) - lipgloss.Width(right); pad > 1 {
			line += strings.Repeat(" ", pad) + right
		}
	}
	line = clip(line, body)
	if pad := body - lipgloss.Width(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	if start == 0 {
		return barStyle.Render(line)
	}
	return barStyle.Render(line) + chipStyle.Render(themeChip())
}
