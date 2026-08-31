package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/charmbracelet/lipgloss"
)

// Fixed chip widths at the right edge of the status bar, so hit-testing is
// exact no matter what the rest of the bar says.
const (
	themeToggleWidth = 9
	// showDiffWidth is the chip that brings the diff back. It only exists
	// while the diff is hidden — the rest of the time its own header carries
	// the sizes, which is where a button for a pane belongs.
	showDiffWidth = 8
	// gaugeCells is how wide a bar is drawn. Ten cells means one cell is ten
	// percent, which is about all a bar is read for — the number beside it is
	// there for the rest.
	gaugeCells = 10
	// The bar is three rows: who and where, the gauges, and what the agent is
	// allowed to do. A short terminal gives up the middle one — its numbers
	// move up to the first row rather than disappearing.
	statusRowsFull  = 3
	statusRowsShort = 2
	shortTerminal   = 16
	// dirBudget is how many columns the working directory may take before it
	// is shortened to its last two elements.
	dirBudget = 28
)

// StatusRows is how many rows the status bar occupies in a terminal h rows
// tall. The layout subtracts it, and View trims the frame to match, so the two
// have to agree.
func StatusRows(h int) int {
	if h > 0 && h < shortTerminal {
		return statusRowsShort
	}
	return statusRowsFull
}

type StatusData struct {
	Agent, Mode, Dir, Spin, Note string
	Model                        string
	Diff                         DiffView
	Busy                         bool
	ElapsedSec                   float64
	Cost                         float64
	Adds, Dels                   int
	// Branch and Untracked come from the same git read as Adds/Dels and make
	// up the git:(…) group.
	Branch    string
	Untracked int
	// ContextTokens/ContextWindow drive the context gauge; Limits are the
	// backend's usage windows (Claude Code's rolling 5-hour and 7-day
	// allowances).
	ContextTokens, ContextWindow int64
	Limits                       []agent.RateLimit
	// PR is the branch's pull request, when gh knows of one — a chip on the
	// mode row, coloured by its state, clickable to open it.
	PR *PRInfo
}

// seg is one run of styled text in the bar. Everything is drawn on the bar's
// own surface unless it sets bg, which is how a gauge is filled: a run of
// spaces with a background is a solid block in any font.
type seg struct {
	text  string
	color lipgloss.Color
	bg    lipgloss.Color
	bold  bool
}

func txt(c lipgloss.Color, s string) seg     { return seg{text: s, color: c} }
func boldTxt(c lipgloss.Color, s string) seg { return seg{text: s, color: c, bold: true} }
func block(c lipgloss.Color, cells int) seg  { return seg{text: sp(cells), color: c, bg: c} }
func sp(n int) string                        { return strings.Repeat(" ", max(n, 0)) }
func sep() seg                               { return txt(T.Muted, " │ ") }

func segWidth(segs []seg) (n int) {
	for _, g := range segs {
		n += lipgloss.Width(g.text)
	}
	return n
}

func (g seg) render() string {
	if g.text == "" {
		return ""
	}
	st := lipgloss.NewStyle().Foreground(g.color).Background(T.Surface).Bold(g.bold)
	if g.bg != "" {
		st = st.Background(g.bg)
	}
	return st.Render(g.text)
}

// fitSegs cuts a group down to w columns, clipping the segment that straddles
// the edge rather than dropping it whole.
func fitSegs(segs []seg, w int) []seg {
	var out []seg
	used := 0
	for _, g := range segs {
		gw := lipgloss.Width(g.text)
		if used+gw <= w {
			out, used = append(out, g), used+gw
			continue
		}
		if room := w - used; room > 0 {
			g.text = clip(g.text, room)
			out = append(out, g)
		}
		break
	}
	return out
}

