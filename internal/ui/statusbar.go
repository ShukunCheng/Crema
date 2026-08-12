package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/charmbracelet/lipgloss"
)

// themeToggleWidth is the fixed width of the chip at the right edge of the
// status bar, so hit-testing is exact no matter what the rest of the bar says.
const themeToggleWidth = 9

type StatusData struct {
	Agent, Mode, Dir, Spin, Note string
	Model                        string
	Busy                         bool
	ElapsedSec                   float64
	Cost                         float64
	Adds, Dels                   int
	// ContextTokens/ContextWindow drive the "ctx" segment; Limit is the
	// backend's usage window (Claude Code's rolling 5-hour / 7-day allowance).
	ContextTokens, ContextWindow int64
	Limit                        *agent.RateLimit
}

// contextSegment reports how full the model's context window is.
func contextSegment(used, window int64) string {
	if used <= 0 || window <= 0 {
		return ""
	}
	return fmt.Sprintf("ctx %d%%", int(float64(used)*100/float64(window)))
}

// limitSegment reports the backend's usage window and when it resets, e.g.
// "5h 97% · resets in 2h14m".
func limitSegment(l *agent.RateLimit, now time.Time) string {
	if l == nil {
		return ""
	}
	s := fmt.Sprintf("%s %d%%", l.Label(), int(l.Utilization*100))
	if l.ResetsAt.IsZero() {
		return s
	}
	d := l.ResetsAt.Sub(now)
	if d <= 0 {
		return s + " · resetting"
	}
	if h := int(d.Hours()); h > 0 {
		return fmt.Sprintf("%s · resets in %dh%02dm", s, h, int(d.Minutes())%60)
	}
	return fmt.Sprintf("%s · resets in %dm", s, int(d.Minutes())+1)
}

// ThemeToggleRange is the half-open column range [start, end) of the clickable
// theme chip, or 0,0 when the bar is too narrow to show it.
func ThemeToggleRange(w int) (start, end int) {
	if w < themeToggleWidth+2 {
		return 0, 0
	}
	return w - themeToggleWidth, w
}

// chip renders a fixed-width button label, centered.
func chip(label string, width int) string {
	pad := width - 2 - len(label)
	if pad < 0 {
		pad = 0
	}
	left := pad / 2
	return "[" + strings.Repeat(" ", left) + label + strings.Repeat(" ", pad-left) + "]"
}

func themeChip() string { return chip(CurrentMode().String(), themeToggleWidth) }

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
	if s.Model != "" {
		left = append(left, s.Model)
	}
	if s.Cost > 0 {
		left = append(left, fmt.Sprintf("$%.4f", s.Cost))
	}
	left = append(left, fmt.Sprintf("+%d −%d", s.Adds, s.Dels))
	if seg := contextSegment(s.ContextTokens, s.ContextWindow); seg != "" {
		left = append(left, seg)
	}
	if seg := limitSegment(s.Limit, time.Now()); seg != "" {
		left = append(left, seg)
	}
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
