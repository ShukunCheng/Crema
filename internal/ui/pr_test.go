package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ShukunCheng/Crema/internal/gitdiff"
	tea "github.com/charmbracelet/bubbletea"
)

func stubPR(t *testing.T, out string, err error) *int {
	t.Helper()
	calls := 0
	prev := ghPRLookup
	ghPRLookup = func(dir string) ([]byte, error) { calls++; return []byte(out), err }
	t.Cleanup(func() { ghPRLookup = prev })
	return &calls
}

// deliverDiff hands the app a diff result naming a branch, which is what
// triggers the PR lookup, and pumps the command it returns.
func deliverDiff(t *testing.T, a *App, s *Session, branch string) {
	t.Helper()
	s.diffSeq++
	_, cmd := a.Update(diffMsg{sess: s.ID, seq: s.diffSeq, ds: gitdiff.DiffSet{Branch: branch}})
	pump(t, a, cmd)
}

// A branch with a PR gets a chip in the bottom hub, and clicking it opens
// the PR in the browser.
func TestABranchWithAPRGetsAChip(t *testing.T) {
	a := testApp(t)
	a.resize(120, 26)
	stubPR(t, `{"number":3265,"state":"OPEN","isDraft":false,"url":"https://github.com/x/y/pull/3265"}`, nil)
	deliverDiff(t, a, a.cur(), "feature/pr-chip")

	if !strings.Contains(stripSGR(a.View()), "[PR #3265]") {
		t.Fatalf("no chip:\n%s", stripSGR(a.statusLine()))
	}

	var opened string
	prevOpen := openURL
	openURL = func(u string) error { opened = u; return nil }
	t.Cleanup(func() { openURL = prevOpen })
	start, end := PRRange(a.w, a.diffView, a.cur().pr)
	if start <= 0 || end <= start {
		t.Fatalf("range = %d,%d", start, end)
	}
	a.Update(tea.MouseMsg{X: start + 1, Y: a.h - 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	a.Update(tea.MouseMsg{X: start + 1, Y: a.h - 1, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if opened != "https://github.com/x/y/pull/3265" {
		t.Fatalf("opened = %q", opened)
	}
}

// The states spell themselves out, in their own colours; plain open does not.
func TestPRChipStates(t *testing.T) {
	for pr, want := range map[*PRInfo]string{
		{Number: 7, State: "OPEN"}:              "PR #7",
		{Number: 7, State: "OPEN", Draft: true}: "PR #7 draft",
		{Number: 7, State: "MERGED"}:            "PR #7 merged",
		{Number: 7, State: "CLOSED"}:            "PR #7 closed",
	} {
		if got := prLabel(pr); got != want {
			t.Fatalf("prLabel(%+v) = %q, want %q", pr, got, want)
		}
	}
}

// No PR is an answer too: no chip, and the miss is cached rather than asked
// again on every diff refresh.
func TestNoPRMeansNoChipAndNoNagging(t *testing.T) {
	a := testApp(t)
	a.resize(120, 26)
	calls := stubPR(t, "", errors.New("no pull requests found"))
	s := a.cur()
	deliverDiff(t, a, s, "main")
	if strings.Contains(stripSGR(a.View()), "PR #") {
		t.Fatal("no PR, no chip")
	}
	deliverDiff(t, a, s, "main")
	deliverDiff(t, a, s, "main")
	if *calls != 1 {
		t.Fatalf("gh was asked %d times; the miss should be cached", *calls)
	}
	// A branch switch is news, though.
	deliverDiff(t, a, s, "feature/other")
	if *calls != 2 {
		t.Fatalf("a new branch should be asked about: %d", *calls)
	}
}

// A stale cache ages out.
func TestPRCacheAgesOut(t *testing.T) {
	a := testApp(t)
	calls := stubPR(t, `{"number":1,"state":"OPEN","url":"https://x.test/pull/1"}`, nil)
	s := a.cur()
	deliverDiff(t, a, s, "main")
	s.prAt = time.Now().Add(-prCheckEvery - time.Minute)
	deliverDiff(t, a, s, "main")
	if *calls != 2 {
		t.Fatalf("an aged answer should be re-asked: %d", *calls)
	}
}