// statusRow lays out one row of the bar: the left group, the bar's own
// background, then the right group flush to the edge. Always exactly w wide,
// which is what keeps the chips under the columns hit-testing expects.
func statusRow(left, right []seg, w int) string {
	if w <= 0 {
		return ""
	}
	rw := segWidth(right)
	if rw > w {
		right, rw = nil, 0
	}
	left = fitSegs(left, w-rw)
	var b strings.Builder
	for _, g := range left {
		b.WriteString(g.render())
	}
	if pad := w - segWidth(left) - rw; pad > 0 {
		b.WriteString(lipgloss.NewStyle().Background(T.Surface).Render(sp(pad)))
	}
	for _, g := range right {
		b.WriteString(g.render())
	}
	return b.String()
}

// gaugeColor grades a bar by how alarming it is, which is the only thing the
// colour of a gauge should ever mean.
func gaugeColor(frac float64) lipgloss.Color {
	switch {
	case frac >= 0.85:
		return T.Red
	case frac >= 0.6:
		return T.Yellow
	}
	return T.Green
}

// gauge draws a filled bar and its percentage. A negative fraction means the
// number isn't known yet — the track is drawn empty with a dash, so the row
// keeps its shape before the first turn reports anything.
func gauge(frac float64) []seg {
	if frac < 0 {
		return []seg{{text: sp(gaugeCells), color: T.Track, bg: T.Track}, txt(T.Muted, " —")}
	}
	frac = min(max(frac, 0), 1)
	fill := int(frac*gaugeCells + 0.5)
	c := gaugeColor(frac)
	var out []seg
	if fill > 0 {
		out = append(out, block(c, fill))
	}
	if fill < gaugeCells {
		out = append(out, block(T.Track, gaugeCells-fill))
	}
	return append(out, boldTxt(c, fmt.Sprintf(" %d%%", int(frac*100+0.5))))
}

// contextFrac is how full the model's context window is, or -1 when the
// backend hasn't said yet.
func contextFrac(used, window int64) float64 {
	if used <= 0 || window <= 0 {
		return -1
	}
	return float64(used) / float64(window)
}

// plus writes a big unit and its remainder, dropping the remainder when it is
// zero: "4d 18h", but "3h" rather than "3h 0m".
func plus(big int, bu string, small int, su string) string {
	if small == 0 {
		return fmt.Sprintf("%d%s", big, bu)
	}
	return fmt.Sprintf("%d%s %d%s", big, bu, small, su)
}

// resetIn says when a usage window comes back, in the largest unit that still
// tells you something: days and hours for a weekly allowance, minutes for one
// about to turn over.
func resetIn(d time.Duration) string {
	switch {
	case d <= 0:
		return "resetting"
	case d >= 24*time.Hour:
		return "resets in " + plus(int(d.Hours())/24, "d", int(d.Hours())%24, "h")
	case d >= time.Hour:
		return "resets in " + plus(int(d.Hours()), "h", int(d.Minutes())%60, "m")
	}
	return fmt.Sprintf("resets in %dm", int(d.Minutes())+1)
}

// shortDir keeps a long path readable by showing only what tells you which
// project you're in: its folder and the one above it.
func shortDir(dir string) string {
	if dir == "" || lipgloss.Width(dir) <= dirBudget {
		return dir
	}
	parent, base := filepath.Split(filepath.Clean(dir))
	if p := filepath.Base(filepath.Clean(parent)); p != "" && p != "." && p != base {
		return "…" + string(filepath.Separator) + p + string(filepath.Separator) + base
	}
	return base
}

