package ui

import (
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

// RenderDiffSet draws a whole diff with every file open. The pane folds files
// by default and opens the ones you click; this is the plain rendering of the
// lot, with nothing hidden.
func RenderDiffSet(ds gitdiff.DiffSet, w int) string {
	return joinRows(renderDiffRows(ds, w, allOpen(ds)))
}

// allOpen marks every file in ds as opened.
func allOpen(ds gitdiff.DiffSet) map[string]bool {
	open := make(map[string]bool, len(ds.Files))
	for _, f := range ds.Files {
		open[DiffFileKey(f)] = true
	}
	return open
}

func joinRows(rows []diffRow) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.text)
		b.WriteByte('\n')
	}
	return b.String()
}

// renderDiffRows draws the unified view: one column, +/- prefixes, the shape
// a pull request shows. It is what the pane uses, where a split would leave
// two columns too narrow to read.
func renderDiffRows(ds gitdiff.DiffSet, w int, open map[string]bool) []diffRow {
	return renderRows(ds, w, open, renderDiffFile)
}

func renderDiffFile(f gitdiff.File, w int, folded bool) []diffRow {
	key := DiffFileKey(f)
	rows := diffFileHeader(f, w, folded)
	bodyRow := func(s string) diffRow { return diffRow{text: s, file: key} }
	if f.Note != "" {
		rows = append(rows, bodyRow(fg(T.Yellow).Width(w).Render(clip("  "+f.Note, w))))
	}
	if folded {
		return rows // the ▸ on the header says there is more behind it
	}

	add := addLine().Width(w)
	del := delLine().Width(w)
	ctx := fg(T.Muted).Width(w)
	hdr := fg(T.Purple).Width(w)
	for _, h := range f.Hunks {
		rows = append(rows, bodyRow(hdr.Render(clip(h.Header, w))))
		for _, ln := range h.Lines {
			switch ln.Kind {
			case gitdiff.LineAdd:
				rows = append(rows, bodyRow(add.Render(clip("+"+ln.Text, w))))
			case gitdiff.LineDel:
				rows = append(rows, bodyRow(del.Render(clip("-"+ln.Text, w))))
			default:
				rows = append(rows, bodyRow(ctx.Render(clip(" "+ln.Text, w))))
			}
		}
	}
	return rows
}

func statusGlyph(status string) string {
	switch status {
	case "added":
		return "+"
	case "deleted":
		return "×"
	case "renamed":
		return "→"
	case "untracked":
		return "?"
	default:
		return "●"
	}
}

type DiffPanel struct {
	ds     gitdiff.DiffSet
	vp     viewport.Model
	width  int
	height int // the pane's own height; the viewport gives up a row to search
	rows   []diffRow
	search diffSearch
	// open holds the files the user has clicked open. Files arrive folded, so
	// the pane is a list of what changed and by how much, and a file's diff is
	// something you ask for — the conversation already shows the edits as the
	// agent makes them.
	open map[string]bool
	// searchOpened is the subset of open that the search opened itself, so a
	// changed query can close them again.
	searchOpened map[string]bool
	// full is the whole-frame shape: a file browser rather than one long
	// stacked diff. pick is which file it is showing, and listFile maps a row
	// of the file column back to one.
	full     bool
	pick     int
	listFile []int
	// view is which of the three sizes the diff is at, so the buttons in its
	// own header can show that and offer the others.
	view DiffView
	// The diff is as worth copying as the conversation is — a path, a hunk
	// header, the line an agent just changed — so it selects the same way.
	sel selection
}

func NewDiffPanel(w, h int) *DiffPanel {
	d := &DiffPanel{
		vp: viewport.New(max(1, w), max(1, h)), open: map[string]bool{},
	}
	d.vp.Style = base() // paint the theme behind the empty rows too
	d.SetSize(w, h)     // one place decides what the header and box leave over
	return d
}

func (d *DiffPanel) SetSize(w, h int) {
	d.width, d.height = max(1, w), max(1, h)
	d.vp.Width, d.vp.Height = d.bodyWidth(), max(1, d.height-1-d.searchRows())
	d.render()
}

// bodyWidth is how much the diff itself gets: everything, unless the file
// browser is taking a column.
func (d *DiffPanel) bodyWidth() int {
	if d.browsing() {
		return max(1, d.width-listW-1)
	}
	return d.width
}

// searchRows is the height the search box takes off the viewport.
func (d *DiffPanel) searchRows() int {
	if d.search.open {
		return 1
	}
	return 0
}

