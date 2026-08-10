package ui

import (
	"fmt"
	"strings"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/charmbracelet/lipgloss"
)

// MaxOutputLines caps a single tool output. Anything beyond is dropped with a
// visible, counted label — crema never folds output silently.
const MaxOutputLines = 4000

func body(w int) lipgloss.Style {
	return lipgloss.NewStyle().Width(max(1, w))
}

// rail prefixes every line with a colored vertical guide.
func rail(s string, c lipgloss.Color) string {
	bar := lipgloss.NewStyle().Foreground(c).Render("│")
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, ln := range lines {
		lines[i] = bar + " " + ln
	}
	return strings.Join(lines, "\n")
}

// Truncate returns the first maxLines lines and how many were dropped.
func Truncate(s string, maxLines int) (string, int) {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= maxLines {
		return strings.TrimRight(s, "\n"), 0
	}
	return strings.Join(lines[:maxLines], "\n"), len(lines) - maxLines
}

func RenderUser(text string, w int) string {
	head := lipgloss.NewStyle().Foreground(T.Pink).Bold(true).Render("❯ you")
	return head + "\n" + body(w).Foreground(T.Fg).Render(text) + "\n"
}

func RenderAssistant(text string, w int) string {
	return body(w).Foreground(T.Fg).Render(text) + "\n"
}

func RenderThinking(text string, w int) string {
	head := lipgloss.NewStyle().Foreground(T.Muted).Italic(true).Render("✳ thinking")
	txt := body(max(1, w-2)).Foreground(T.Muted).Italic(true).Render(text)
	return head + "\n" + rail(txt, T.Muted) + "\n"
}

func RenderTool(name, input string, w int) string {
	head := lipgloss.NewStyle().Foreground(T.Purple).Bold(true).Render("⏵ " + name)
	if strings.TrimSpace(input) == "" {
		return head + "\n"
	}
	txt := body(max(1, w-2)).Foreground(T.Lilac).Render(input)
	return head + "\n" + rail(txt, T.Purple) + "\n"
}

func RenderToolOutput(content string, isErr bool, w int) string {
	fg, railC := T.Muted, T.Purple
	if isErr {
		fg, railC = T.Red, T.Red
	}
	shown, cut := Truncate(content, MaxOutputLines)
	if strings.TrimSpace(shown) == "" && cut == 0 {
		return rail(lipgloss.NewStyle().Foreground(T.Muted).Render("(no output)"), railC) + "\n"
	}
	txt := body(max(1, w-2)).Foreground(fg).Render(shown)
	if cut > 0 {
		label := fmt.Sprintf("… +%d lines truncated (crema cap %d)", cut, MaxOutputLines)
		txt += "\n" + lipgloss.NewStyle().Foreground(T.Yellow).Bold(true).Render(label)
	}
	return rail(txt, railC) + "\n"
}

func RenderError(text string, w int) string {
	head := lipgloss.NewStyle().Foreground(T.Red).Bold(true).Render("✖ error")
	txt := body(max(1, w-2)).Foreground(T.Red).Render(text)
	return head + "\n" + rail(txt, T.Red) + "\n"
}

func RenderSystem(text string, w int) string {
	return body(w).Foreground(T.Muted).Italic(true).Render("· "+text) + "\n"
}

func RenderStats(r *agent.TurnResult, w int) string {
	if r == nil {
		return ""
	}
	segs := []string{fmt.Sprintf("%.1fs", float64(r.DurationMS)/1000)}
	if r.CostUSD > 0 {
		segs = append(segs, fmt.Sprintf("$%.4f", r.CostUSD))
	}
	if r.InputTokens > 0 || r.OutputTokens > 0 {
		segs = append(segs, fmt.Sprintf("%d↑ %d↓ tok", r.InputTokens, r.OutputTokens))
	}
	mark := "✔"
	c := T.Green
	switch {
	case r.Canceled:
		mark, c = "⨯ canceled", T.Yellow
	case r.Err != "":
		mark, c = "✖ failed", T.Red
	}
	line := mark + " " + strings.Join(segs, " · ")
	return body(w).Foreground(c).Render(line) + "\n"
}