// gitSegs is the git:(branch* +a −d ?u) group. Without a branch — a directory
// that isn't a repo — the counts still show, because a change crema can see is
// worth reporting whatever git thinks of the place. With neither, nothing does.
func gitSegs(s StatusData) []seg {
	counts := []seg{
		txt(T.Green, fmt.Sprintf(" +%d", s.Adds)),
		txt(T.Red, fmt.Sprintf(" −%d", s.Dels)),
	}
	if s.Untracked > 0 {
		counts = append(counts, txt(T.Muted, fmt.Sprintf(" ?%d", s.Untracked)))
	}
	if s.Branch == "" {
		if s.Adds == 0 && s.Dels == 0 && s.Untracked == 0 {
			return nil
		}
		return counts // no repo: the counts stand alone
	}
	out := []seg{txt(T.Muted, "git:("), txt(T.Magenta, s.Branch)}
	if s.Adds > 0 || s.Dels > 0 || s.Untracked > 0 {
		out = append(out, txt(T.Yellow, "*"))
	}
	out = append(out, counts...)
	return append(out, txt(T.Muted, ")"))
}

// identityRow is who is running, on what, where. Compact terminals get the
// gauge numbers here too, since the row that would have held them is gone.
func identityRow(s StatusData, w int, withGauges bool) string {
	var l []seg
	if s.Busy {
		l = append(l, txt(T.Pink, " "+s.Spin+" "))
	} else {
		l = append(l, txt(T.Green, " ● "))
	}
	l = append(l, boldTxt(T.Fg, s.Agent))
	if s.Model != "" {
		l = append(l, sep(), txt(T.Purple, "["+s.Model+"]"))
	}
	git := gitSegs(s)
	if d := shortDir(s.Dir); d != "" {
		l = append(l, sep(), txt(T.Yellow, d))
		if git != nil {
			l = append(l, txt(T.Muted, " "))
		}
	} else if git != nil {
		l = append(l, sep())
	}
	l = append(l, git...)
	if s.Busy {
		l = append(l, sep(), txt(T.Lilac, fmt.Sprintf("%.1fs", s.ElapsedSec)))
	}
	if s.Cost > 0 {
		l = append(l, sep(), txt(T.Muted, fmt.Sprintf("$%.4f", s.Cost)))
	}
	if withGauges {
		if f := contextFrac(s.ContextTokens, s.ContextWindow); f >= 0 {
			l = append(l, sep(), txt(T.Muted, "ctx "), boldTxt(gaugeColor(f), fmt.Sprintf("%d%%", int(f*100+0.5))))
		}
		for _, lim := range s.Limits {
			if !lim.Known {
				continue // no room here to explain a window without a number
			}
			l = append(l, sep(), txt(T.Muted, lim.Label()+" "),
				boldTxt(gaugeColor(lim.Utilization),
					fmt.Sprintf("%d%%", int(lim.Utilization*100+0.5))))
		}
	}
	return statusRow(l, nil, w)
}

// gaugeRow is the allowances that run out: the context window, then each of
// the backend's usage windows.
//
// A window whose share is unknown is drawn as its name and its countdown, with
// no bar. The reset is real and worth knowing; a bar would have to claim a
// number nobody reported.
func gaugeRow(s StatusData, w int) string {
	l := []seg{txt(T.Muted, " Context ")}
	l = append(l, gauge(contextFrac(s.ContextTokens, s.ContextWindow))...)
	for _, lim := range s.Limits {
		l = append(l, sep(), txt(T.Muted, lim.Label()+" "))
		switch {
		case lim.Known:
			l = append(l, gauge(lim.Utilization)...)
		case lim.Surpassed > 0:
			// Not the number, but the floor under it, which is what the CLI
			// says once a window is far enough along to be worth saying.
			l = append(l, boldTxt(gaugeColor(lim.Surpassed),
				fmt.Sprintf("over %d%% ", int(lim.Surpassed*100+0.5))))
		}
		if lim.ResetsAt.IsZero() {
			continue
		}
		since := resetIn(time.Until(lim.ResetsAt))
		if lim.Known {
			since = " (" + since + ")" // beside a bar it reads as a footnote
		}
		l = append(l, txt(T.Muted, since))
	}
	return statusRow(l, nil, w)
}

