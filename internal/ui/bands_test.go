package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Crema paints three bands behind whole rows — what you typed, and the lines a
// change added or removed — and they have to be the same three wherever they
// appear. These tests read the colours back out of the escape codes, since
// that is the only place the band actually exists.

var backgroundPattern = regexp.MustCompile(`48;2;(\d+);(\d+);(\d+)`)

// inColor renders with real colours for the duration of a test; the package
// otherwise runs with them stripped so assertions can see plain text.
func inColor(t *testing.T) {
	t.Helper()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
}

// rgb is a colour written the way an escape code writes it.
func rgb(c lipgloss.Color) string {
	var r, g, b int
	fmt.Sscanf(string(c), "#%02x%02x%02x", &r, &g, &b)
	return fmt.Sprintf("%d;%d;%d", r, g, b)
}

// hasBand reports whether a rendered line switches to a given background.
func hasBand(line string, c lipgloss.Color) bool {
	for _, m := range backgroundPattern.FindAllString(line, -1) {
		if m == "48;2;"+rgb(c) {
			return true
		}
	}
	return false
}

func TestYourMessagesSitOnABandOfTheirOwn(t *testing.T) {
	inColor(t)
	for _, line := range strings.Split(strings.TrimRight(RenderUser("fix the parser", 40), "\n"), "\n") {
		if !hasBand(line, T.UserBg) {
			t.Fatalf("no band behind %q", stripSGR(line))
		}
	}
	// The agent's own prose is not banded, or the two would be hard to tell
	// apart at a glance.
	if hasBand(RenderAssistant("I'll take a look.", 40), T.UserBg) {
		t.Fatal("the agent's reply must not wear the same band")
	}
}

// The same two bands mark a change in the conversation and in the diff pane.
func TestAChangeIsBandedEverywhere(t *testing.T) {
	inColor(t)
	edit := `{"file_path":"main.go","old_string":"one\ntwo","new_string":"one\nTWO"}`

	views := map[string][]string{
		"conversation": strings.Split(RenderTool("Edit", edit, 40), "\n"),
		"diff pane":    diffTexts(renderDiffRows(sampleDiff(), 40, allOpen(sampleDiff()))),
		"split view":   diffTexts(renderSplitRows(splitSample(), 100, allOpen(splitSample()))),
	}
	for name, lines := range views {
		t.Run(name, func(t *testing.T) {
			var add, del bool
			for _, l := range lines {
				add = add || hasBand(l, T.AddBg)
				del = del || hasBand(l, T.DelBg)
			}
			if !add {
				t.Errorf("no added-line band:\n%s", strings.Join(stripAll(lines), "\n"))
			}
			if !del {
				t.Errorf("no removed-line band:\n%s", strings.Join(stripAll(lines), "\n"))
			}
		})
	}
}

// Context lines stay on the ordinary background: a band means something
// changed, and a band on everything would mean nothing.
func TestContextLinesAreNotBanded(t *testing.T) {
	inColor(t)
	for _, r := range renderDiffRows(sampleDiff(), 40, allOpen(sampleDiff())) {
		plain := stripSGR(r.text)
		if !strings.HasPrefix(plain, " ctx") {
			continue
		}
		if hasBand(r.text, T.AddBg) || hasBand(r.text, T.DelBg) {
			t.Fatalf("a context line must not be banded: %q", plain)
		}
	}
}

func diffTexts(rows []diffRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.text
	}
	return out
}

func stripAll(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = stripSGR(l)
	}
	return out
}
