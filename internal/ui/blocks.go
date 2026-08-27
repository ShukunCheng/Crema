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

// body wraps text to w and pads every line to w, so the theme background
// covers the full row rather than stopping at the end of the text.
func body(w int) lipgloss.Style {
	return base().Width(max(1, w))
}

// head is a one-line, full-width heading.
func head(text string, w int, c lipgloss.Color) string {
	return fg(c).Bold(true).Width(max(1, w)).Render(text)
}

// rail prefixes every line with a colored vertical guide.
func rail(s string, c lipgloss.Color) string {
	bar := fg(c).Render("│ ")
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, ln := range lines {
		lines[i] = bar + ln
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

// RenderUser draws what you asked for on a band of its own, so a long
// conversation can be skimmed by looking for where you last spoke.
func RenderUser(text string, w int) string {
	band := lipgloss.NewStyle().Background(T.UserBg).Width(max(1, w))
	return band.Foreground(T.Pink).Bold(true).Render("❯ you") + "\n" +
		band.Foreground(T.Fg).Render(text) + "\n"
}

// renderPending draws a message that is waiting for the running turn to end.
// It sits on the same grey band as one you have already sent, because that is
// what it is about to be — dimmed, and marked, so it is never mistaken for
// something the agent has already been told. One line each: the queue is a
// list of what is coming, not a second transcript.
func renderPending(text string, w int) string {
	band := lipgloss.NewStyle().Background(T.UserBg).Width(max(1, w))
	return band.Foreground(T.Muted).Render(clip("❯ "+oneLine(text)+"  · waiting", max(1, w))) + "\n"
}

func RenderAssistant(text string, w int) string {
	var b strings.Builder
	for _, seg := range splitTables(text) {
		if seg.table {
			rows := make([][]string, 0, len(seg.lines))
			for i, ln := range seg.lines {
				if i == 1 { // the dashes-and-pipes rule; the drawn one replaces it
					continue
				}
				rows = append(rows, tableCells(ln))
			}
			b.WriteString(drawTable(rows, w))
			continue
		}
		b.WriteString(body(w).Foreground(T.Fg).Render(strings.Join(seg.lines, "\n")) + "\n")
	}
	return b.String()
}

func RenderThinking(text string, w int) string {
	txt := body(max(1, w-2)).Foreground(T.Muted).Italic(true).Render(text)
	return fg(T.Muted).Italic(true).Width(max(1, w)).Render("▾ thinking") + "\n" +
		rail(txt, T.Muted) + "\n"
}

func RenderTool(name, input string, w int) string {
	h := head("▾ "+name, w, T.Purple)
	if strings.TrimSpace(input) == "" {
		return h + "\n"
	}
	// An edit is shown as the change it makes, not as the arguments that
	// describe it. Anything else prints as it came.
	if diff := renderEdit(name, input, max(1, w-2)); diff != "" {
		return h + "\n" + rail(diff, T.Purple) + "\n"
	}
	txt := body(max(1, w-2)).Foreground(T.Lilac).Render(input)
	return h + "\n" + rail(txt, T.Purple) + "\n"
}

func RenderToolOutput(content string, isErr bool, w int) string {
	fgc, railC := T.Muted, T.Purple
	if isErr {
		fgc, railC = T.Red, T.Red
	}
	shown, cut := Truncate(content, MaxOutputLines)
	if strings.TrimSpace(shown) == "" && cut == 0 {
		return rail(body(max(1, w-2)).Foreground(T.Muted).Render("(no output)"), railC) + "\n"
	}
	txt := body(max(1, w-2)).Foreground(fgc).Render(shown)
	if cut > 0 {
		label := fmt.Sprintf("… +%d lines truncated (crema cap %d)", cut, MaxOutputLines)
		txt += "\n" + body(max(1, w-2)).Foreground(T.Yellow).Bold(true).Render(label)
	}
	return rail(txt, railC) + "\n"
}

func RenderError(text string, w int) string {
	txt := body(max(1, w-2)).Foreground(T.Red).Render(text)
	return head("▾ error", w, T.Red) + "\n" + rail(txt, T.Red) + "\n"
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
	mark := "✓"
	c := T.Green
	switch {
	case r.Canceled:
		mark, c = "⨯ canceled", T.Yellow
	case r.Err != "":
		mark, c = "× failed", T.Red
	}
	return body(w).Foreground(c).Render(mark+" "+strings.Join(segs, " · ")) + "\n"
}
