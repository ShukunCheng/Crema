package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/gitdiff"
	"github.com/charmbracelet/lipgloss"
)

func sampleDiff() gitdiff.DiffSet {
	return gitdiff.DiffSet{
		Repo: "/repo",
		Files: []gitdiff.File{
			{Path: "staged.go", Status: "added", Staged: true, Additions: 1,
				Hunks: []gitdiff.Hunk{{Header: "@@ -0,0 +1 @@", Lines: []gitdiff.Line{{Kind: gitdiff.LineAdd, Text: "package main"}}}}},
			{Path: "work.go", Status: "modified", Additions: 1, Deletions: 1,
				Hunks: []gitdiff.Hunk{{Header: "@@ -1,2 +1,2 @@", Lines: []gitdiff.Line{
					{Kind: gitdiff.LineContext, Text: "ctx"},
					{Kind: gitdiff.LineDel, Text: "old"},
					{Kind: gitdiff.LineAdd, Text: "new"},
				}}}},
			{Path: "notes.md", Status: "untracked", Note: "untracked binary file", Binary: true},
		},
		Additions: 2, Deletions: 1,
	}
}

func TestRenderDiffSetGroupsAndMarksLines(t *testing.T) {
	out := RenderDiffSet(sampleDiff(), 50)
	for _, want := range []string{"STAGED", "UNSTAGED", "UNTRACKED", "staged.go", "work.go", "notes.md",
		"@@ -1,2 +1,2 @@", "+new", "-old", "untracked binary file"} {
		if !strings.Contains(out, want) {
			t.Fatalf("diff render missing %q:\n%s", want, out)
		}
	}
}

func TestRenderDiffSetNeverExceedsWidth(t *testing.T) {
	ds := sampleDiff()
	ds.Files[1].Hunks[0].Lines[0].Text = strings.Repeat("verylongtoken", 40)
	for _, w := range []int{20, 34, 80} {
		for _, ln := range strings.Split(RenderDiffSet(ds, w), "\n") {
			if lipgloss.Width(ln) > w {
				t.Fatalf("width %d exceeded (%d): %q", w, lipgloss.Width(ln), ln)
			}
		}
	}
}

func TestRenderDiffSetEmptyAndError(t *testing.T) {
	if out := RenderDiffSet(gitdiff.DiffSet{Repo: "/r"}, 40); !strings.Contains(out, "clean") {
		t.Fatalf("empty diff should say clean: %q", out)
	}
	out := RenderDiffSet(gitdiff.DiffSet{Err: "not a git repository (diff panel needs one)"}, 40)
	if !strings.Contains(out, "not a git repository") {
		t.Fatalf("error must surface: %q", out)
	}
}
