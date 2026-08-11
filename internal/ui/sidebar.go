package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// SidebarWidth is the sidebar's total width including its border.
const SidebarWidth = 24

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
// content area (inside the border); the result is exactly h lines of w columns.
func RenderSidebar(sessions []*Session, active int, spin string, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	title := lipgloss.NewStyle().Foreground(T.Magenta).Bold(true)
	sel := lipgloss.NewStyle().Foreground(T.Pink).Bold(true)
	idle := lipgloss.NewStyle().Foreground(T.Lilac)
	run := lipgloss.NewStyle().Foreground(T.Green)
	dim := lipgloss.NewStyle().Foreground(T.Muted)

	lines := []string{title.Render(clip("AGENTS", w))}
	for i, s := range sessions {
		marker := "  "
		if i == active {
			marker = "▸ "
		}
		state := "idle"
		st := idle
		if s.busy {
			state = fmt.Sprintf("%s %.0fs", spin, s.Elapsed().Seconds())
			st = run
		}
		// index prefix doubles as the alt+N jump hint
		head := fmt.Sprintf("%s%d %s", marker, i+1, s.Title())
		room := w - lipgloss.Width(state) - 1
		if room < 1 {
			room = 1
		}
		head = clip(head, room)
		pad := w - lipgloss.Width(head) - lipgloss.Width(state)
		if pad < 1 {
			pad = 1
		}
		row := head + strings.Repeat(" ", pad) + state
		if i == active {
			lines = append(lines, sel.Render(clip(row, w)))
		} else {
			lines = append(lines, st.Render(clip(row, w)))
		}
	}
	lines = append(lines, "")
	lines = append(lines, dim.Render(clip(NewAgentRow+"  ^n", w)))

	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines[:h], "\n")
}
