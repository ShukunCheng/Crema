package ui

import (
	"strings"

	"github.com/ShukunCheng/Crema/internal/gitdiff"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// A diff line is as wide as the code is, and the pane is a column beside the
// conversation. Until now the answer was to cut every line at the pane's width
// and mark the cut with "›" — which hides exactly the part of a long line that
// is usually worth seeing, since the end of a line is where the call arguments,
// the string literal and the comment are. So the body slides sideways, with a
// bar under it saying how much of the width is on screen and where.
//
// It is the unified view that scrolls. The split view puts a divider down the
// middle at a position the pane's own width decides, and sliding the rendered
// rows under it would slide the divider off the screen with them; there both
// halves are already sized to the frame.

// maxDiffWidth bounds how wide the rows are rendered. A minified bundle or a
// base64 blob in a diff can run to hundreds of thousands of columns, and every
// row is padded to the widest one — so this is what keeps one pathological
// line out of the memory of every row on the screen.
const maxDiffWidth = 2000

// hscrollStep is how far one notch of the wheel or one arrow moves the body.
// Wide enough to get somewhere, short enough to land on what you were aiming
// at.
const hscrollStep = 8

// hscrollable reports whether the body slides sideways: the unified diff does,
// the split view and the file browser do not.
func (d *DiffPanel) hscrollable() bool { return !d.full && !d.browsing() }

// naturalWidth is how wide the rows want to be: the longest line of any file
// that is open, with the tab expansion clip would do and room for the +/-
// column. A folded file contributes only its header, which is why opening one
// can widen the pane's content.
func naturalWidth(ds gitdiff.DiffSet, open map[string]bool) int {
	w := 0
	for _, f := range ds.Files {
		if !open[DiffFileKey(f)] {
			continue
		}
		for _, h := range f.Hunks {
			if n := lipgloss.Width(strings.ReplaceAll(h.Header, "\t", "    ")); n > w {
				w = n
			}
			for _, ln := range h.Lines {
				// One column for the +/-/space that every body line carries.
				n := 1 + lipgloss.Width(strings.ReplaceAll(ln.Text, "\t", "    "))
				if n > w {
					w = n
				}
			}
		}
	}
	return min(w, maxDiffWidth)
}

// renderWidth is the width the rows are drawn at: the pane's own, or the
// content's when that is wider and the view is one that slides.
func (d *DiffPanel) renderWidth() int {
	w := d.bodyWidth()
	if !d.hscrollable() {
		return w
	}
	return max(w, naturalWidth(d.ds, d.open))
}

// maxXOff is the furthest right the body can go: far enough to bring the last
// column into view and no further, so there is never a screen of padding after
// the code.
func (d *DiffPanel) maxXOff() int {
	return max(0, d.contentW-d.bodyWidth())
}

// clampX keeps the offset inside what there is to see. Called after anything
// that changes the content or the size, since either can leave the body
// scrolled past its own end.
func (d *DiffPanel) clampX() {
	d.xoff = max(0, min(d.xoff, d.maxXOff()))
}

// ScrollX moves the body sideways by n columns and reports whether it moved,
// so a caller can tell a scroll that did something from one that hit the end.
func (d *DiffPanel) ScrollX(n int) bool {
	if !d.hscrollable() {
		return false
	}
	was := d.xoff
	d.xoff = max(0, min(d.xoff+n, d.maxXOff()))
	if d.xoff == was {
		return false
	}
	d.sync()
	return true
}

// XOff is how far the body is scrolled sideways, for hit-testing: a column on
// the screen is that many columns further into the content.
func (d *DiffPanel) XOff() int { return d.xoff }

// hbarRows is the height the horizontal bar takes off the viewport: a row,
// once there is something to scroll to.
func (d *DiffPanel) hbarRows() int {
	if d.hscrollable() && d.maxXOff() > 0 {
		return 1
	}
	return 0
}

// window slides a rendered line to the current offset. The rows are drawn at
// the content's full width, so what is on screen is a cut out of one.
func (d *DiffPanel) window(lines []string) []string {
	if d.xoff <= 0 && d.contentW <= d.bodyWidth() {
		return lines
	}
	w := d.bodyWidth()
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = ansi.Cut(ln, d.xoff, d.xoff+w)
	}
	return out
}

// hbar is the scroll bar itself: a track the width of the pane with a thumb on
// it as wide a share of the track as the screen is of the content, sitting
// where the screen sits. The columns on show are named at the right, because a
// thumb says roughly and a number says exactly — and in a diff the column is
// often the thing you are counting.
func (d *DiffPanel) hbar(w int) string {
	if w < 8 {
		return ""
	}
	label := " " + itoa(d.xoff+1) + "-" + itoa(min(d.xoff+d.bodyWidth(), d.contentW)) +
		" of " + itoa(d.contentW) + " "
	track := w - lipgloss.Width(label)
	if track < 4 {
		label, track = "", w
	}
	// The thumb is at least one column, so a very wide diff still shows where
	// it is rather than rounding down to nothing.
	thumb := max(1, track*d.bodyWidth()/max(d.contentW, 1))
	thumb = min(thumb, track)
	at := 0
	if m := d.maxXOff(); m > 0 {
		at = (track - thumb) * d.xoff / m
	}
	var b strings.Builder
	b.WriteString(fg(T.Muted).Render(strings.Repeat("─", at)))
	b.WriteString(fg(T.Purple).Render(strings.Repeat("▌", thumb)))
	b.WriteString(fg(T.Muted).Render(strings.Repeat("─", max(0, track-at-thumb))))
	if label != "" {
		b.WriteString(fg(T.Muted).Render(label))
	}
	return b.String()
}
