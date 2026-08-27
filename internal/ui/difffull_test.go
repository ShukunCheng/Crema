package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/gitdiff"
	"github.com/charmbracelet/lipgloss"
)

// fullPanel is a diff pane with the whole frame and something in it.
func fullPanel(t *testing.T, w, h int) *DiffPanel {
	t.Helper()
	d := NewDiffPanel(w, h)
	d.SetDiff(sampleDiff())
	d.SetView(DiffFull)
	return d
}

// Full screen is a browser: the files down one side, the one you picked on the
// other — not one stack of everything.
func TestFullScreenListsTheFilesBesideTheOnePicked(t *testing.T) {
	d := fullPanel(t, 120, 20)
	if !d.Browsing() {
		t.Fatal("120 columns is room enough for the browser")
	}
	view := stripSGR(d.View())
	for _, want := range []string{"staged.go", "work.go", "notes.md", "STAGED", "UNTRACKED"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the list is missing %q:\n%s", want, view)
		}
	}
	// The body is the first file's diff, with no second copy of its header:
	// the list already said which file this is.
	if !strings.Contains(view, "package main") {
		t.Fatalf("the picked file's diff should be beside it:\n%s", view)
	}
	if strings.Count(view, "staged.go") != 1 {
		t.Fatalf("the file is named once, in the list:\n%s", view)
	}
	if strings.Contains(view, "old") {
		t.Fatalf("only the picked file is shown:\n%s", view)
	}
}

// The arrows walk the list, and the diff follows.
func TestMovingThroughTheFileList(t *testing.T) {
	d := fullPanel(t, 120, 20)
	if f, _ := d.Selected(); f.Path != "staged.go" {
		t.Fatalf("starts on the first file, got %q", f.Path)
	}
	d.SelectFile(1)
	f, _ := d.Selected()
	if f.Path != "work.go" {
		t.Fatalf("Selected = %q", f.Path)
	}
	if !strings.Contains(stripSGR(d.View()), "old") {
		t.Fatal("the body should be showing work.go now")
	}
	d.SelectFile(-1)
	if f, _ := d.Selected(); f.Path != "staged.go" {
		t.Fatalf("back up: %q", f.Path)
	}
	d.SelectFile(-1) // wraps rather than stopping
	if f, _ := d.Selected(); f.Path != "notes.md" {
		t.Fatalf("wrapped to %q", f.Path)
	}
}

// Clicking a file picks it; clicking a section heading is not a file.
func TestClickingAFileInTheList(t *testing.T) {
	d := fullPanel(t, 120, 20)
	rows := d.listRows() // ── STAGED, staged.go, ── UNSTAGED, work.go, …
	if !d.SelectFileAt(3) {
		t.Fatalf("row 3 should be a file: %q", stripSGR(strings.Join(rows, "\n")))
	}
	if f, _ := d.Selected(); f.Path != "work.go" {
		t.Fatalf("clicked row 3, got %q", f.Path)
	}
	if d.SelectFileAt(0) {
		t.Fatal("row 0 is a section heading, not a file")
	}
	if d.SelectFileAt(99) {
		t.Fatal("a row past the end is not a file")
	}
}

// The file column only exists while the browser does, and only its own columns
// belong to it.
func TestTheFileColumnOwnsItsOwnColumns(t *testing.T) {
	d := fullPanel(t, 120, 20)
	if !d.InFileList(0) || !d.InFileList(listW-1) {
		t.Fatal("the list owns its columns")
	}
	if d.InFileList(listW) || d.InFileList(-1) {
		t.Fatal("and no others")
	}
	if got := d.BodyCol(listW + 1); got != 0 {
		t.Fatalf("the body starts after the divider, got column %d", got)
	}
	d.SetView(DiffSide)
	if d.InFileList(0) {
		t.Fatal("there is no list beside the conversation")
	}
	if got := d.BodyCol(3); got != 3 {
		t.Fatalf("and no offset either, got %d", got)
	}
}

// Too narrow for two columns and a readable diff: the browser stands down
// rather than squeezing both.
func TestANarrowFullScreenFallsBackToTheStack(t *testing.T) {
	d := NewDiffPanel(browserMinW-1, 20)
	d.SetDiff(sampleDiff())
	d.SetView(DiffFull)
	if d.Browsing() {
		t.Fatal("there is no room for the list here")
	}
	if !strings.Contains(stripSGR(d.View()), "work.go") {
		t.Fatal("the stacked view should still show every file")
	}
}

// Added is green and removed is red wherever the two are counted — the list,
// and the file headers in the stacked view.
func TestTheCountsAreGreenAndRed(t *testing.T) {
	restoreTheme(t)
	SetMode(ModeDark)
	inColor(t)

	list := strings.Join(fullPanel(t, 120, 20).listRows(), "\n")
	if !strings.Contains(list, "38;2;"+rgb(T.Green)) || !strings.Contains(list, "38;2;"+rgb(T.Red)) {
		t.Fatalf("the list's counts should be coloured: %q", list)
	}
	header := diffFileHeader(sampleDiff().Files[1], 60, true)[0].text
	if !strings.Contains(header, "38;2;"+rgb(T.Green)) {
		t.Fatalf("+1 should be green: %q", header)
	}
	if !strings.Contains(header, "38;2;"+rgb(T.Red)) {
		t.Fatalf("−1 should be red: %q", header)
	}
}

// Searching in the browser goes to the file the text is in, since only one
// file is on screen at a time.
func TestSearchingInTheBrowserGoesToTheFile(t *testing.T) {
	d := fullPanel(t, 120, 20)
	d.StartSearch()
	for _, r := range "old" { // only in work.go
		d.SearchKey(runeKey(r))
	}
	if f, _ := d.Selected(); f.Path != "work.go" {
		t.Fatalf("the search should have gone to work.go, sitting on %q", f.Path)
	}
	if len(d.search.hits) == 0 {
		t.Fatal("and found it there")
	}
}

// The pane is exactly as tall as it was told, header and all.
func TestTheBrowserKeepsThePanesShape(t *testing.T) {
	for _, size := range [][2]int{{120, 20}, {80, 10}, {50, 6}} {
		d := NewDiffPanel(size[0], size[1])
		d.SetDiff(sampleDiff())
		d.SetView(DiffFull)
		lines := strings.Split(d.View(), "\n")
		if len(lines) != size[1] {
			t.Fatalf("%dx%d: %d rows", size[0], size[1], len(lines))
		}
		for i, ln := range lines {
			if w := lipgloss.Width(ln); w > size[0] {
				t.Fatalf("%dx%d: row %d is %d wide", size[0], size[1], i, w)
			}
		}
	}
}

// A refresh that removes the file being looked at must not leave the browser
// pointing past the end of the list.
func TestTheSelectionSurvivesTheFileGoingAway(t *testing.T) {
	d := fullPanel(t, 120, 20)
	d.SelectFile(2) // notes.md, the last one
	d.SetDiff(gitdiff.DiffSet{Repo: "/repo", Files: sampleDiff().Files[:1]})
	if f, ok := d.Selected(); !ok || f.Path != "staged.go" {
		t.Fatalf("Selected = %+v, %v", f, ok)
	}
	_ = d.View() // must not panic
}
