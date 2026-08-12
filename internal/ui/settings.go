package ui

import (
	"strings"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// Header line counts, shared by View and RowAt so a click lands on the row it
// appears to: title, blank, "PERMISSIONS".
const settingsHeaderRows = 3

type settingsKind int

const (
	settingsNone settingsKind = iota
	settingsPermission
	settingsModel
)

// settingsRow is one selectable line in the modal.
type settingsRow struct {
	kind  settingsKind
	perm  agent.PermissionMode
	model string
	label string
	desc  string
	on    bool // currently active
}

// Settings is the per-agent settings modal: permission mode and model. Only
// modes and models the focused backend actually supports are listed.
type Settings struct {
	title string
	rows  []settingsRow
	idx   int
}

func NewSettings(s *Session) *Settings {
	st := &Settings{title: "settings — " + s.Title()}
	for _, m := range s.Backend.Modes() {
		st.rows = append(st.rows, settingsRow{
			kind: settingsPermission, perm: m,
			label: m.Label(), desc: m.Describe(), on: m == s.Permission,
		})
	}
	for _, m := range s.Backend.Models() {
		label, desc := m, ""
		if m == agent.DefaultModel {
			label, desc = "default", "whatever the CLI is configured to use"
		}
		st.rows = append(st.rows, settingsRow{
			kind: settingsModel, model: m, label: label, desc: desc, on: m == s.Model,
		})
	}
	for i, r := range st.rows { // start on the active permission
		if r.on && r.kind == settingsPermission {
			st.idx = i
			break
		}
	}
	return st
}

// firstModelRow is the index of the first model row, or len(rows) when there
// are none — used to place the MODEL heading.
func (s *Settings) firstModelRow() int {
	for i, r := range s.rows {
		if r.kind == settingsModel {
			return i
		}
	}
	return len(s.rows)
}

// Update handles a key. done reports that a setting was chosen (the caller
// applies it and may leave the modal open); canceled closes it.
func (s *Settings) Update(msg tea.KeyMsg) (chosen *settingsRow, canceled bool) {
	switch msg.String() {
	case "esc", "ctrl+c", "ctrl+p":
		return nil, true
	case "up", "k":
		if s.idx > 0 {
			s.idx--
		}
	case "down", "j":
		if s.idx < len(s.rows)-1 {
			s.idx++
		}
	case "enter", " ":
		return s.choose(s.idx), false
	}
	return nil, false
}

// ClickRow selects and activates a row, exactly as enter would.
func (s *Settings) ClickRow(row int) (chosen *settingsRow, canceled bool) {
	if i, ok := s.RowAt(row); ok {
		s.idx = i
		return s.choose(i), false
	}
	return nil, false
}

func (s *Settings) choose(i int) *settingsRow {
	if i < 0 || i >= len(s.rows) {
		return nil
	}
	pick := s.rows[i]
	// reflect the new state so the modal can stay open
	for j := range s.rows {
		if s.rows[j].kind == pick.kind {
			s.rows[j].on = j == i
		}
	}
	return &pick
}

// RowAt maps a content row of the modal to a settings row index.
func (s *Settings) RowAt(row int) (int, bool) {
	i := row - settingsHeaderRows
	if i < 0 {
		return 0, false
	}
	// a blank line and the MODEL heading sit between the two groups
	if firstModel := s.firstModelRow(); i >= firstModel {
		i -= 2
		if i < firstModel {
			return 0, false // the heading rows themselves
		}
	}
	if i >= len(s.rows) {
		return 0, false
	}
	return i, true
}

// View renders the modal: exactly h lines tall, at most w columns wide.
func (s *Settings) View(w, h int) string {
	if w < 12 || h < 5 {
		return strings.Repeat("\n", max(0, h-1))
	}
	inner := w - 4
	content := h - 2
	headSty := fg(T.Pink).Bold(true).Width(inner)
	groupSty := fg(T.Magenta).Bold(true).Width(inner)
	selSty := fg(T.Pink).Bold(true).Width(inner)
	dim := fg(T.Muted).Width(inner)

	lines := []string{headSty.Render(clip(s.title, inner)), "", groupSty.Render(clip("PERMISSIONS", inner))}
	firstModel := s.firstModelRow()
	for i, r := range s.rows {
		if i == firstModel {
			lines = append(lines, "", groupSty.Render(clip("MODEL", inner)))
		}
		mark := "  "
		if r.on {
			mark = "● "
		}
		text := mark + r.label
		if r.desc != "" {
			text += " — " + r.desc
		}
		if i == s.idx {
			lines = append(lines, selSty.Render(clip("▸ "+strings.TrimPrefix(text, "  "), inner)))
		} else {
			lines = append(lines, base().Width(inner).Render(clip(text, inner)))
		}
	}
	footer := []string{"", dim.Render(clip("↑↓ move · enter apply · esc close", inner))}
	lines = append(lines, footer...)

	if len(lines) > content { // cramped box: drop middle rows, keep the help
		keep := max(0, content-len(footer))
		lines = append(lines[:keep], footer...)
		if len(lines) > content {
			lines = lines[:content]
		}
	}
	for len(lines) < content {
		lines = append(lines, "")
	}
	return pane(T.Purple).Padding(0, 1).
		Width(w - 2).Height(content).
		Render(strings.Join(lines, "\n"))
}
