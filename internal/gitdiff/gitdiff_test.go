package gitdiff

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.name", "Crema Test")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func find(ds DiffSet, path string, staged bool) *File {
	for i := range ds.Files {
		if ds.Files[i].Path == path && ds.Files[i].Staged == staged {
			return &ds.Files[i]
		}
	}
	return nil
}

func TestCollectOutsideRepoReportsError(t *testing.T) {
	ds := Collect(t.TempDir())
	if ds.Err == "" {
		t.Fatal("want an Err for a non-repo directory")
	}
	if len(ds.Files) != 0 {
		t.Fatalf("want no files, got %d", len(ds.Files))
	}
}

func TestCollectStagedUnstagedUntracked(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\ntwo\n")
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-qm", "init")

	write(t, dir, "a.txt", "one\ntwo\nthree\n") // unstaged modification
	write(t, dir, "b.txt", "brand new\n")
	gitRun(t, dir, "add", "b.txt") // staged addition
	write(t, dir, "c.txt", "untracked\n")

	ds := Collect(dir)
	if ds.Err != "" {
		t.Fatalf("unexpected Err: %s", ds.Err)
	}
	if ds.Repo == "" {
		t.Fatal("Repo must be set inside a repository")
	}
	a := find(ds, "a.txt", false)
	if a == nil || a.Status != "modified" || a.Additions != 1 {
		t.Fatalf("a.txt unstaged: %+v", a)
	}
	if len(a.Hunks) == 0 || !strings.HasPrefix(a.Hunks[0].Header, "@@") {
		t.Fatalf("a.txt hunks: %+v", a.Hunks)
	}
	b := find(ds, "b.txt", true)
	if b == nil || b.Status != "added" || b.Additions != 1 {
		t.Fatalf("b.txt staged: %+v", b)
	}
	c := find(ds, "c.txt", false)
	if c == nil || c.Status != "untracked" || c.Additions != 1 {
		t.Fatalf("c.txt untracked: %+v", c)
	}
	if c.Hunks[0].Lines[0].Kind != LineAdd || c.Hunks[0].Lines[0].Text != "untracked" {
		t.Fatalf("untracked body: %+v", c.Hunks[0].Lines)
	}
	if ds.Additions != 3 {
		t.Fatalf("total additions = %d, want 3", ds.Additions)
	}
}

func TestCollectCleanRepoIsEmpty(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.txt", "x\n")
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-qm", "init")
	ds := Collect(dir)
	if ds.Err != "" || len(ds.Files) != 0 || ds.Additions != 0 {
		t.Fatalf("clean repo should be empty: %+v", ds)
	}
}