// SetDiff replaces the content, keeping the scroll offset when possible so a
// background refresh doesn't jump the pane under the user. Whatever the user
// opened stays open across refreshes, since the key survives.
func (d *DiffPanel) SetDiff(ds gitdiff.DiffSet) {
	d.ds = ds
	if d.search.open {
		d.applySearch(false) // a file the agent just touched may now match
		return
	}
	d.render()
}

// Invalidate re-renders the current diff, e.g. after a theme change.
func (d *DiffPanel) Invalidate() { d.render() }

// SetView tells the pane how much room it has, which is what decides its
// shape: a column beside the conversation is one stacked unified diff, and the
// whole frame is a file browser with the one you picked beside the list.
func (d *DiffPanel) SetView(v DiffView) {
	if d.view == v {
		return
	}
	d.view, d.full = v, v == DiffFull
	d.SetSize(d.width, d.height) // the body's width changes with the shape
}

func (d *DiffPanel) render() {
	d.vp.Style = base()
	off := d.vp.YOffset
	w := d.bodyWidth()
	switch {
	case d.browsing():
		d.rows = d.browserBody(w)
	case d.full:
		d.rows = renderSplitRows(d.ds, w, d.open)
	default:
		d.rows = renderDiffRows(d.ds, w, d.open)
	}
	// Anything that changes the rows moves the matches, so they are found
	// again here rather than at each of the places that can do it.
	if d.search.open {
		d.search.hits = findHits(d.contentLines(), d.search.query)
		if d.search.idx >= len(d.search.hits) {
			d.search.idx = 0
		}
	}
	d.sync()
	d.vp.SetYOffset(off)
}

// sync pushes the rows into the viewport with the search matches and any
// selection painted over them, in that order: a selection is something you are
// doing now, so it wins the cells it covers.
func (d *DiffPanel) sync() {
	if !d.sel.hasRange() && len(d.search.hits) == 0 {
		d.vp.SetContent(joinRows(d.rows))
		return
	}
	lines := paintHits(d.contentLines(), d.search.hits, d.search.idx)
	if d.sel.hasRange() {
		lines = d.sel.paint(lines)
	}
	d.vp.SetContent(strings.Join(lines, "\n") + "\n")
}

func (d *DiffPanel) contentLines() []string {
	lines := make([]string, len(d.rows))
	for i, r := range d.rows {
		lines[i] = r.text
	}
	return lines
}

// BeginSelect starts a drag at a content line and column.
func (d *DiffPanel) BeginSelect(line, col int) {
	d.sel.begin(line, col)
	d.sync()
}

// ExtendSelect moves the loose end of an in-progress drag.
func (d *DiffPanel) ExtendSelect(line, col int) {
	if d.sel.extend(line, col) {
		d.sync()
	}
}

// EndSelect finishes a drag and reports the selected text, empty when the drag
// never left its starting cell.
func (d *DiffPanel) EndSelect() string {
	d.sel.end()
	d.sync()
	return d.SelectedText()
}

// ClearSelection drops any highlight.
func (d *DiffPanel) ClearSelection() {
	if d.sel.clear() {
		d.sync()
	}
}

func (d *DiffPanel) HasSelection() bool { return d.sel.done() }

// SelectedText is the plain text under the highlight.
func (d *DiffPanel) SelectedText() string { return d.sel.text(d.contentLines()) }

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

// ToggleCollapse opens a folded file or folds an open one.
func (d *DiffPanel) ToggleCollapse(key string) {
	if key == "" {
		return
	}
	d.open[key] = !d.open[key]
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

func (d *DiffPanel) View() string {
	body := d.vp.View()
	if d.browsing() {
		body = d.browserView(d.vp.Height)
	}
	out := d.headerRow(d.width) + "\n" + body
	if d.search.open {
		out += "\n" + d.searchBar(d.width)
	}
	return out
}

// paintedRow joins already-styled pieces into a row exactly w wide, padding
// with the theme background. Pieces are rendered separately and never nested:
// a style inside a style resets the background for everything after it, which
// is how a row ends up with unpainted gaps in it.
func paintedRow(w int, pieces ...string) string {
	used := 0
	var b strings.Builder
	for _, p := range pieces {
		pw := lipgloss.Width(p)
		if used+pw > w {
			b.WriteString(clip(p, max(0, w-used)))
			used = w
			break
		}
		b.WriteString(p)
		used += pw
	}
	if pad := w - used; pad > 0 {
		b.WriteString(base().Render(strings.Repeat(" ", pad)))
	}
	return b.String()
}
