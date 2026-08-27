package ui

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/ShukunCheng/Crema/internal/agent"
)

// maxCompletionRows caps the drop-up so a project with a thousand files still
// leaves most of the conversation on screen.
const maxCompletionRows = 8

// The input box completes two things, the way the CLIs' own menus do: "/" at
// the start of the draft names a command or skill, and "@" anywhere in it
// names a file in the project. Both use the same list, the same keys, and the
// same rule — the draft is only ever what the user typed, until they take a
// name with tab.

type completionKind int

const (
	completeCommand completionKind = iota
	completeFile
)

func (k completionKind) trigger() string {
	if k == completeFile {
		return "@"
	}
	return "/"
}

// completionItem is one row: the text that gets inserted, and what to say
// about it.
type completionItem struct {
	name  string
	desc  string
	tag   string // "command", "skill", "file"
	scope string // the plugin, the project, or the containing directory
}

// Completions is the list that floats above the input while a name is being
// typed.
type Completions struct {
	kind  completionKind
	at    int // where the trigger character sits in the draft
	rows  []completionItem
	extra int // matches that didn't fit in the box
	idx   int
}

// completionTrigger reads the draft and reports what the user is naming. A
// command has to be the whole draft — that is what makes it a command rather
// than a mention of one — while a file can be any word, since "@" is how you
// point at a file in the middle of a sentence.
func completionTrigger(draft string) (kind completionKind, query string, at int, ok bool) {
	if strings.HasPrefix(draft, "/") {
		if rest := draft[1:]; !strings.ContainsAny(rest, " \t\n") {
			return completeCommand, rest, 0, true
		}
		return 0, "", 0, false
	}
	start := strings.LastIndexAny(draft, " \t\n") + 1
	if word := draft[start:]; strings.HasPrefix(word, "@") {
		return completeFile, word[1:], start, true
	}
	return 0, "", 0, false
}

// NewCompletions filters items by query, or returns nil when nothing matches —
// an empty box would just cover the conversation for no reason.
func NewCompletions(kind completionKind, at int, items []completionItem, query string) *Completions {
	m := matchItems(items, query)
	if len(m) == 0 {
		return nil
	}
	c := &Completions{kind: kind, at: at, rows: m}
	if len(m) > maxCompletionRows {
		c.rows, c.extra = m[:maxCompletionRows], len(m)-maxCompletionRows
	}
	return c
}

// commandItems and fileItems adapt the two sources onto the same row.
func commandItems(cmds []agent.Command) []completionItem {
	out := make([]completionItem, len(cmds))
	for i, c := range cmds {
		out[i] = completionItem{name: c.Name, desc: c.Desc, tag: c.Kind.String(), scope: c.Scope}
	}
	return out
}

func fileItems(files []string) []completionItem {
	out := make([]completionItem, len(files))
	for i, f := range files {
		dir := path.Dir(f)
		if dir == "." {
			dir = "the project root"
		}
		out[i] = completionItem{name: f, tag: "file", scope: dir}
	}
	return out
}

// matchItems ranks by how directly a name answers the query: one that starts
// with it, then one whose own last part does (so "query" finds
// honeycomb:query-patterns, and "app.go" finds internal/ui/app.go), then
// anything containing it.
func matchItems(items []completionItem, query string) []completionItem {
	q := strings.ToLower(query)
	type scored struct {
		item completionItem
		rank int
	}
	var hits []scored
	for _, it := range items {
		name := strings.ToLower(it.name)
		tail := name[strings.LastIndexAny(name, ":/")+1:]
		switch {
		case q == "" || strings.HasPrefix(name, q):
			hits = append(hits, scored{it, 0})
		case strings.HasPrefix(tail, q):
			hits = append(hits, scored{it, 1})
		case strings.Contains(name, q):
			hits = append(hits, scored{it, 2})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].rank < hits[j].rank })
	out := make([]completionItem, len(hits))
	for i, h := range hits {
		out[i] = h.item
	}
	return out
}

func (c *Completions) Move(delta int) {
	c.idx = min(max(c.idx+delta, 0), len(c.rows)-1)
}

// Selected is the row tab would insert.
func (c *Completions) Selected() completionItem { return c.rows[c.idx] }

// Apply is the draft with the selected name written in, ready for whatever
// comes after it.
func (c *Completions) Apply(draft string) string {
	return draft[:c.at] + c.kind.trigger() + c.Selected().name + " "
}

// Height is the rows the box occupies on screen: border, the matches, and the
// hint line.
func (c *Completions) Height() int { return len(c.rows) + 3 }

// RowAt maps a row inside the box to a match index, -1 for the border and the
// hint line.
func (c *Completions) RowAt(row int) int {
	if i := row - 1; i >= 0 && i < len(c.rows) {
		return i
	}
	return -1
}

// Click moves the cursor onto a clicked row and reports whether it was a match
// (so the caller can insert it, exactly as tab would).
func (c *Completions) Click(row int) bool {
	if i := c.RowAt(row); i >= 0 {
		c.idx = i
		return true
	}
	return false
}

// View renders the box w columns wide.
func (c *Completions) View(w int) string {
	rows := make([]string, len(c.rows))
	for i, r := range c.rows {
		rows[i] = c.kind.trigger() + r.name
		if r.desc != "" {
			rows[i] += " — " + oneLine(r.desc)
		}
	}
	return renderDropUp(rows, c.idx, c.hint(), w)
}

// renderDropUp draws the box that floats above the input: a list with one row
// picked out, and a line of hint under it. Both the completion list and the
// agent's own questions are this shape, so they are this one function.
func renderDropUp(rows []string, idx int, hint string, w int) string {
	inner := max(1, w-4)
	lines := make([]string, 0, len(rows)+1)
	for i, r := range rows {
		if i == idx {
			lines = append(lines, fg(T.Pink).Bold(true).Width(inner).Render(clip("▸ "+r, inner)))
			continue
		}
		lines = append(lines, base().Width(inner).Render(clip("  "+r, inner)))
	}
	lines = append(lines, fg(T.Muted).Width(inner).Render(clip(hint, inner)))

	return pane(T.Purple).Padding(0, 1).
		Width(max(1, w-2)).Height(len(lines)).
		Render(strings.Join(lines, "\n"))
}

// hint carries what the rows leave out: what the selected entry is and where
// it came from, plus how many matches didn't fit.
func (c *Completions) hint() string {
	sel := c.Selected()
	h := fmt.Sprintf("%s · %s", sel.tag, sel.scope)
	if c.extra > 0 {
		h += fmt.Sprintf(" · +%d more", c.extra)
	}
	return h + " · ↑↓ move · tab insert · esc dismiss"
}

// oneLine flattens the wrapped front-matter descriptions skills use into
// something that fits on a single row.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// overlayBottom draws box over the last rows of body, which is what makes the
// drop-up appear to float above the input.
func overlayBottom(body, box string) string {
	lines := strings.Split(body, "\n")
	over := strings.Split(box, "\n")
	if len(over) >= len(lines) {
		return strings.Join(over[len(over)-len(lines):], "\n") // cramped: box only
	}
	copy(lines[len(lines)-len(over):], over)
	return strings.Join(lines, "\n")
}
