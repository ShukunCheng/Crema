package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/gitdiff"
)

// shape renders the diff as one character per line, so a test can state what
// it expects without repeating the text: space context, - removed, + added.
func shape(lines []editLines) string {
	var b strings.Builder
	for _, l := range lines {
		switch l.kind {
		case gitdiff.LineAdd:
			b.WriteByte('+')
		case gitdiff.LineDel:
			b.WriteByte('-')
		default:
			b.WriteByte(' ')
		}
	}
	return b.String()
}

func TestDiffStringsPairsChangesAndKeepsSharedLines(t *testing.T) {
	cases := []struct {
		name          string
		before, after string
		want          string
	}{
		{"one line replaced", "a\nb\nc", "a\nB\nc", " -+ "},
		// The line between two changes is shared, and stays context — the
		// thing a plain before/after split gets wrong.
		{"two changes around a shared line", "a\nb\nX\nc\nd", "a\nB\nX\nC\nd", " -+ -+ "},
		{"pure insertion", "a\nc", "a\nb\nc", " + "},
		{"pure deletion", "a\nb\nc", "a\nc", " - "},
		{"a whole new file", "", "one\ntwo", "++"},
		{"everything removed", "one\ntwo", "", "--"},
		{"nothing at all", "", "", ""},
		{"unchanged", "same", "same", " "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shape(diffStrings(c.before, c.after)); got != c.want {
				t.Fatalf("diffStrings(%q, %q) = %q, want %q", c.before, c.after, got, c.want)
			}
		})
	}
}

// The cheap comparison is only for edits too big to line up, and still has to
// produce a sane diff.
func TestTrimmedDiffKeepsTheEnds(t *testing.T) {
	got := shape(trimmedDiff([]string{"a", "b", "c"}, []string{"a", "B", "c"}))
	if got != " -+ " {
		t.Fatalf("shape = %q, want %q", got, " -+ ")
	}
}

func TestRenderEditDrawsTheChange(t *testing.T) {
	in := `{"file_path":"main.go","old_string":"one\ntwo","new_string":"one\nTWO"}`
	out := stripSGR(renderEdit("Edit", in, 40))
	for _, want := range []string{"main.go", "  one", "- two", "+ TWO"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}

// A Write is all addition; a MultiEdit is several edits in a row.
func TestRenderEditHandlesWriteAndMultiEdit(t *testing.T) {
	write := stripSGR(renderEdit("Write", `{"file_path":"a.txt","content":"x\ny"}`, 40))
	if !strings.Contains(write, "+ x") || !strings.Contains(write, "+ y") {
		t.Fatalf("a Write is all addition:\n%s", write)
	}
	multi := stripSGR(renderEdit("MultiEdit", `{"file_path":"a.go","edits":[
		{"old_string":"one","new_string":"1"},
		{"old_string":"two","new_string":"2"}]}`, 40))
	for _, want := range []string{"- one", "+ 1", "- two", "+ 2"} {
		if !strings.Contains(multi, want) {
			t.Fatalf("missing %q:\n%s", want, multi)
		}
	}
}

// Anything that isn't an edit is left to print as it came.
func TestRenderEditIgnoresOtherTools(t *testing.T) {
	if got := renderEdit("Bash", `{"command":"go test"}`, 40); got != "" {
		t.Fatalf("Bash is not an edit: %q", got)
	}
	if got := renderEdit("Edit", "not json at all", 40); got != "" {
		t.Fatalf("unparseable arguments must fall back: %q", got)
	}
	if got := renderEdit("Edit", `{"file_path":"a.go"}`, 40); got != "" {
		t.Fatalf("an edit with no text is not a diff: %q", got)
	}
}

// The block itself shows the diff rather than the arguments.
func TestRenderToolShowsAnEditAsADiff(t *testing.T) {
	in := `{"file_path":"main.go","old_string":"gone","new_string":"here"}`
	out := stripSGR(RenderTool("Edit", in, 50))
	if strings.Contains(out, "old_string") || strings.Contains(out, "{") {
		t.Fatalf("the arguments should not be on screen:\n%s", out)
	}
	if !strings.Contains(out, "- gone") || !strings.Contains(out, "+ here") {
		t.Fatalf("the change should be:\n%s", out)
	}
}
