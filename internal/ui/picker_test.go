package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type unavailableAgent struct{ name string }

func (u unavailableAgent) Name() string     { return u.name }
func (u unavailableAgent) Label() string    { return u.name }
func (u unavailableAgent) Available() error { return errors.New(u.name + " CLI not found") }
func (u unavailableAgent) Modes() []agent.PermissionMode {
	return []agent.PermissionMode{agent.PermissionDefault, agent.PermissionAcceptEdits}
}
func (u unavailableAgent) Models() []string { return []string{agent.DefaultModel} }
func (u unavailableAgent) Run(context.Context, agent.RunOptions) (<-chan agent.Event, error) {
	return nil, errors.New("unavailable")
}

func TestPickerBackendThenDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	mk := agent.NewMock()
	p := NewPicker([]agent.Agent{mk}, root)

	if done, cancel := p.Update(kmsg(tea.KeyEnter)); done || cancel {
		t.Fatal("choosing a backend should only advance the stage")
	}
	if p.Stage() != stageDir {
		t.Fatal("expected the directory stage")
	}
	if len(p.entries) == 0 || p.entries[0] != useThisDir {
		t.Fatalf("the first row must select the current directory: %v", p.entries)
	}

	// descend into child, then accept it
	p.dIdx = indexOf(p.entries, "child")
	if p.dIdx < 0 {
		t.Fatalf("child dir not listed: %v", p.entries)
	}
	p.Update(kmsg(tea.KeyEnter))
	if filepath.Base(p.dir) != "child" {
		t.Fatalf("enter on a directory must descend: %q", p.dir)
	}
	done, cancel := p.Update(kmsg(tea.KeyEnter)) // row 0 is "use this directory"
	if !done || cancel {
		t.Fatalf("selecting the current directory must finish: done=%v cancel=%v", done, cancel)
	}
	gotAgent, gotDir := p.Result()
	if gotAgent != agent.Agent(mk) {
		t.Fatal("wrong backend returned")
	}
	if filepath.Base(gotDir) != "child" {
		t.Fatalf("wrong directory returned: %q", gotDir)
	}
}

func TestPickerGoesUpWithLeftAndBackWithEsc(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "sub")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	p := NewPicker([]agent.Agent{agent.NewMock()}, child)
	p.Update(kmsg(tea.KeyEnter)) // into the directory stage
	p.Update(kmsg(tea.KeyLeft))
	if p.dir != root {
		t.Fatalf("left must go to the parent: %q want %q", p.dir, root)
	}
	if _, cancel := p.Update(kmsg(tea.KeyEsc)); cancel {
		t.Fatal("esc on the directory step should go back, not cancel outright")
	}
	if p.Stage() != stageBackend {
		t.Fatal("esc should return to the backend list")
	}
	if _, cancel := p.Update(kmsg(tea.KeyEsc)); !cancel {
		t.Fatal("esc on the backend list should cancel")
	}
}

func TestPickerRefusesAnUninstalledBackend(t *testing.T) {
	p := NewPicker([]agent.Agent{unavailableAgent{name: "codex"}}, t.TempDir())
	done, cancel := p.Update(kmsg(tea.KeyEnter))
	if done || cancel {
		t.Fatal("an unavailable backend must not be selectable")
	}
	if p.Stage() != stageBackend {
		t.Fatal("should stay on the backend list")
	}
	if !strings.Contains(p.warn, "not found") {
		t.Fatalf("the reason must be shown: %q", p.warn)
	}
}

func TestPickerStartsOnTheFirstInstalledBackend(t *testing.T) {
	mk := agent.NewMock()
	p := NewPicker([]agent.Agent{unavailableAgent{name: "claude"}, mk}, t.TempDir())
	if p.bIdx != 1 {
		t.Fatalf("cursor should skip the uninstalled backend, got index %d", p.bIdx)
	}
}

func TestPickerViewIsExactlyTheRequestedBox(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a", "b", "c", "d", "e", "f"} {
		os.MkdirAll(filepath.Join(root, n), 0o755)
	}
	p := NewPicker([]agent.Agent{agent.NewMock()}, root)
	for _, dims := range [][2]int{{72, 23}, {60, 12}, {40, 8}, {30, 6}} {
		out := p.View(dims[0], dims[1])
		lines := strings.Split(out, "\n")
		if len(lines) != dims[1] {
			t.Fatalf("backend stage %dx%d: %d lines, want %d", dims[0], dims[1], len(lines), dims[1])
		}
		for _, ln := range lines {
			if lipgloss.Width(ln) > dims[0] {
				t.Fatalf("width %d exceeded (%d): %q", dims[0], lipgloss.Width(ln), ln)
			}
		}
	}
	p.Update(kmsg(tea.KeyEnter))
	p.warn = "a warning that must not push the box out of shape"
	for _, dims := range [][2]int{{72, 23}, {60, 12}, {40, 8}} {
		out := p.View(dims[0], dims[1])
		if got := len(strings.Split(out, "\n")); got != dims[1] {
			t.Fatalf("dir stage %dx%d: %d lines, want %d", dims[0], dims[1], got, dims[1])
		}
		// Trimming drops middle rows, never the footer. The text itself may be
		// clipped on a narrow box, which the "›" marker makes visible.
		if !strings.Contains(out, "esc") {
			t.Fatalf("the help line must survive a cramped box at %dx%d:\n%s", dims[0], dims[1], out)
		}
		if !strings.Contains(out, "warning") {
			t.Fatalf("the warning must survive a cramped box at %dx%d:\n%s", dims[0], dims[1], out)
		}
	}
	if out := p.View(72, 23); !strings.Contains(out, "esc back") {
		t.Fatalf("a roomy box should show the full help text:\n%s", out)
	}
}

func TestListDirsSkipsFilesAndHiddenEntries(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "visible"), 0o755)
	os.MkdirAll(filepath.Join(root, ".hidden"), 0o755)
	os.MkdirAll(filepath.Join(root, "node_modules"), 0o755)
	os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644)

	got, err := listDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	if indexOf(got, "visible") < 0 {
		t.Fatalf("real directory missing: %v", got)
	}
	for _, unwanted := range []string{".hidden", "node_modules", "file.txt"} {
		if indexOf(got, unwanted) >= 0 {
			t.Fatalf("%q should not be listed: %v", unwanted, got)
		}
	}
	if indexOf(got, "..") < 0 {
		t.Fatalf("parent entry missing: %v", got)
	}
}

func indexOf(xs []string, want string) int {
	for i, x := range xs {
		if x == want {
			return i
		}
	}
	return -1
}
