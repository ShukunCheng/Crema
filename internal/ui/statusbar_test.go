package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/charmbracelet/lipgloss"
)

// sample is a bar with something in every group.
func sample() StatusData {
	return StatusData{
		Agent: "Claude Code", Mode: "acceptEdits", Dir: "D:/Crema", Model: "opus",
		Busy: true, Spin: "⠋", ElapsedSec: 12.5, Cost: 0.42,
		Branch: "main", Adds: 10, Dels: 3, Untracked: 2,
		ContextTokens: 40_000, ContextWindow: 200_000,
		Limits: []agent.RateLimit{{Type: "five_hour", Utilization: 0.87, Known: true,
			ResetsAt: time.Now().Add(3 * time.Hour)}},
	}
}

// Every row is exactly the terminal's width, or the panes above it shift.
func TestRenderStatusIsARectangle(t *testing.T) {
	for _, rows := range []int{statusRowsShort, statusRowsFull} {
		for _, w := range []int{120, 80, 40, 20} {
			out := RenderStatus(sample(), w, rows)
			lines := strings.Split(out, "\n")
			if len(lines) != rows {
				t.Fatalf("w=%d: %d lines, want %d", w, len(lines), rows)
			}
			for i, ln := range lines {
				if lipgloss.Width(ln) != w {
					t.Fatalf("w=%d row %d is %d wide: %q", w, i, lipgloss.Width(ln), ln)
				}
			}
		}
	}
}

// A short terminal gives up the gauge row, but not the numbers on it.
func TestAShortTerminalKeepsTheNumbersItDropsTheBarsFor(t *testing.T) {
	if got := StatusRows(24); got != statusRowsFull {
		t.Fatalf("StatusRows(24) = %d", got)
	}
	if got := StatusRows(12); got != statusRowsShort {
		t.Fatalf("StatusRows(12) = %d", got)
	}
	short := stripSGR(RenderStatus(sample(), 120, statusRowsShort))
	if strings.Contains(short, "Context") {
		t.Fatalf("the gauge row should be gone: %q", short)
	}
	for _, want := range []string{"ctx 20%", "5h 87%"} {
		if !strings.Contains(short, want) {
			t.Fatalf("compact bar lost %q: %q", want, short)
		}
	}
}

func TestRenderStatusShowsWhatIsRunningAndWhatItMay(t *testing.T) {
	out := stripSGR(RenderStatus(sample(), 120, statusRowsFull))
	for _, want := range []string{"Claude Code", "[opus]", "acceptEdits", "12.5s", "$0.4200"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status is missing %q: %q", want, out)
		}
	}
}

func TestRenderStatusIdleHidesTimer(t *testing.T) {
	out := RenderStatus(StatusData{Agent: "Codex", Mode: "full-auto", ElapsedSec: 9}, 100, statusRowsFull)
	if strings.Contains(out, "9.0s") {
		t.Fatalf("idle status must not show a running timer: %q", out)
	}
}

// The git group reads like a prompt's: branch, dirty, and the counts.
func TestGitGroupReadsLikeAPrompt(t *testing.T) {
	out := stripSGR(RenderStatus(sample(), 120, statusRowsFull))
	if !strings.Contains(out, "git:(main* +10 −3 ?2)") {
		t.Fatalf("git group: %q", out)
	}
	d := sample()
	d.Branch, d.Untracked = "", 0
	plain := stripSGR(RenderStatus(d, 120, statusRowsFull))
	if strings.Contains(plain, "git:(") || !strings.Contains(plain, "+10 −3") {
		t.Fatalf("outside a repo the counts stand alone: %q", plain)
	}
}

// The gauges are drawn as filled blocks, so what's on screen is a run of
// spaces — the percentages are how a reader (and this test) tells them apart.
func TestGaugesReportContextAndUsage(t *testing.T) {
	out := stripSGR(RenderStatus(sample(), 120, statusRowsFull))
	row := strings.Split(out, "\n")[1]
	for _, want := range []string{"Context", "20%", "5h", "87%", "(resets in "} {
		if !strings.Contains(row, want) {
			t.Fatalf("gauge row is missing %q: %q", want, row)
		}
	}
}

// Before the first turn there is nothing to gauge, and the row says so rather
// than claiming 0%.
func TestAnUnknownGaugeShowsADash(t *testing.T) {
	out := stripSGR(RenderStatus(StatusData{Agent: "Mock", Mode: "plan"}, 80, statusRowsFull))
	row := strings.Split(out, "\n")[1]
	if !strings.Contains(row, "Context") || !strings.Contains(row, "—") {
		t.Fatalf("unknown context should read as a dash: %q", row)
	}
	if strings.Contains(row, "0%") {
		t.Fatalf("an unknown window is not an empty one: %q", row)
	}
}

