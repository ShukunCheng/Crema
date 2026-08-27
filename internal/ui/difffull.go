package ui

import (
	"fmt"
	"strings"

	"github.com/ShukunCheng/Crema/internal/gitdiff"
	"github.com/charmbracelet/lipgloss"
)

// Full screen is a different job from the column beside the conversation. In
// the column you are glancing at what changed; with the whole frame you are
// reading one file, and the others are a list you move through. So full screen
// is a browser: the files down the left, the one you picked filling the rest.
const (
	// listW is the file column. Wide enough for a nested path, narrow enough
	// to leave the diff the room that made you go full screen.
	listW = 34
	// browserMinW is the width below which two columns plus a readable diff
	// stop fitting, and the pane falls back to the stacked view.
	browserMinW = 60
)

// browsing reports whether the pane is drawing the file browser: full screen,
// and wide enough for it.
func (d *DiffPanel) browsing() bool { return d.full && d.width >= browserMinW }

// listed is the files in the order the list shows them, which is the order the
// sections are drawn in: staged, then unstaged, then untracked.
func listed(ds gitdiff.DiffSet) []gitdiff.File {
	var out []gitdiff.File
	for _, pick := range []func(gitdiff.File) bool{
		func(f gitdiff.File) bool { return f.Staged },
		func(f gitdiff.File) bool { return !f.Staged && f.Status != "untracked" },
		func(f gitdiff.File) bool { return f.Status == "untracked" },
	} {
		for _, f := range ds.Files {
			if pick(f) {
				out = append(out, f)
			}
		}
	}
	return out
}

// Selected is the file the browser is showing, or a zero File when the diff is
// empty.
func (d *DiffPanel) Selected() (gitdiff.File, bool) {
	files := listed(d.ds)
	if len(files) == 0 {
		return gitdiff.File{}, false
	}
	return files[min(max(d.pick, 0), len(files)-1)], true
}

// SelectFile moves the browser's selection, wrapping at both ends so a long
// list can be walked in either direction without stopping.
func (d *DiffPanel) SelectFile(delta int) {
	n := len(listed(d.ds))
	if n == 0 {
		return
	}
	d.pick = ((d.pick+delta)%n + n) % n
	d.render()
	d.vp.GotoTop() // a different file starts at its own top, not the last one's
}

// SelectFileAt picks the file drawn on a row of the list, reporting false for
// a row that is a section heading or empty space.
func (d *DiffPanel) SelectFileAt(row int) bool {
	i, ok := d.listRowFile(row)
	if !ok || i == d.pick {
		return ok
	}
	d.pick = i
	d.render()
	d.vp.GotoTop()
	return true
}

// listRows draws the file column: section headings, then a row per file with
// what it gained and lost. The selected row is picked out the way a highlighted
// row is everywhere else in crema.
func (d *DiffPanel) listRows() []string {
	files := listed(d.ds)
	var rows []string
	d.listFile = d.listFile[:0]

	section := ""
	for i, f := range files {
		if s := sectionOf(f); s != section {
			section = s
			rows = append(rows, fg(T.Magenta).Bold(true).Width(listW).Render(clip("── "+s, listW)))
			d.listFile = append(d.listFile, -1)
		}
		name := clip(f.Path, listW-12)
		adds, dels := fmt.Sprintf("+%d", f.Additions), fmt.Sprintf("−%d", f.Deletions)
		gap := listW - 2 - lipgloss.Width(name) - lipgloss.Width(adds) - lipgloss.Width(dels) - 1

		// The selected row is a band, so every piece of it carries that
		// background rather than the pane's.
		bg, fgName := base(), fg(T.Lilac)
		if i == d.pick {
			bg = lipgloss.NewStyle().Background(T.UserBg)
			fgName = bg.Foreground(T.Fg).Bold(true)
		}
		rows = append(rows, paintedRow(listW,
			bg.Render(" "), fgName.Render(name), bg.Render(strings.Repeat(" ", max(gap, 1))),
			bg.Foreground(T.Green).Render(adds), bg.Render(" "),
			bg.Foreground(T.Red).Render(dels), bg.Render(" "),
		))
		d.listFile = append(d.listFile, i)
	}
	if len(rows) == 0 {
		rows = append(rows, fg(T.Muted).Width(listW).Render(clip(" working tree clean", listW)))
		d.listFile = append(d.listFile, -1)
	}
	return rows
}

func sectionOf(f gitdiff.File) string {
	switch {
	case f.Staged:
		return "STAGED"
	case f.Status == "untracked":
		return "UNTRACKED"
	}
	return "UNSTAGED"
}

