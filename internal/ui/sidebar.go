package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// SidebarWidth is the sidebar's total width including its border. Two columns
// of it belong to the × that closes an agent, and it was widened by that much
// when the × arrived rather than take the room out of the names.
const SidebarWidth = 26

// NewAgentRow is the label of the row that opens the new-agent picker.
const NewAgentRow = "+ new agent"

// Row layout, shared by RenderSidebar and SidebarRowAt so a click always lands
// on whatever is actually drawn there:
//
//	0            "AGENTS"
//	1..n         one row per session
//	n+1          blank
//	n+2          "+ new agent"
const (
	sidebarTitleRows = 1
	sidebarGapRows   = 1
)

type SidebarTarget int

const (
	SidebarNone SidebarTarget = iota
	SidebarSession
	SidebarNewAgent
)

// closeWidth is the room the × takes at the right of an agent's row: the
// glyph and the space before it.
const closeWidth = 2

// SidebarCloseCol is the first column of that ×, counted from the left of the
// sidebar's content area. A click at or past it closes the agent instead of
// selecting it.
func SidebarCloseCol(w int) int { return w - closeWidth }

// SidebarRowOf is the inverse of SidebarRowAt for session rows.
func SidebarRowOf(sessionIndex int) int { return sidebarTitleRows + sessionIndex }

// SidebarRowAt maps a row inside the sidebar's content area to what is drawn
// there. The index is meaningful only for SidebarSession.
func SidebarRowAt(sessionCount, row int) (SidebarTarget, int) {
	if row < sidebarTitleRows {
		return SidebarNone, 0
	}
	if i := row - sidebarTitleRows; i < sessionCount {
		return SidebarSession, i
	}
	if row == sidebarTitleRows+sessionCount+sidebarGapRows {
		return SidebarNewAgent, 0
	}
	return SidebarNone, 0
}

// RenderSidebar lists every open agent with its live state. w and h are the
// content area (inside the border); the result is exactly h lines of w
// columns. dragging is the row being carried by the mouse, noDrag for none:
// it is painted as a lifted block — the user band behind it, a grab mark in
// the margin — so the row in hand reads as in hand while the rest of the
// list reflows around the pointer.
func RenderSidebar(sessions []*Session, active, dragging int, spin string, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	title := fg(T.Magenta).Bold(true).Width(w)
	sel := fg(T.Pink).Bold(true).Width(w)
	idle := fg(T.Lilac).Width(w)
	run := fg(T.Green).Width(w)
	dim := fg(T.Muted).Width(w)
	lift := lipgloss.NewStyle().Background(T.UserBg).Foreground(T.Pink).Bold(true)
	liftDim := lipgloss.NewStyle().Background(T.UserBg).Foreground(T.Muted)

	lines := []string{title.Render(clip("AGENTS", w))}
	for i, s := range sessions {
		marker := "  "
		if i == active {
			marker = "▸ "
		}
		if i == dragging {
			marker = "▌ " // the grab mark: this row is in hand
		}
		state := "idle"
		st := idle
		if s.busy {
			state = fmt.Sprintf("%s %.0fs", spin, s.Elapsed().Seconds())
			st = run
		}
		// Work handed to a subagent is still this agent's work, and the
		// sidebar is the only place every agent is visible at once: without
		// this, an agent whose subagent is grinding away looks the same as
		// one thinking by itself.
		if n := s.RunningTasks(); n > 0 {
			state += fmt.Sprintf(" +%d", n)
			st = run
		}
		// index prefix doubles as the alt+N jump hint
		head := fmt.Sprintf("%s%d %s", marker, i+1, s.Title())
		// The × is drawn separately so it can keep its own colour, which
		// leaves the rest of the row this much narrower.
		body := SidebarCloseCol(w)
		room := body - lipgloss.Width(state) - 1
		if room < 1 {
			room = 1
		}
		head = clip(head, room)
		pad := body - lipgloss.Width(head) - lipgloss.Width(state)
		if pad < 1 {
			pad = 1
		}
		row := clip(head+strings.Repeat(" ", pad)+state, body)
		style, closer := st, dim
		if i == active {
			style = sel
		}
		if i == dragging {
			style, closer = lift, liftDim
		}
		lines = append(lines, style.Width(body).Render(row)+closer.Width(closeWidth).Render(" ×"))
	}
	lines = append(lines, "")
	lines = append(lines, dim.Render(clip(NewAgentRow+"  ^n", w)))

	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines[:h], "\n")
}
