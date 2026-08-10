// Package gitdiff collects the working-tree state by shelling out to git and
// parsing unified diffs. It never mutates the repository.
package gitdiff

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	maxUntrackedBytes = 256 * 1024
	gitTimeout        = 10 * time.Second
)

// execCommand is a seam for tests that need to stub git.
var execCommand = exec.CommandContext

type LineKind int

const (
	LineContext LineKind = iota
	LineAdd
	LineDel
)

type Line struct {
	Kind LineKind
	Text string
}

type Hunk struct {
	Header string
	Lines  []Line
}

type File struct {
	Path      string
	OldPath   string
	Status    string
	Staged    bool
	Binary    bool
	Additions int
	Deletions int
	Hunks     []Hunk
	Note      string
}

type DiffSet struct {
	Repo      string
	Files     []File
	Additions int
	Deletions int
	Err       string
}

// Collect gathers staged, unstaged, and untracked changes for the repo at dir.
func Collect(dir string) DiffSet {
	var ds DiffSet
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		ds.Err = "not a git repository (diff panel needs one)"
		return ds
	}
	ds.Repo = strings.TrimSpace(top)

	staged, err := git(dir, "diff", "--cached", "--no-color", "--no-ext-diff", "-M", "--unified=3")
	if err != nil {
		ds.Err = err.Error()
		return ds
	}
	unstaged, err := git(dir, "diff", "--no-color", "--no-ext-diff", "-M", "--unified=3")
	if err != nil {
		ds.Err = err.Error()
		return ds
	}
	ds.Files = append(ds.Files, ParseUnified(staged, true)...)
	ds.Files = append(ds.Files, ParseUnified(unstaged, false)...)

	if others, err := git(dir, "ls-files", "--others", "--exclude-standard", "-z"); err == nil {
		for _, rel := range strings.Split(others, "\x00") {
			if rel != "" {
				ds.Files = append(ds.Files, untracked(ds.Repo, rel))
			}
		}
	}
	for _, f := range ds.Files {
		ds.Additions += f.Additions
		ds.Deletions += f.Deletions
	}
	return ds
}

func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	full := append([]string{"-c", "core.quotepath=false"}, args...)
	cmd := execCommand(ctx, "git", full...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out.String(), nil
}

func untracked(repo, rel string) File {
	f := File{Path: rel, Status: "untracked"}
	abs := filepath.Join(repo, filepath.FromSlash(rel))
	info, err := os.Stat(abs)
	if err != nil {
		f.Note = "untracked (unreadable: " + err.Error() + ")"
		return f
	}
	if info.Size() > maxUntrackedBytes {
		f.Note = fmt.Sprintf("untracked, %.1f KB — too large to render", float64(info.Size())/1024)
		return f
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		f.Note = "untracked (unreadable: " + err.Error() + ")"
		return f
	}
	if bytes.IndexByte(data, 0) >= 0 {
		f.Binary = true
		f.Note = "untracked binary file"
		return f
	}
	body := strings.TrimSuffix(string(data), "\n")
	if body == "" {
		f.Note = "untracked, empty file"
		return f
	}
	var h Hunk
	rows := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for _, r := range rows {
		h.Lines = append(h.Lines, Line{Kind: LineAdd, Text: r})
	}
	h.Header = "@@ -0,0 +1," + strconv.Itoa(len(rows)) + " @@"
	f.Hunks = []Hunk{h}
	f.Additions = len(rows)
	return f
}

// ParseUnified turns `git diff` output into Files. Unknown header lines are ignored.
func ParseUnified(raw string, staged bool) []File {
	raw = strings.TrimSuffix(raw, "\n")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var files []File
	cur, hi := -1, -1
	for _, ln := range strings.Split(raw, "\n") {
		switch {
		case strings.HasPrefix(ln, "diff --git "):
			old, nw := splitDiffGitPaths(strings.TrimPrefix(ln, "diff --git "))
			files = append(files, File{Path: nw, OldPath: old, Status: "modified", Staged: staged})
			cur, hi = len(files)-1, -1
		case cur < 0:
			// preamble before the first file header
		case strings.HasPrefix(ln, "@@"):
			files[cur].Hunks = append(files[cur].Hunks, Hunk{Header: hunkHeader(ln)})
			hi = len(files[cur].Hunks) - 1
		case hi < 0 && strings.HasPrefix(ln, "--- "):
			if p := strings.TrimPrefix(ln, "--- "); p != "/dev/null" {
				files[cur].OldPath = unquote(strings.TrimPrefix(p, "a/"))
			}
		case hi < 0 && strings.HasPrefix(ln, "+++ "):
			if p := strings.TrimPrefix(ln, "+++ "); p != "/dev/null" {
				files[cur].Path = unquote(strings.TrimPrefix(p, "b/"))
			}
		case hi < 0 && strings.HasPrefix(ln, "new file mode"):
			files[cur].Status = "added"
		case hi < 0 && strings.HasPrefix(ln, "deleted file mode"):
			files[cur].Status = "deleted"
		case hi < 0 && strings.HasPrefix(ln, "rename from "):
			files[cur].OldPath = unquote(strings.TrimPrefix(ln, "rename from "))
			files[cur].Status = "renamed"
		case hi < 0 && strings.HasPrefix(ln, "rename to "):
			files[cur].Path = unquote(strings.TrimPrefix(ln, "rename to "))
			files[cur].Status = "renamed"
		case hi < 0 && (strings.HasPrefix(ln, "Binary files ") || strings.HasPrefix(ln, "GIT binary patch")):
			files[cur].Binary = true
			files[cur].Note = "binary file"
		case hi < 0:
			// index/mode/similarity lines
		case strings.HasPrefix(ln, "+"):
			files[cur].Hunks[hi].Lines = append(files[cur].Hunks[hi].Lines, Line{Kind: LineAdd, Text: ln[1:]})
			files[cur].Additions++
		case strings.HasPrefix(ln, "-"):
			files[cur].Hunks[hi].Lines = append(files[cur].Hunks[hi].Lines, Line{Kind: LineDel, Text: ln[1:]})
			files[cur].Deletions++
		case strings.HasPrefix(ln, `\`):
			// "\ No newline at end of file"
		default:
			files[cur].Hunks[hi].Lines = append(files[cur].Hunks[hi].Lines,
				Line{Kind: LineContext, Text: strings.TrimPrefix(ln, " ")})
		}
	}
	return files
}

// hunkHeader keeps "@@ -a,b +c,d @@" and drops the trailing function context,
// which is often too wide for the diff pane.
func hunkHeader(ln string) string {
	if i := strings.Index(ln[2:], "@@"); i >= 0 {
		return ln[:i+4]
	}
	return ln
}

func splitDiffGitPaths(s string) (old, nw string) {
	if i := strings.Index(s, " b/"); i > 0 {
		return unquote(strings.TrimPrefix(s[:i], "a/")), unquote(s[i+3:])
	}
	return "", s
}

func unquote(p string) string {
	if strings.HasPrefix(p, `"`) {
		if s, err := strconv.Unquote(p); err == nil {
			return s
		}
	}
	return p
}
