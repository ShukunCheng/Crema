package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type StatusData struct {
	Agent, Mode, Dir, Spin, Note string
	Busy                         bool
	ElapsedSec                   float64
	Cost                         float64
	Adds, Dels                   int
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
	if s.Dir != "" {
		right := s.Dir + " "
		if pad := w - lipgloss.Width(line) - lipgloss.Width(right); pad > 1 {
			line += strings.Repeat(" ", pad) + right
		}
	}
	line = clip(line, w)
	if pad := w - lipgloss.Width(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return lipgloss.NewStyle().Foreground(T.Lilac).Background(T.Surface).Render(line)
}
