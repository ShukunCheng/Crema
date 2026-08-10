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

func RenderDiffSet(ds gitdiff.DiffSet, w int) string {
	if w <= 0 {
		return ""
	}
	var b strings.Builder
	dim := lipgloss.NewStyle().Foreground(T.Muted)
	if ds.Err != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(T.Yellow).Render(clip(ds.Err, w)) + "\n")
		return b.String()
	}
	if len(ds.Files) == 0 {
		b.WriteString(dim.Render(clip("working tree clean", w)) + "\n")
		return b.String()
	}
	sections := []struct {
		title string
		pick  func(gitdiff.File) bool
	}{
		{"STAGED", func(f gitdiff.File) bool { return f.Staged }},
		{"UNSTAGED", func(f gitdiff.File) bool { return !f.Staged && f.Status != "untracked" }},
		{"UNTRACKED", func(f gitdiff.File) bool { return f.Status == "untracked" }},
	}
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
		b.WriteString(lipgloss.NewStyle().Foreground(T.Magenta).Bold(true).
			Render(clip("── "+sec.title+" ", w)) + "\n")
		for _, f := range files {
			b.WriteString(renderDiffFile(f, w))
		}
	}
	return b.String()
}

func renderDiffFile(f gitdiff.File, w int) string {
	var b strings.Builder
	name := f.Path
	if f.Status == "renamed" && f.OldPath != "" {
		name = f.OldPath + " → " + f.Path
	}
	head := fmt.Sprintf("%s %s  +%d −%d", statusGlyph(f.Status), name, f.Additions, f.Deletions)
	b.WriteString(lipgloss.NewStyle().Foreground(T.Pink).Bold(true).Render(clip(head, w)) + "\n")
	if f.Note != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(T.Yellow).Render(clip("  "+f.Note, w)) + "\n")
	}
	add := lipgloss.NewStyle().Foreground(T.Green)
	del := lipgloss.NewStyle().Foreground(T.Red)
	ctx := lipgloss.NewStyle().Foreground(T.Muted)
	hdr := lipgloss.NewStyle().Foreground(T.Purple)
	for _, h := range f.Hunks {
		b.WriteString(hdr.Render(clip(h.Header, w)) + "\n")
		for _, ln := range h.Lines {
			switch ln.Kind {
			case gitdiff.LineAdd:
				b.WriteString(add.Render(clip("+"+ln.Text, w)) + "\n")
			case gitdiff.LineDel:
				b.WriteString(del.Render(clip("-"+ln.Text, w)) + "\n")
			default:
				b.WriteString(ctx.Render(clip(" "+ln.Text, w)) + "\n")
			}
		}
	}
	return b.String()
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
	ds    gitdiff.DiffSet
	vp    viewport.Model
	width int
}

func NewDiffPanel(w, h int) *DiffPanel {
	return &DiffPanel{vp: viewport.New(max(1, w), max(1, h)), width: max(1, w)}
}

func (d *DiffPanel) SetSize(w, h int) {
	d.width = max(1, w)
	d.vp.Width, d.vp.Height = max(1, w), max(1, h)
	d.vp.SetContent(RenderDiffSet(d.ds, d.width))
}

// SetDiff replaces the content, keeping the scroll offset when possible so a
// background refresh doesn't jump the pane under the user.
func (d *DiffPanel) SetDiff(ds gitdiff.DiffSet) {
	off := d.vp.YOffset
	d.ds = ds
	d.vp.SetContent(RenderDiffSet(ds, d.width))
	d.vp.SetYOffset(off)
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
