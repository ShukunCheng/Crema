package ui

import (
	"strings"

	"github.com/ShukunCheng/Crema/internal/gitdiff"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// A diff is the one pane you arrive at knowing what you are looking for — the
// function you renamed, the file you meant to leave alone. Scrolling a
// thousand-line diff to find it is the wrong tool.
//
// Search works on what is rendered, so it finds a path in a header as readily
// as a line in a hunk, and it opens the files it finds things in: a match
// hidden behind a folded file would otherwise be a search that lies.
type diffSearch struct {
	open  bool // the box is up and taking keys
	query string
	hits  []searchHit
	idx   int
}

// searchHit is one match, in the pane's own content coordinates: which line,
// and which display columns of it.
type searchHit struct{ line, start, end int }

// findHits locates every match of q across the rendered lines.
func findHits(lines []string, q string) []searchHit {
	if q == "" {
		return nil
	}
	var out []searchHit
	for i, ln := range lines {
		plain := ansi.Strip(ln)
		hay, needle := strings.ToLower(plain), strings.ToLower(q)
		if len(hay) != len(plain) || len(needle) != len(q) {
			// Lowercasing changed the byte length, so offsets into hay no
			// longer point into plain. Rare enough (a dotted capital I, say)
			// to be worth a case-sensitive answer rather than a wrong one.
			hay, needle = plain, q
		}
		for off := 0; off < len(hay); {
			j := strings.Index(hay[off:], needle)
			if j < 0 {
				break
			}
			at := off + j
			start := lipgloss.Width(plain[:at])
			out = append(out, searchHit{
				line: i, start: start, end: start + lipgloss.Width(plain[at:at+len(needle)]),
			})
			off = at + len(needle)
		}
	}
	return out
}

// paintHits draws the matches over the rendered lines, the current one picked
// out from the rest. Like the selection, a match is stripped of its own colour
// first: highlighted text has to be readable against the highlight.
func paintHits(lines []string, hits []searchHit, cur int) []string {
	if len(hits) == 0 {
		return lines
	}
	other := lipgloss.NewStyle().Foreground(T.Bg).Background(T.Yellow)
	here := lipgloss.NewStyle().Foreground(T.Bg).Background(T.Pink).Bold(true)

	byLine := map[int][]int{} // line → indexes into hits
	for i, h := range hits {
		byLine[h.line] = append(byLine[h.line], i)
	}
	for line, idxs := range byLine {
		if line < 0 || line >= len(lines) {
			continue
		}
		ln := lines[line]
		w := lipgloss.Width(ln)
		var b strings.Builder
		at := 0
		for _, i := range idxs {
			h := hits[i]
			if h.start >= w {
				break
			}
			style := other
			if i == cur {
				style = here
			}
			b.WriteString(ansi.Cut(ln, at, h.start))
			b.WriteString(style.Render(ansi.Strip(ansi.Cut(ln, h.start, min(h.end, w)))))
			at = min(h.end, w)
		}
		b.WriteString(ansi.Cut(ln, at, w))
		lines[line] = b.String()
	}
	return lines
}

// fileHasText reports whether q appears anywhere in a file's diff — its path,
// its note, a hunk header, or a line of it. Only what the pane actually draws
// counts: opening a file whose rendered text has no match would be a file
// opened for nothing.
func fileHasText(f gitdiff.File, q string) bool {
	has := func(s string) bool { return strings.Contains(strings.ToLower(s), strings.ToLower(q)) }
	if has(f.Path) || has(f.Note) {
		return true
	}
	for _, h := range f.Hunks {
		if has(h.Header) {
			return true
		}
		for _, ln := range h.Lines {
			if has(ln.Text) {
				return true
			}
		}
	}
	return false
}

// Searching reports whether the box is up, which is how the app knows to hand
// it the keystrokes.
func (d *DiffPanel) Searching() bool { return d.search.open }

// StartSearch opens the box, keeping whatever was searched for last — the same
// query twice is the common case.
func (d *DiffPanel) StartSearch() {
	if d.search.open {
		return
	}
	d.search.open = true
	d.SetSize(d.width, d.height) // the box takes a row from the viewport
	d.applySearch(true)
}

// EndSearch closes the box and drops the highlight. The files it opened stay
// open — finding them is what was asked for — so they stop being the search's
// to close.
func (d *DiffPanel) EndSearch() {
	if !d.search.open {
		return
	}
	d.search = diffSearch{query: d.search.query} // remember the query, forget the rest
	d.searchOpened = nil
	d.SetSize(d.width, d.height)
	d.sync()
}

// SetQuery re-runs the search. Files containing the text are opened first, so
// what is counted is everything in the diff rather than everything on screen.
func (d *DiffPanel) SetQuery(q string) {
	d.search.query = q
	d.applySearch(true)
}

// applySearch opens the files the text is in and re-renders, which is what
// finds the matches. jump moves to the first of them.
//
// Files it opened last keystroke are closed again first. Typing "new" passes
// through "n", which matches nearly everything; without this the pane would be
// left holding every file the prefixes happened to hit.
func (d *DiffPanel) applySearch(jump bool) {
	// In the browser only one file is on screen, so finding text in another
	// one means going to it — otherwise the search would answer "nothing here"
	// about a diff it can see perfectly well.
	if d.browsing() && d.search.query != "" {
		for i, f := range listed(d.ds) {
			if fileHasText(f, d.search.query) {
				if i != d.pick {
					d.pick = i
					d.vp.GotoTop()
				}
				break
			}
		}
	}
	for key := range d.searchOpened {
		delete(d.open, key)
	}
	d.searchOpened = map[string]bool{}
	for _, f := range d.ds.Files {
		if key := DiffFileKey(f); d.search.query != "" && !d.open[key] &&
			fileHasText(f, d.search.query) {
			d.open[key], d.searchOpened[key] = true, true
		}
	}
	d.render()
	if jump {
		d.search.idx = 0
		d.scrollToHit()
		d.sync()
	}
}

// MoveMatch steps through the matches, wrapping at both ends — a search that
// stops at the last match makes you retype it to get back to the first.
func (d *DiffPanel) MoveMatch(delta int) {
	if n := len(d.search.hits); n > 0 {
		d.search.idx = ((d.search.idx+delta)%n + n) % n
	}
	d.scrollToHit()
	d.sync()
}

// scrollToHit brings the current match into view, a third of the way down, so
// there is context above it and room to read below.
func (d *DiffPanel) scrollToHit() {
	if len(d.search.hits) == 0 {
		return
	}
	line := d.search.hits[d.search.idx].line
	d.vp.SetYOffset(max(0, line-d.vp.Height/3))
}

// SearchKey handles a keystroke while the box is up. It reports false for a key
// the box has no use for, which the pane then treats as scrolling.
func (d *DiffPanel) SearchKey(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "esc":
		d.EndSearch()
	case "enter", "down":
		d.MoveMatch(1)
	case "shift+tab", "up":
		d.MoveMatch(-1)
	case "backspace", "ctrl+h":
		if q := []rune(d.search.query); len(q) > 0 {
			d.SetQuery(string(q[:len(q)-1]))
		}
	case "ctrl+u":
		d.SetQuery("")
	case "ctrl+w", "ctrl+backspace":
		d.SetQuery(dropWord(d.search.query))
	default:
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			d.SetQuery(d.search.query + string(msg.Runes))
			return true
		}
		if msg.Type == tea.KeySpace {
			d.SetQuery(d.search.query + " ")
			return true
		}
		return false
	}
	return true
}

// dropWord removes the last word of a query.
func dropWord(s string) string {
	s = strings.TrimRight(s, " ")
	if i := strings.LastIndexAny(s, " \t/\\."); i >= 0 {
		return s[:i+1]
	}
	return ""
}

// searchBar is the one row under the diff: what is being looked for, how many
// there are, and how to move.
func (d *DiffPanel) searchBar(w int) string {
	if w <= 0 {
		return ""
	}
	left := fg(T.Muted).Render("find ") + fg(T.Fg).Render(d.search.query) +
		fg(T.Pink).Render("▌")

	count := fg(T.Muted).Render("type to search")
	switch n := len(d.search.hits); {
	case d.search.query == "":
	case n == 0:
		count = fg(T.Red).Render("no matches")
	default:
		count = fg(T.Yellow).Render(itoa(d.search.idx+1) + "/" + itoa(n))
	}
	right := count + fg(T.Muted).Render("  ↑↓ move · esc close")

	pad := w - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		return clip(left+" "+right, w)
	}
	return left + base().Render(strings.Repeat(" ", pad)) + right
}

// itoa keeps the bar's assembly readable without dragging fmt in for two ints.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