// modeRow states what the agent may do, carries the transient note, and holds
// the two buttons.
func modeRow(s StatusData, w int) string {
	l := []seg{txt(T.Magenta, " ▸▸ ")}
	if s.Mode != "" {
		l = append(l, boldTxt(T.Yellow, s.Mode), txt(T.Muted, " (ctrl+p to change)"))
	}
	if s.Note != "" {
		l = append(l, txt(T.Muted, " · "), txt(T.Pink, s.Note))
	}

	var right []seg
	if start, _ := PRRange(w, s.Diff, s.PR); start > 0 {
		right = append(right, boldTxt(prColor(s.PR), chip(prLabel(s.PR), prChipWidth(s.PR))))
	}
	if start, _ := ShowDiffRange(w, s.Diff); start > 0 {
		right = append(right, boldTxt(T.Lilac, chip("diff", showDiffWidth)))
	}
	if start, _ := ThemeToggleRange(w); start > 0 {
		right = append(right, seg{text: themeChip(), color: T.Surface, bg: T.Purple, bold: true})
	}
	return statusRow(l, right, w)
}

// ThemeToggleRange is the half-open column range [start, end) of the clickable
// theme chip, or 0,0 when the bar is too narrow to show it.
func ThemeToggleRange(w int) (start, end int) {
	if w < themeToggleWidth+2 {
		return 0, 0
	}
	return w - themeToggleWidth, w
}

// ShowDiffRange is the half-open column range of the chip that brings a hidden
// diff back, or 0,0 when the diff is on screen and carrying its own buttons.
func ShowDiffRange(w int, v DiffView) (start, end int) {
	theme, _ := ThemeToggleRange(w)
	if v != DiffHidden || theme == 0 || w < themeToggleWidth+showDiffWidth+2 {
		return 0, 0
	}
	return theme - showDiffWidth, theme
}

// prLabel is the chip's text; the state only spells itself out once it is no
// longer the ordinary one.
func prLabel(pr *PRInfo) string {
	l := "PR #" + itoa(pr.Number)
	switch {
	case pr.State == "MERGED":
		l += " merged"
	case pr.State == "CLOSED":
		l += " closed"
	case pr.Draft:
		l += " draft"
	}
	return l
}

func prChipWidth(pr *PRInfo) int { return lipgloss.Width(prLabel(pr)) + 2 }

func prColor(pr *PRInfo) lipgloss.Color {
	switch {
	case pr.State == "MERGED":
		return T.Purple
	case pr.State == "CLOSED":
		return T.Red
	case pr.Draft:
		return T.Muted
	}
	return T.Green
}

// PRRange is the half-open column range of the PR chip: to the left of
// whatever chips already hold the corner, or 0,0 when there is no PR or no
// room.
func PRRange(w int, v DiffView, pr *PRInfo) (start, end int) {
	if pr == nil {
		return 0, 0
	}
	left, _ := ThemeToggleRange(w)
	if ds, _ := ShowDiffRange(w, v); ds > 0 {
		left = ds
	}
	pw := prChipWidth(pr)
	if left == 0 || left-pw < 2 {
		return 0, 0
	}
	return left - pw, left
}

// chip renders a fixed-width button label, centered.
func chip(label string, width int) string {
	pad := width - 2 - lipgloss.Width(label)
	if pad < 0 {
		pad = 0
	}
	left := pad / 2
	return "[" + sp(left) + label + sp(pad-left) + "]"
}

func themeChip() string { return chip(CurrentMode().String(), themeToggleWidth) }

// RenderStatus draws the bar: exactly rows lines, each exactly w columns, with
// the buttons on the last one.
func RenderStatus(s StatusData, w, rows int) string {
	if w <= 0 || rows <= 0 {
		return ""
	}
	full := rows >= statusRowsFull
	out := []string{identityRow(s, w, !full)}
	if full {
		out = append(out, gaugeRow(s, w))
	}
	for len(out) < rows-1 {
		out = append(out, statusRow(nil, nil, w))
	}
	return strings.Join(append(out, modeRow(s, w)), "\n")
}
