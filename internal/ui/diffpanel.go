package ui

import (
	"fmt"
	"strings"

	"github.com/ShukunCheng/Crema/internal/gitdiff"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// clip hard-cuts a line to w display columns, marking the cut with "›" so a
// clipped line is never mistaken for the whole line. Width-aware, so CJK and
// emoji in paths don't overflow the pane.
func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = strings.ReplaceAll(s, "\t", "    ")
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "›")
}

// diffRow is one rendered line plus what it belongs to, so a click can be
// mapped back to a file without re-parsing the styled text.
type diffRow struct {
	text   string
	file   string // collapse key; "" for section headers and notices
	header bool   // the file's own header line — the click target
}

// DiffFileKey identifies a file within a DiffSet. A path can appear twice, once
// staged and once not, so the flag is part of the key.
func DiffFileKey(f gitdiff.File) string {
	if f.Staged {
		return "staged:" + f.Path
	}
	return "work:" + f.Path
}

func RenderDiffSet(ds gitdiff.DiffSet, w int) string {
	return joinRows(renderDiffRows(ds, w, nil))
}

func joinRows(rows []diffRow) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.text)
		b.WriteByte('\n')
	}
	return b.String()
}

func renderDiffRows(ds gitdiff.DiffSet, w int, collapsed map[string]bool) []diffRow {
	if w <= 0 {
		return nil
	}
	dim := lipgloss.NewStyle().Foreground(T.Muted)
	if ds.Err != "" {
		return []diffRow{{text: lipgloss.NewStyle().Foreground(T.Yellow).Render(clip(ds.Err, w))}}
	}
	if len(ds.Files) == 0 {
		return []diffRow{{text: dim.Render(clip("working tree clean", w))}}
	}
	sections := []struct {
		title string
		pick  func(gitdiff.File) bool
	}{
		{"STAGED", func(f gitdiff.File) bool { return f.Staged }},
		{"UNSTAGED", func(f gitdiff.File) bool { return !f.Staged && f.Status != "untracked" }},
		{"UNTRACKED", func(f gitdiff.File) bool { return f.Status == "untracked" }},
	}
	var rows []diffRow
	for _, sec := range sections {
		var files []gitdiff.File
		for _, f := range ds.Files {
			if sec.pick(f) {
				files = append(files, f)
			}
		}
		if len(files) == 0 {
			continue
		}
		rows = append(rows, diffRow{text: lipgloss.NewStyle().Foreground(T.Magenta).Bold(true).
			Render(clip("── "+sec.title+" ", w))})
		for _, f := range files {
			rows = append(rows, renderDiffFile(f, w, collapsed[DiffFileKey(f)])...)
		}
	}
	return rows
}

func renderDiffFile(f gitdiff.File, w int, folded bool) []diffRow {
	key := DiffFileKey(f)
	name := f.Path
	if f.Status == "renamed" && f.OldPath != "" {
		name = f.OldPath + " → " + f.Path
	}
	marker := "▾"
	if folded {
		marker = "▸"
	}
	head := fmt.Sprintf("%s %s %s  +%d −%d", marker, statusGlyph(f.Status), name, f.Additions, f.Deletions)
	rows := []diffRow{{
		text:   lipgloss.NewStyle().Foreground(T.Pink).Bold(true).Render(clip(head, w)),
		file:   key,
		header: true,
	}}

	body := func(s string) diffRow { return diffRow{text: s, file: key} }
	if f.Note != "" {
		rows = append(rows, body(lipgloss.NewStyle().Foreground(T.Yellow).Render(clip("  "+f.Note, w))))
	}
	if folded {
		hidden := 0
		for _, h := range f.Hunks {
			hidden += 1 + len(h.Lines)
		}
		if hidden > 0 {
			rows = append(rows, body(lipgloss.NewStyle().Foreground(T.Yellow).
				Render(clip(fmt.Sprintf("  %d lines hidden, click to expand", hidden), w))))
		}
		return rows
	}

	add := lipgloss.NewStyle().Foreground(T.Green)
	del := lipgloss.NewStyle().Foreground(T.Red)
	ctx := lipgloss.NewStyle().Foreground(T.Muted)
	hdr := lipgloss.NewStyle().Foreground(T.Purple)
	for _, h := range f.Hunks {
		rows = append(rows, body(hdr.Render(clip(h.Header, w))))
		for _, ln := range h.Lines {
			switch ln.Kind {
			case gitdiff.LineAdd:
				rows = append(rows, body(add.Render(clip("+"+ln.Text, w))))
			case gitdiff.LineDel:
				rows = append(rows, body(del.Render(clip("-"+ln.Text, w))))
			default:
				rows = append(rows, body(ctx.Render(clip(" "+ln.Text, w))))
			}
		}
	}
	return rows
}

func statusGlyph(status string) string {
	switch status {
	case "added":
		return "✚"
	case "deleted":
		return "✖"
	case "renamed":
		return "➜"
	case "untracked":
		return "?"
	default:
		return "●"
	}
}

type DiffPanel struct {
	ds        gitdiff.DiffSet
	vp        viewport.Model
	width     int
	rows      []diffRow
	collapsed map[string]bool // file key → folded by the user
}

func NewDiffPanel(w, h int) *DiffPanel {
	d := &DiffPanel{
		vp: viewport.New(max(1, w), max(1, h)), width: max(1, w),
		collapsed: map[string]bool{},
	}
	d.render()
	return d
}

func (d *DiffPanel) SetSize(w, h int) {
	d.width = max(1, w)
	d.vp.Width, d.vp.Height = max(1, w), max(1, h)
	d.render()
}

// SetDiff replaces the content, keeping the scroll offset when possible so a
// background refresh doesn't jump the pane under the user. Folded files stay
// folded across refreshes, since the key survives.
func (d *DiffPanel) SetDiff(ds gitdiff.DiffSet) {
	d.ds = ds
	d.render()
}

// Invalidate re-renders the current diff, e.g. after a theme change.
func (d *DiffPanel) Invalidate() { d.render() }

func (d *DiffPanel) render() {
	off := d.vp.YOffset
	d.rows = renderDiffRows(d.ds, d.width, d.collapsed)
	d.vp.SetContent(joinRows(d.rows))
	d.vp.SetYOffset(off)
}

// YOffset is the first content line currently visible, for hit-testing.
func (d *DiffPanel) YOffset() int { return d.vp.YOffset }

// HeaderFileAt returns the collapse key of the file whose header sits on
// contentLine, or "" when that line is not a file header.
func (d *DiffPanel) HeaderFileAt(contentLine int) string {
	if contentLine < 0 || contentLine >= len(d.rows) {
		return ""
	}
	if r := d.rows[contentLine]; r.header {
		return r.file
	}
	return ""
}

// ToggleCollapse folds or unfolds one file.
func (d *DiffPanel) ToggleCollapse(key string) {
	if key == "" {
		return
	}
	d.collapsed[key] = !d.collapsed[key]
	d.render()
}

func (d *DiffPanel) Update(msg tea.Msg) tea.Cmd {
	// The viewport's keymap has no home/end bindings, so handle them here.
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "home":
			d.vp.GotoTop()
			return nil
		case "end":
			d.vp.GotoBottom()
			return nil
		}
	}
	var cmd tea.Cmd
	d.vp, cmd = d.vp.Update(msg)
	return cmd
}

func (d *DiffPanel) View() string { return d.vp.View() }