// Colour on a gauge means one thing: how close it is to running out.
func TestGaugeColorGradesByHowFullItIs(t *testing.T) {
	SetMode(ModeDark)
	for _, c := range []struct {
		frac float64
		want lipgloss.Color
	}{{0, T.Green}, {0.59, T.Green}, {0.6, T.Yellow}, {0.84, T.Yellow}, {0.85, T.Red}, {1, T.Red}} {
		if got := gaugeColor(c.frac); got != c.want {
			t.Fatalf("gaugeColor(%v) = %v, want %v", c.frac, got, c.want)
		}
	}
}

// The bar fills in proportion: ten cells, one per ten percent.
func TestAGaugeFillsInProportion(t *testing.T) {
	for _, c := range []struct{ frac, fill float64 }{{0, 0}, {0.25, 3}, {0.5, 5}, {1, 10}} {
		filled := 0
		for _, g := range gauge(c.frac) {
			if g.bg == T.Track || g.bg == "" {
				continue
			}
			filled += lipgloss.Width(g.text)
		}
		if float64(filled) != c.fill {
			t.Fatalf("%v filled %d cells, want %v", c.frac, filled, c.fill)
		}
	}
}

func TestResetInPicksAUsefulUnit(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{-time.Minute, "resetting"},
		{90 * time.Second, "resets in 2m"},
		{3*time.Hour + 20*time.Minute, "resets in 3h 20m"},
		{3 * time.Hour, "resets in 3h"},
		{4*24*time.Hour + 18*time.Hour, "resets in 4d 18h"},
	} {
		if got := resetIn(c.d); got != c.want {
			t.Fatalf("resetIn(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// A path long enough to crowd the row is cut down to the part that says which
// project it is.
func TestALongDirIsShortenedToItsLastTwoParts(t *testing.T) {
	if got := shortDir("D:/Crema"); got != "D:/Crema" {
		t.Fatalf("a short path is left alone: %q", got)
	}
	long := "D:/work/customers/acme/services/checkout"
	got := shortDir(long)
	if !strings.HasSuffix(strings.ReplaceAll(got, "\\", "/"), "services/checkout") {
		t.Fatalf("shortDir(%q) = %q", long, got)
	}
	if lipgloss.Width(got) >= lipgloss.Width(long) {
		t.Fatalf("shortDir made it no shorter: %q", got)
	}
}

// The buttons have to land exactly where hit-testing looks for them.
func TestTheChipsSitAtTheColumnsTheClickHandlerExpects(t *testing.T) {
	const w = 100
	last := strings.Split(RenderStatus(sample(), w, statusRowsFull), "\n")[statusRowsFull-1]
	plain := []rune(stripSGR(last))
	if len(plain) != w {
		t.Fatalf("last row is %d cells", len(plain))
	}
	theme, _ := ThemeToggleRange(w)
	if string(plain[theme:]) != themeChip() {
		t.Fatalf("theme chip is at %q, not %d", string(plain[theme:]), theme)
	}
}

// usageStub is a backend that reports allowance windows.
type usageStub struct {
	*agent.Mock
	windows []agent.RateLimit
}

func (u usageStub) Usage() []agent.RateLimit { return u.windows }

// The percentage comes from the backend's usage cache; the stream's own event
// contributes a window the cache doesn't cover, reset time only.
func TestLimitsMergeTheCacheAndTheStream(t *testing.T) {
	reset := time.Now().Add(2 * time.Hour)
	s := NewSession(1, usageStub{agent.NewMock(), []agent.RateLimit{
		{Type: "seven_day", Utilization: 0.27, Known: true, ResetsAt: reset},
	}}, t.TempDir())
	if len(s.limits) != 1 || !s.limits[0].Known {
		t.Fatalf("a new session should already know the allowance: %+v", s.limits)
	}

	s.refreshLimits([]agent.RateLimit{{Type: "five_hour", ResetsAt: reset}})
	if len(s.limits) != 2 {
		t.Fatalf("the stream's window should be added: %+v", s.limits)
	}
	if s.limits[1].Known {
		t.Fatalf("the stream reported no percentage: %+v", s.limits[1])
	}
	// And it survives a refresh that has nothing new to say.
	s.refreshLimits(nil)
	if len(s.limits) != 2 {
		t.Fatalf("refreshing dropped what the stream had said: %+v", s.limits)
	}
}

// A window the cache covers is not doubled by the stream's event for it.
func TestTheCacheWinsAWindowTheStreamAlsoNames(t *testing.T) {
	reset := time.Now().Add(2 * time.Hour)
	s := NewSession(1, usageStub{agent.NewMock(), []agent.RateLimit{
		{Type: "five_hour", Utilization: 0.12, Known: true, ResetsAt: reset},
	}}, t.TempDir())
	s.refreshLimits([]agent.RateLimit{{Type: "five_hour", ResetsAt: reset}})
	if len(s.limits) != 1 || !s.limits[0].Known {
		t.Fatalf("want one window, with its number: %+v", s.limits)
	}
}

// A window with no percentage is drawn as its name and its countdown. There is
// no bar, because there is no number — and no 0%, because that would be one.
func TestAWindowWithoutAPercentageShowsItsResetNotAZero(t *testing.T) {
	d := sample()
	d.Limits = []agent.RateLimit{{Type: "five_hour", ResetsAt: time.Now().Add(2 * time.Hour)}}
	row := strings.Split(stripSGR(RenderStatus(d, 120, statusRowsFull)), "\n")[1]
	if !strings.Contains(row, "5h resets in ") {
		t.Fatalf("row = %q", row)
	}
	_, window, _ := strings.Cut(row, "5h ")
	if strings.Contains(window, "%") {
		t.Fatalf("an unreported share must not read as a number: %q", row)
	}
}

// countingUsage reports how often it was asked.
type countingUsage struct {
	*agent.Mock
	calls *int
}

func (c countingUsage) Usage() []agent.RateLimit {
	*c.calls++
	return []agent.RateLimit{{Type: "five_hour", Utilization: 0.42, Known: true,
		ResetsAt: time.Now().Add(time.Hour)}}
}

// The bridge rewrites the allowance whenever an interactive session redraws,
// so crema re-reads it as it sits there — but not on every frame.
func TestTheAllowanceIsRereadOnATimerNotEveryFrame(t *testing.T) {
	calls := 0
	s := NewSession(1, countingUsage{agent.NewMock(), &calls}, t.TempDir())
	if calls != 1 {
		t.Fatalf("a new agent should read it once, got %d", calls)
	}
	for i := 0; i < 50; i++ {
		s.maybeRefreshLimits()
	}
	if calls != 1 {
		t.Fatalf("a frame must not re-read it: %d reads", calls)
	}

	s.limitsAt = time.Now().Add(-usageRefresh - time.Second)
	s.maybeRefreshLimits()
	if calls != 2 {
		t.Fatalf("after the interval it should read again: %d reads", calls)
	}
	if len(s.limits) != 1 || !s.limits[0].Known {
		t.Fatalf("limits = %+v", s.limits)
	}
}

// Both windows draw the same way: a bar, a percentage, and when they reset.
func TestEveryKnownWindowGetsABar(t *testing.T) {
	restoreTheme(t)
	SetMode(ModeDark)
	inColor(t)
	d := sample()
	d.Limits = []agent.RateLimit{
		{Type: "five_hour", Utilization: 0.72, Known: true, ResetsAt: time.Now().Add(2 * time.Hour)},
		{Type: "seven_day", Utilization: 0.27, Known: true, ResetsAt: time.Now().Add(50 * time.Hour)},
	}
	row := strings.Split(RenderStatus(d, 150, statusRowsFull), "\n")[1]
	for _, want := range []string{"Context", "5h", "72%", "7d", "27%", "resets in 2d"} {
		if !strings.Contains(stripSGR(row), want) {
			t.Fatalf("gauge row is missing %q: %q", want, stripSGR(row))
		}
	}
	// Three bars, each filled in the colour its own level earns.
	for _, c := range []lipgloss.Color{T.Green, T.Yellow} {
		if !strings.Contains(row, "48;2;"+rgb(c)) {
			t.Fatalf("no bar filled in %v", c)
		}
	}
	if n := strings.Count(row, "48;2;"+rgb(T.Track)); n != 3 {
		t.Fatalf("want three unfilled tracks, got %d", n)
	}
}

// A window that has reset is not still 94% used. The turn's remembered
// report used to outlive its own reset time and keep a stale warning on the
// gauges — measured live, one minute after a reset.
func TestAStaleTurnLimitDropsAtItsReset(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.refreshLimits([]agent.RateLimit{{
		Type: "five_hour", Utilization: 0.94, Known: true,
		ResetsAt: time.Now().Add(-time.Minute), Surpassed: 0.9,
	}})
	for _, l := range s.limits {
		if l.Type == "five_hour" {
			t.Fatalf("the window reset a minute ago; showing %+v is a lie", l)
		}
	}
	// Still running windows stay.
	s.refreshLimits([]agent.RateLimit{{
		Type: "five_hour", Utilization: 0.05, Known: true,
		ResetsAt: time.Now().Add(4 * time.Hour),
	}})
	if len(s.limits) == 0 || s.limits[0].Utilization != 0.05 {
		t.Fatalf("limits = %+v", s.limits)
	}
}

// An idle crema still watches the numbers: the heartbeat re-arms itself and
// wakes the frame, and each wake-up is willing to re-read the usage file.
func TestTheHeartbeatKeepsIdleGaugesFresh(t *testing.T) {
	a := testApp(t)
	_, cmd := a.Update(heartbeatMsg{})
	if cmd == nil {
		t.Fatal("the heartbeat must re-arm or the app goes back to sleep")
	}
	s := a.cur()
	s.limitsAt = time.Now().Add(-usageRefresh - time.Second)
	was := s.limitsAt
	_ = a.View()
	if !s.limitsAt.After(was) {
		t.Fatal("a frame after the refresh interval should re-read the allowance")
	}
}
