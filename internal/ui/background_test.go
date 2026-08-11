package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// trueColor switches on real escape codes for tests that inspect them.
func trueColor(t *testing.T) {
	t.Helper()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
}

var bgSeq = regexp.MustCompile(`\x1b\[[0-9;]*48;2;(\d+);(\d+);(\d+)m`)

// paintedCells counts, per line, how many columns are covered by some
// background color — the terminal's own background shows through anywhere else.
func unpaintedLines(frame string) []int {
	var bad []int
	for i, ln := range strings.Split(frame, "\n") {
		if strings.TrimSpace(stripANSIFull(ln)) == "" && !bgSeq.MatchString(ln) {
			bad = append(bad, i) // a blank row with no background at all
			continue
		}
		if !bgSeq.MatchString(ln) {
			bad = append(bad, i)
		}
	}
	return bad
}

func stripANSIFull(s string) string {
	return regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`).ReplaceAllString(s, "")
}

// bareTails finds lines with visible characters after the final reset. Those
// trail off into the terminal's own background even though the line started
// painted — the subtle half of the "only the button changed" bug.
func bareTails(frame string) []int {
	var bad []int
	for i, ln := range strings.Split(frame, "\n") {
		idx := strings.LastIndex(ln, "\x1b[0m")
		if idx < 0 {
			continue // handled by unpaintedLines
		}
		if tail := ln[idx+len("\x1b[0m"):]; stripANSIFull(tail) != "" {
			bad = append(bad, i)
		}
	}
	return bad
}

func frameApp(t *testing.T, w, h int) *App {
	t.Helper()
	mk := fastMock()
	reg := &agent.Registry{Agents: []agent.Agent{mk}}
	a := NewApp(reg, mk, t.TempDir())
	a.resize(w, h)
	a.cur().tl.Append(Block{Kind: BlockTool, Name: "Bash", Text: "echo hi"})
	a.cur().dp.SetDiff(sampleDiff())
	return a
}

// TestEveryLinePaintsABackground is the regression guard for "dark mode only
// changed the button": if any row leaves the terminal's own background showing,
// switching themes will not look like it did anything.
func TestEveryLinePaintsABackground(t *testing.T) {
	restoreTheme(t)
	trueColor(t)
	for _, mode := range []Mode{ModeDark, ModeLight} {
		SetMode(mode)
		for _, size := range [][2]int{{80, 24}, {140, 30}} {
			a := frameApp(t, size[0], size[1])
			frame := a.View()
			if bad := unpaintedLines(frame); len(bad) > 0 {
				t.Fatalf("%s at %dx%d: lines %v have no background",
					mode, size[0], size[1], bad)
			}
			if bad := bareTails(frame); len(bad) > 0 {
				t.Fatalf("%s at %dx%d: lines %v trail off unpainted after their last reset",
					mode, size[0], size[1], bad)
			}
		}
	}
}

func TestPickerFramePaintsABackground(t *testing.T) {
	restoreTheme(t)
	trueColor(t)
	SetMode(ModeLight)
	a := frameApp(t, 120, 26)
	a.openPicker()
	if bad := unpaintedLines(a.View()); len(bad) > 0 {
		t.Fatalf("picker frame: lines %v have no background", bad)
	}
}

// TestThemeSwitchChangesTheBackground proves the two modes actually differ in
// the painted background, not only in text color.
func TestThemeSwitchChangesTheBackground(t *testing.T) {
	restoreTheme(t)
	trueColor(t)

	SetMode(ModeDark)
	a := frameApp(t, 120, 20)
	darkBGs := bgColors(a.View())

	start, _ := ThemeToggleRange(a.w)
	a.Update(click(start+2, a.h-1))
	lightBGs := bgColors(a.View())

	if len(darkBGs) == 0 || len(lightBGs) == 0 {
		t.Fatal("expected background colors in both frames")
	}
	for c := range darkBGs {
		if lightBGs[c] {
			t.Fatalf("background %s survives the theme switch — it is hard-coded somewhere", c)
		}
	}
}

func bgColors(frame string) map[string]bool {
	out := map[string]bool{}
	for _, m := range bgSeq.FindAllStringSubmatch(frame, -1) {
		out[m[1]+","+m[2]+","+m[3]] = true
	}
	return out
}