// listRowFile maps a row of the file column to its file, or false for a
// heading.
func (d *DiffPanel) listRowFile(row int) (int, bool) {
	if row < 0 || row >= len(d.listFile) || d.listFile[row] < 0 {
		return 0, false
	}
	return d.listFile[row], true
}

// browserView puts the list beside the diff, both exactly h rows tall so the
// pane keeps its shape however short either one is.
func (d *DiffPanel) browserView(h int) string {
	list := d.listRows()
	for len(list) < h {
		list = append(list, base().Width(listW).Render(""))
	}
	list = list[:h]

	div := make([]string, h)
	for i := range div {
		div[i] = fg(T.Muted).Render(splitDivider)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		strings.Join(list, "\n"), strings.Join(div, "\n"), d.vp.View())
}

// The diff's own size used to be a chip in the status bar, three lines away
// from the pane it resized. It belongs on the pane: its header carries what
// the diff is and the three sizes it can take, with the one it is at picked
// out.
var diffModes = []struct {
	view  DiffView
	label string
}{
	{DiffSide, "side"},
	{DiffFull, "full"},
	{DiffHidden, "off"},
}

// modeButtons is the width of the whole set, which is what the header reserves
// on the right.
func modeButtons() int {
	n := 0
	for _, m := range diffModes {
		n += lipgloss.Width(modeText(m.label))
	}
	return n
}

func modeText(label string) string { return " " + label + " " }

// headerRow is the pane's own title bar: what it holds on the left, the three
// sizes on the right.
func (d *DiffPanel) headerRow(w int) string {
	if w <= 0 {
		return ""
	}
	// Pieces are rendered one at a time rather than nested: a style inside a
	// style resets the background for everything after it, which is what used
	// to leave unpainted cells across a row.
	var b strings.Builder
	title := " changes"
	b.WriteString(fg(T.Lilac).Render(title))
	if a, r := d.ds.Additions, d.ds.Deletions; a > 0 || r > 0 {
		counts := "  " + itoa(a) + " added " + itoa(r) + " removed"
		title += counts
		b.WriteString(base().Render("  "))
		b.WriteString(fg(T.Green).Render(itoa(a) + " added"))
		b.WriteString(base().Render(" "))
		b.WriteString(fg(T.Red).Render(itoa(r) + " removed"))
	}
	pad := w - lipgloss.Width(title) - modeButtons()
	if pad < 1 {
		return base().Width(w).Render(clip(title, w))
	}
	b.WriteString(base().Render(strings.Repeat(" ", pad)))
	for _, m := range diffModes {
		style := base().Foreground(T.Muted)
		if m.view == d.view {
			style = lipgloss.NewStyle().Background(T.Purple).Foreground(T.Surface).Bold(true)
		}
		b.WriteString(style.Render(modeText(m.label)))
	}
	return b.String()
}

// HeaderModeAt maps a column of the header to the size it selects.
func (d *DiffPanel) HeaderModeAt(x int) (DiffView, bool) {
	col := d.width - modeButtons()
	for _, m := range diffModes {
		next := col + lipgloss.Width(modeText(m.label))
		if x >= col && x < next {
			return m.view, true
		}
		col = next
	}
	return 0, false
}

// InFileList reports whether a content column falls in the browser's file
// column, which is what decides whether a click picks a file or lands in the
// diff.
func (d *DiffPanel) InFileList(col int) bool { return d.browsing() && col >= 0 && col < listW }

// BodyCol turns a column of the pane's content into a column of the diff
// itself, which is offset by the file list when the browser is up.
func (d *DiffPanel) BodyCol(col int) int {
	if d.browsing() {
		return col - listW - 1
	}
	return col
}

// Browsing reports whether the file browser is up, which is what decides
// whether the arrow keys walk the list or scroll the diff.
func (d *DiffPanel) Browsing() bool { return d.browsing() }

// browserBody is the diff of the file the list has picked. The list already
// says which file and what it gained, so the body drops the section and file
// headers the stacked view needs and gets on with the hunks.
func (d *DiffPanel) browserBody(w int) []diffRow {
	f, ok := d.Selected()
	if !ok {
		return []diffRow{{text: fg(T.Muted).Width(w).Render(clip("working tree clean", w))}}
	}
	if d.pick >= len(listed(d.ds)) {
		d.pick = 0
	}
	if w < 2*minSideW+1 {
		return renderDiffFile(f, w, false)[1:] // too narrow to pair the sides
	}
	rows := []diffRow{{text: splitHeaderRow(w)}}
	return append(rows, renderSplitFile(f, w, false)[1:]...)
}
