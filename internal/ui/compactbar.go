package ui

import (
	"strings"
	"time"
)

// Compacting is the one thing an agent does that has nothing to show for
// itself while it happens: the conversation is being read and summarised, and
// until the summary arrives the transcript sits there looking stopped. So it
// gets a line of its own at the bottom, the way the CLI says "Compacting
// conversation" rather than leaving you wondering.
//
// The bar sweeps rather than fills. There is no fraction to fill it with —
// the CLI knows how big the conversation was and how big the summary came out
// (its own preTokens and postTokens), but the second of those only exists
// once the work is done, and nothing reports progress through it. A bar that
// crept along on a guess would be a lie told in a convincing shape, so this
// one says "still going" and leaves the honest numbers beside it: how much is
// being folded up, how long it has taken, and what has been written so far.

const (
	// sweepCells is the bar's width in columns, matching the status bar's
	// gauges so the bottom of the screen keeps one measure.
	sweepCells = gaugeCells
	// sweepBlock is how much of the track the moving part covers.
	sweepBlock = 4
	// sweepPeriod is one full pass. Slow enough to read as deliberate.
	sweepPeriod = 1200 * time.Millisecond
)

// sweep draws the indeterminate bar at the phase the clock puts it in.
func sweep(elapsed time.Duration) []seg {
	span := sweepCells + sweepBlock
	at := int(float64(span) * (float64(elapsed%sweepPeriod) / float64(sweepPeriod)))
	start, end := max(at-sweepBlock, 0), min(at, sweepCells)
	var out []seg
	if start > 0 {
		out = append(out, block(T.Track, start))
	}
	if end > start {
		out = append(out, block(T.Purple, end-start))
	}
	if end < sweepCells {
		out = append(out, block(T.Track, sweepCells-end))
	}
	return out
}

// compactingRow is the line drawn while an agent folds itself up, or "" when
// it isn't. Sits with the roster under the status bar.
func (s *Session) compactingRow(w int) string {
	if !s.compacting || w < 24 {
		return ""
	}
	l := []seg{txt(T.Magenta, " ▸▸ "), boldTxt(T.Fg, "Compacting conversation"), txt(T.Muted, "  ")}
	l = append(l, sweep(s.Elapsed())...)

	var facts []string
	if s.compactFrom > 0 {
		facts = append(facts, shortCount(s.compactFrom)+" tokens")
	}
	if d := s.Elapsed(); d > 0 {
		facts = append(facts, shortDur(d))
	}
	if s.turnOut > 0 {
		facts = append(facts, "↓ "+shortCount(s.turnOut)+" written")
	}
	if len(facts) > 0 {
		l = append(l, txt(T.Muted, "  "+strings.Join(facts, " · ")))
	}
	return statusRow(l, nil, w)
}

// CompactingRows is how many rows that takes, so the frame can give them up.
func (s *Session) CompactingRows() int {
	if s.compacting {
		return 1
	}
	return 0
}

// noteCompacting records what is about to be folded up, so the line can say
// how much work it is. Called wherever compaction starts.
func (s *Session) noteCompacting() {
	s.compacting = true
	s.compactFrom = s.ctxTokens
}
