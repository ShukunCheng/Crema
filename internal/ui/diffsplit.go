package ui

import (
	"fmt"
	"strings"

	"github.com/ShukunCheng/Crema/internal/gitdiff"
	"github.com/charmbracelet/lipgloss"
)

// The split view is the full-screen shape of the diff: old on the left, new on
// the right, lines paired up so a change reads across the divider. The pane
// version stays unified, which is the right shape for a narrow column.

const (
	splitDivider = "│"
	// gutterW is the line-number column, wide enough for a five-digit file.
	gutterW = 6
	// minSideW is the point below which two columns stop being readable and
	// the split view gives up in favour of the unified one.
	minSideW = 24
)

// splitRow is one screen line of the split view: at most one line from each
// side, already paired.
type splitRow struct {
	oldNum, newNum int    // the line's number, 0 when the diff didn't say
	oldText        string //
	newText        string
	hasOld, hasNew bool // whether that side has a line here at all
	changed        bool // this row is part of a change, not shared context
}

// pairHunk turns a hunk's sequence of -/+/context lines into rows that line up
// across the two sides. Runs of deletions and additions are matched off
// against each other, which is what makes an edited line show its before and
// after on the same row; whatever is left over pairs with filler.
func pairHunk(h gitdiff.Hunk) []splitRow {
	var rows []splitRow
	oldNo, newNo := h.OldStart, h.NewStart
	var dels, adds []string

	flush := func() {
		for i := 0; i < max(len(dels), len(adds)); i++ {
			var r splitRow
			r.changed = true
			if i < len(dels) {
				r.oldNum, r.oldText, r.hasOld = oldNo, dels[i], true
				oldNo++
			}
			if i < len(adds) {
				r.newNum, r.newText, r.hasNew = newNo, adds[i], true
				newNo++
			}
			rows = append(rows, r)
		}
		dels, adds = nil, nil
	}

	for _, ln := range h.Lines {
		switch ln.Kind {
		case gitdiff.LineDel:
			dels = append(dels, ln.Text)
		case gitdiff.LineAdd:
			adds = append(adds, ln.Text)
		default:
			flush()
			rows = append(rows, splitRow{
				oldNum: oldNo, oldText: ln.Text, hasOld: true,
				newNum: newNo, newText: ln.Text, hasNew: true,
			})
			oldNo, newNo = oldNo+1, newNo+1
		}
	}
	flush()
	return rows
}

// renderSplitRows is renderDiffRows' side-by-side twin. It keeps the same
// section and file headers — those are full width in both views — and only
// changes how a hunk's body is drawn.
func renderSplitRows(ds gitdiff.DiffSet, w int, open map[string]bool) []diffRow {
	if w < 2*minSideW+1 {
		return renderDiffRows(ds, w, open) // too narrow for two columns
	}
	rows := renderRows(ds, w, open, renderSplitFile)
	if len(rows) == 0 {
		return rows
	}
	return append([]diffRow{{text: splitHeaderRow(w)}}, rows...)
}

func renderSplitFile(f gitdiff.File, w int, folded bool) []diffRow {
	rows := diffFileHeader(f, w, folded)
	if folded {
		return rows
	}
	key := DiffFileKey(f)
	left := (w - lipgloss.Width(splitDivider)) / 2
	right := w - left - lipgloss.Width(splitDivider)
	div := fg(T.Muted).Render(splitDivider)

	if f.Note != "" {
		rows = append(rows, diffRow{
			text: fg(T.Yellow).Width(w).Render(clip("  "+f.Note, w)), file: key,
		})
	}
	for _, h := range f.Hunks {
		rows = append(rows, diffRow{
			text: fg(T.Purple).Width(w).Render(clip(h.Header, w)), file: key,
		})
		for _, r := range pairHunk(h) {
			rows = append(rows, diffRow{
				text: splitSide(r.hasOld, r.oldNum, r.oldText, left, r.changed, gitdiff.LineDel) +
					div +
					splitSide(r.hasNew, r.newNum, r.newText, right, r.changed, gitdiff.LineAdd),
				file: key,
			})
		}
	}
	return rows
}

// splitSide renders one half of a split row: a line-number gutter and the
// text. A changed line is tinted across the whole half, gutter included, so
// the change reads as a band rather than as coloured words. A side with no
// line on this row is drawn as an empty band of its own, which is what makes
// an insertion visibly an insertion rather than a shifted line.
func splitSide(has bool, num int, text string, w int, changed bool, kind gitdiff.LineKind) string {
	if w <= 0 {
		return ""
	}
	if !has {
		return base().Background(T.Surface).Width(w).Render("")
	}
	line := fg(T.Muted)
	if changed {
		line = delLine()
		if kind == gitdiff.LineAdd {
			line = addLine()
		}
	}
	number := strings.Repeat(" ", gutterW-1) // a diff that gave no line numbers
	if num > 0 {
		number = fmt.Sprintf("%*d", gutterW-1, num)
	}
	gutter := line.Foreground(T.Muted).Render(number + " ")
	return gutter + line.Width(max(0, w-gutterW)).Render(clip(text, max(0, w-gutterW)))
}

// renderRows walks the diff's three sections and hands each file to fileRows,
// which is what differs between the unified and split views.
func renderRows(ds gitdiff.DiffSet, w int, open map[string]bool,
	fileRows func(gitdiff.File, int, bool) []diffRow) []diffRow {
	if w <= 0 {
		return nil
	}
	if ds.Err != "" {
		return []diffRow{{text: fg(T.Yellow).Width(w).Render(clip(ds.Err, w))}}
	}
	if len(ds.Files) == 0 {
		return []diffRow{{text: fg(T.Muted).Width(w).Render(clip("working tree clean", w))}}
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
		rows = append(rows, diffRow{text: fg(T.Magenta).Bold(true).Width(w).
			Render(clip("── "+sec.title+" ", w))})
		for _, f := range files {
			rows = append(rows, fileRows(f, w, !open[DiffFileKey(f)])...)
		}
	}
	return rows
}

// diffFileHeader is the clickable line both views share.
func diffFileHeader(f gitdiff.File, w int, folded bool) []diffRow {
	name := f.Path
	if f.Status == "renamed" && f.OldPath != "" {
		name = f.OldPath + " → " + f.Path
	}
	marker := "▾"
	if folded {
		marker = "▸"
	}
	head := fmt.Sprintf("%s %s %s", marker, statusGlyph(f.Status), name)
	return []diffRow{{
		text: paintedRow(w,
			fg(T.Pink).Bold(true).Render(clip(head, w)),
			base().Render("  "),
			fg(T.Green).Render(fmt.Sprintf("+%d", f.Additions)),
			base().Render(" "),
			fg(T.Red).Render(fmt.Sprintf("−%d", f.Deletions)),
		),
		file:   DiffFileKey(f),
		header: true,
	}}
}

// splitHeaderRow labels the two columns, so it is clear which side is which.
func splitHeaderRow(w int) string {
	if w < 2*minSideW+1 {
		return ""
	}
	left := (w - lipgloss.Width(splitDivider)) / 2
	right := w - left - lipgloss.Width(splitDivider)
	sty := fg(T.Muted).Bold(true)
	return sty.Width(left).Render(clip("  before", left)) +
		fg(T.Muted).Render(splitDivider) +
		sty.Width(right).Render(clip("  after", right))
}
