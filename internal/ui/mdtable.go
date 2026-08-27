package ui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The agents write markdown, and most of it reads fine as plain text — but a
// table does not survive plain wrapping: its rows are long, the wrap breaks
// them mid-cell, and the columns shatter. So assistant prose is scanned for
// tables and each one is drawn as a table — columns sized to the pane, long
// cells wrapped inside their own column — while everything around it renders
// exactly as before.

// tableRule matches the separator row that makes a run of |-lines a table:
// dashes and optional alignment colons between pipes.
var tableRule = regexp.MustCompile(`^\|? *:?-+:? *(\| *:?-+:? *)*\|? *$`)

// proseSegment is a stretch of assistant text: either ordinary prose or the
// lines of one table.
type proseSegment struct {
	table bool
	lines []string
}

// splitTables cuts assistant prose into prose and table segments. A table is
// two or more consecutive lines starting with |, the second of which is the
// dashes-and-pipes rule. Fenced code is never a table, whatever it contains.
func splitTables(text string) []proseSegment {
	lines := strings.Split(text, "\n")
	var segs []proseSegment
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			segs = append(segs, proseSegment{lines: cur})
			cur = nil
		}
	}
	fenced := false
	for i := 0; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "```") {
			fenced = !fenced
		}
		if !fenced && strings.HasPrefix(t, "|") &&
			i+1 < len(lines) && tableRule.MatchString(strings.TrimSpace(lines[i+1])) {
			var tbl []string
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
				tbl = append(tbl, strings.TrimSpace(lines[i]))
				i++
			}
			i--
			flush()
			segs = append(segs, proseSegment{table: true, lines: tbl})
			continue
		}
		cur = append(cur, lines[i])
	}
	flush()
	return segs
}

// tableCells splits one |-row into its cells, dropping the empty ends the
// outer pipes leave behind and unescaping the \| a cell is allowed to hold.
func tableCells(line string) []string {
	line = strings.ReplaceAll(line, `\|`, "\x00")
	parts := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	out := make([]string, len(parts))
	for i, p := range parts {
		p = strings.ReplaceAll(p, "\x00", "|")
		// The markdown dressing reads as noise in a drawn table.
		p = strings.ReplaceAll(p, "**", "")
		p = strings.ReplaceAll(p, "`", "")
		out[i] = strings.TrimSpace(p)
	}
	return out
}

// drawTable renders parsed rows as an aligned table w columns wide: a bold
// header, a rule under it, and every long cell wrapped inside its own column.
// rows[0] is the header; the markdown rule line is not among them.
func drawTable(rows [][]string, w int) string {
	ncols := 0
	for _, r := range rows {
		ncols = max(ncols, len(r))
	}
	if ncols == 0 {
		return ""
	}
	widths := tableWidths(rows, ncols, w)
	if widths == nil {
		// Too narrow to be a table at all; plain wrapping is the honest fallback.
		var b strings.Builder
		for _, r := range rows {
			b.WriteString(strings.Join(r, " · ") + "\n")
		}
		return body(w).Foreground(T.Fg).Render(strings.TrimRight(b.String(), "\n")) + "\n"
	}

	bar := fg(T.Muted).Render("│")
	var out []string
	for ri, row := range rows {
		cells := make([][]string, ncols)
		depth := 1
		for c := 0; c < ncols; c++ {
			text := ""
			if c < len(row) {
				text = row[c]
			}
			st := lipgloss.NewStyle().Width(widths[c]).Foreground(T.Fg).Background(T.Bg)
			if ri == 0 {
				st = st.Bold(true).Foreground(T.Lilac)
			}
			cells[c] = strings.Split(st.Render(text), "\n")
			depth = max(depth, len(cells[c]))
		}
		for line := 0; line < depth; line++ {
			var b strings.Builder
			b.WriteString(bar)
			for c := 0; c < ncols; c++ {
				piece := strings.Repeat(" ", widths[c])
				if line < len(cells[c]) {
					piece = cells[c][line]
				}
				b.WriteString(base().Render(" ") + piece + base().Render(" "))
				b.WriteString(bar)
			}
			out = append(out, base().Width(max(1, w)).Render(b.String()))
		}
		if ri == 0 {
			var b strings.Builder
			b.WriteString(bar)
			for c := 0; c < ncols; c++ {
				b.WriteString(fg(T.Muted).Render(strings.Repeat("─", widths[c]+2)))
				b.WriteString(bar)
			}
			out = append(out, base().Width(max(1, w)).Render(b.String()))
		}
	}
	return strings.Join(out, "\n") + "\n"
}

// tableWidths shares w between the columns: what fits naturally keeps its
// size, and the long columns split what is left in proportion to how much
// they have to say. nil means the pane cannot hold this many columns.
func tableWidths(rows [][]string, ncols, w int) []int {
	const minCol = 4
	avail := w - (ncols + 1) - 2*ncols // the bars, and a space each side of every cell
	if avail < ncols*minCol {
		return nil
	}
	natural := make([]int, ncols)
	for _, r := range rows {
		for c, cell := range r {
			natural[c] = max(natural[c], lipgloss.Width(cell))
		}
	}
	total := 0
	for _, n := range natural {
		total += max(n, 1)
	}
	if total <= avail {
		for c := range natural {
			natural[c] = max(natural[c], 1)
		}
		return natural
	}
	// Short columns keep their natural width; the rest is divided among the
	// long ones in proportion to their content.
	widths := make([]int, ncols)
	left, longTotal := avail, 0
	for c, n := range natural {
		if share := avail * max(n, 1) / total; n <= max(share, minCol) {
			widths[c] = max(n, 1)
			left -= widths[c]
		} else {
			longTotal += n
		}
	}
	for c, n := range natural {
		if widths[c] == 0 {
			cw := left * n / longTotal
			widths[c] = max(cw, minCol)
		}
	}
	// Rounding drift lands on the widest column, up or down.
	sum, widest := 0, 0
	for c, cw := range widths {
		sum += cw
		if cw > widths[widest] {
			widest = c
		}
	}
	widths[widest] += avail - sum
	if widths[widest] < minCol {
		return nil
	}
	return widths
}