func TestCollectUntrackedBinaryIsNotedNotRendered(t *testing.T) {
	dir := newRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{0x00, 0x01, 0x02, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	ds := Collect(dir)
	f := find(ds, "blob.bin", false)
	if f == nil || !f.Binary || f.Note == "" || len(f.Hunks) != 0 {
		t.Fatalf("binary untracked: %+v", f)
	}
}

func TestParseUnifiedRename(t *testing.T) {
	raw := "diff --git a/old.txt b/new.txt\nsimilarity index 100%\nrename from old.txt\nrename to new.txt\n"
	files := ParseUnified(raw, true)
	if len(files) != 1 {
		t.Fatalf("files = %+v", files)
	}
	f := files[0]
	if f.Status != "renamed" || f.OldPath != "old.txt" || f.Path != "new.txt" || !f.Staged {
		t.Fatalf("rename: %+v", f)
	}
}

func TestParseUnifiedBinaryAndDeleted(t *testing.T) {
	raw := "diff --git a/img.png b/img.png\nnew file mode 100644\nindex 0000000..abc1234\nBinary files /dev/null and b/img.png differ\n" +
		"diff --git a/gone.txt b/gone.txt\ndeleted file mode 100644\nindex 1234567..0000000\n--- a/gone.txt\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-bye\n-now\n"
	files := ParseUnified(raw, false)
	if len(files) != 2 {
		t.Fatalf("files = %+v", files)
	}
	if !files[0].Binary || files[0].Status != "added" || files[0].Path != "img.png" {
		t.Fatalf("binary: %+v", files[0])
	}
	g := files[1]
	if g.Status != "deleted" || g.Path != "gone.txt" || g.Deletions != 2 || g.Additions != 0 {
		t.Fatalf("deleted: %+v", g)
	}
}

// The split view numbers its gutters from the @@ header, so the starting line
// of each side has to survive parsing.
func TestParseUnifiedKeepsHunkStartLines(t *testing.T) {
	raw := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n" +
		"@@ -14,7 +20,8 @@ func main() {\n context\n-old\n+new\n" +
		"@@ -1 +1 @@\n-a\n+b\n"
	hunks := ParseUnified(raw, false)[0].Hunks
	if len(hunks) != 2 {
		t.Fatalf("got %d hunks", len(hunks))
	}
	if hunks[0].OldStart != 14 || hunks[0].NewStart != 20 {
		t.Fatalf("hunk 0 starts %d/%d, want 14/20", hunks[0].OldStart, hunks[0].NewStart)
	}
	// A single-line hunk has no comma in its range.
	if hunks[1].OldStart != 1 || hunks[1].NewStart != 1 {
		t.Fatalf("hunk 1 starts %d/%d, want 1/1", hunks[1].OldStart, hunks[1].NewStart)
	}
	// The header itself still drops the trailing function context.
	if hunks[0].Header != "@@ -14,7 +20,8 @@" {
		t.Fatalf("header = %q", hunks[0].Header)
	}
}

func TestParseUnifiedDoesNotMistakeBodyLinesForHeaders(t *testing.T) {
	// A removed line whose content starts with "-- " renders as "--- " in the body.
	raw := "diff --git a/x.md b/x.md\nindex 1..2 100644\n--- a/x.md\n+++ b/x.md\n@@ -1,3 +1,3 @@\n context\n--- signature\n+++ new signature\n"
	files := ParseUnified(raw, false)
	f := files[0]
	if f.Path != "x.md" || f.Additions != 1 || f.Deletions != 1 {
		t.Fatalf("counts: %+v", f)
	}
	lines := f.Hunks[0].Lines
	if len(lines) != 3 || lines[1].Kind != LineDel || lines[1].Text != "-- signature" {
		t.Fatalf("body lines: %+v", lines)
	}
	if lines[2].Kind != LineAdd || lines[2].Text != "++ new signature" {
		t.Fatalf("add line: %+v", lines[2])
	}
}

// The status bar names the branch and counts what git isn't tracking yet.
func TestCollectReportsBranchAndUntrackedCount(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\n")
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-qm", "init")
	write(t, dir, "new1.txt", "x\n")
	write(t, dir, "new2.txt", "y\n")

	ds := Collect(dir)
	if ds.Branch != "main" {
		t.Fatalf("Branch = %q, want main", ds.Branch)
	}
	if ds.Untracked != 2 {
		t.Fatalf("Untracked = %d, want 2", ds.Untracked)
	}

	// A detached HEAD has no branch name, so the hash stands in for one.
	gitRun(t, dir, "checkout", "-q", "--detach")
	if b := Collect(dir).Branch; b == "" || b == "main" {
		t.Fatalf("detached HEAD should report a hash, got %q", b)
	}
}

// A repo with no commit yet has no branch to name and must not error out.
func TestCollectOnAnEmptyRepoHasNoBranch(t *testing.T) {
	ds := Collect(newRepo(t))
	if ds.Err != "" {
		t.Fatalf("an empty repo is not an error: %q", ds.Err)
	}
	if ds.Branch != "main" && ds.Branch != "" {
		t.Fatalf("Branch = %q", ds.Branch)
	}
}
