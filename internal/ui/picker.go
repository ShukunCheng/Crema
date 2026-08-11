package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type pickerStage int

const (
	stageBackend pickerStage = iota
	stageDir
)

// useThisDir is the synthetic first row of the directory list. It carries no
// marker of its own — the selection cursor supplies that.
const useThisDir = "use this directory"

// Picker is the new-agent modal: choose a backend, then browse to a working
// directory. It is a plain in-terminal browser rather than a native dialog so
// it still works over SSH.
type Picker struct {
	stage    pickerStage
	backends []agent.Agent
	bIdx     int

	dir     string
	entries []string // useThisDir, then "..", then subdirectories
	dIdx    int
	scroll  int
	warn    string

	chosen agent.Agent
}

// NewPicker lists every registered backend, marking unavailable ones but still
// showing them so the user learns why they can't be picked.
func NewPicker(backends []agent.Agent, startDir string) *Picker {
	p := &Picker{backends: backends, dir: startDir}
	for i, b := range backends { // land the cursor on the first usable backend
		if b.Available() == nil {
			p.bIdx = i
			break
		}
	}
	return p
}

// Header line counts, shared by View and RowAt so a click lands on the row it
// appears to: backend stage draws title+blank, directory stage title+path+blank.
const (
	pickerBackendHeaderRows = 2
	pickerDirHeaderRows     = 3
)

type pickerTarget int

const (
	pickerTargetNone pickerTarget = iota
	pickerTargetBackend
	pickerTargetEntry
)

func (p *Picker) Stage() pickerStage { return p.stage }

// RowAt maps a row inside the modal's content area to the item drawn there.
func (p *Picker) RowAt(row int) (pickerTarget, int) {
	if p.stage == stageBackend {
		if i := row - pickerBackendHeaderRows; i >= 0 && i < len(p.backends) {
			return pickerTargetBackend, i
		}
		return pickerTargetNone, 0
	}
	if row < pickerDirHeaderRows {
		return pickerTargetNone, 0
	}
	if i := p.scroll + row - pickerDirHeaderRows; i < len(p.entries) {
		return pickerTargetEntry, i
	}
	return pickerTargetNone, 0
}

// ClickRow moves the cursor to a row and activates it, exactly as enter would.
func (p *Picker) ClickRow(row int) (done, canceled bool) {
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	switch target, i := p.RowAt(row); target {
	case pickerTargetBackend:
		p.bIdx = i
		return p.updateBackend(enter)
	case pickerTargetEntry:
		p.dIdx = i
		return p.updateDir(enter)
	}
	return false, false
}

// Result is valid only once Update reports done.
func (p *Picker) Result() (agent.Agent, string) { return p.chosen, p.dir }

// Update handles one keypress. done means a backend+directory were chosen;
// canceled means the user backed all the way out.
func (p *Picker) Update(msg tea.KeyMsg) (done, canceled bool) {
	switch p.stage {
	case stageBackend:
		return p.updateBackend(msg)
	default:
		return p.updateDir(msg)
	}
}

func (p *Picker) updateBackend(msg tea.KeyMsg) (done, canceled bool) {
	switch msg.String() {
	case "esc", "ctrl+c":
		return false, true
	case "up", "k":
		if p.bIdx > 0 {
			p.bIdx--
		}
	case "down", "j":
		if p.bIdx < len(p.backends)-1 {
			p.bIdx++
		}
	case "enter":
		if len(p.backends) == 0 {
			return false, true
		}
		b := p.backends[p.bIdx]
		if err := b.Available(); err != nil {
			p.warn = err.Error()
			return false, false
		}
		p.chosen = b
		p.warn = ""
		p.stage = stageDir
		p.loadDir(p.dir)
	}
	return false, false
}

func (p *Picker) updateDir(msg tea.KeyMsg) (done, canceled bool) {
	switch msg.String() {
	case "ctrl+c":
		return false, true
	case "esc": // step back to the backend list rather than losing everything
		p.stage = stageBackend
		p.warn = ""
		return false, false
	case "up", "k":
		if p.dIdx > 0 {
			p.dIdx--
		}
	case "down", "j":
		if p.dIdx < len(p.entries)-1 {
			p.dIdx++
		}
	case "left", "h":
		p.loadDir(parentOf(p.dir))
	case "enter", "right", "l":
		if len(p.entries) == 0 {
			return false, false
		}
		switch e := p.entries[p.dIdx]; e {
		case useThisDir:
			return true, false
		case "..":
			p.loadDir(parentOf(p.dir))
		default:
			if isDriveRow(e) {
				p.loadDir(e)
			} else {
				p.loadDir(filepath.Join(p.dir, e))
			}
		}
	}
	return false, false
}

// loadDir moves to path, keeping the old listing if it cannot be read.
// The path is resolved to an absolute one first: a relative dir like "."
// makes parentOf believe it is already at a filesystem root.
func (p *Picker) loadDir(path string) {
	if path == "" {
		return
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	subs, err := listDirs(path)
	if err != nil {
		p.warn = err.Error()
		return
	}
	p.warn = ""
	p.dir = path
	p.entries = append([]string{useThisDir}, subs...)
	p.dIdx, p.scroll = 0, 0
}

func parentOf(path string) string {
	parent := filepath.Dir(path)
	if parent == path { // already at a root
		return path
	}
	return parent
}

// isDriveRow reports whether an entry is an absolute drive root like "E:\".
func isDriveRow(e string) bool {
	return filepath.IsAbs(e)
}

// listDirs returns "..", any Windows drive roots when at the top, and the
// readable subdirectories of path. Hidden and vendor-noise dirs are skipped.
func listDirs(path string) ([]string, error) {
	ents, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var out []string
	if parentOf(path) != path {
		out = append(out, "..")
	} else if runtime.GOOS == "windows" {
		out = append(out, otherDrives(path)...)
	}
	var dirs []string
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, ".") || n == "node_modules" {
			continue
		}
		dirs = append(dirs, n)
	}
	sort.Strings(dirs)
	return append(out, dirs...), nil
}

// otherDrives lists mounted Windows drives other than the current one, so the
// user isn't trapped on one drive when browsing up from a root.
func otherDrives(cur string) []string {
	var out []string
	for c := 'A'; c <= 'Z'; c++ {
		root := string(c) + `:\`
		if strings.EqualFold(root, cur) {
			continue
		}
		if _, err := os.Stat(root); err == nil {
			out = append(out, root)
		}
	}
	return out
}

// View renders the modal box: exactly h lines tall, at most w columns wide.
func (p *Picker) View(w, h int) string {
	if w < 12 || h < 5 {
		return strings.Repeat("\n", max(0, h-1))
	}
	// The box is w wide overall: border (2) + padding (2) + inner text.
	// Clipping to anything wider than inner would wrap and add a line.
	inner := w - 4
	content := h - 2
	head := lipgloss.NewStyle().Foreground(T.Pink).Bold(true)
	dim := lipgloss.NewStyle().Foreground(T.Muted)
	selSty := lipgloss.NewStyle().Foreground(T.Pink).Bold(true)
	warnSty := lipgloss.NewStyle().Foreground(T.Yellow)

	// footer is the warning (if any) plus a blank line and the help line
	footer := []string{"", dim.Render(clip(p.help(), inner))}
	if p.warn != "" {
		footer = append([]string{"", warnSty.Render(clip(p.warn, inner))}, footer...)
	}

	var lines []string
	if p.stage == stageBackend {
		lines = append(lines, head.Render(clip("new agent — pick a backend", inner)), "")
		for i, ba := range p.backends {
			label := ba.Label()
			if ba.Available() != nil {
				label += "  (not installed)"
			}
			switch {
			case i == p.bIdx:
				lines = append(lines, selSty.Render(clip("▸ "+label, inner)))
			case ba.Available() != nil:
				lines = append(lines, dim.Render(clip("  "+label, inner)))
			default:
				lines = append(lines, clip("  "+label, inner))
			}
		}
	} else {
		lines = append(lines,
			head.Render(clip("new "+p.chosen.Label()+" — pick a working directory", inner)),
			dim.Render(clip(p.dir, inner)), "")
		rows := max(1, content-len(lines)-len(footer))
		if p.dIdx < p.scroll {
			p.scroll = p.dIdx
		}
		if p.dIdx >= p.scroll+rows {
			p.scroll = p.dIdx - rows + 1
		}
		for i := p.scroll; i < min(len(p.entries), p.scroll+rows); i++ {
			e := p.entries[i]
			label := e
			switch {
			case e == useThisDir:
				label = "[ " + e + " ]" // set apart from the browsable rows
			case e != ".." && !isDriveRow(e):
				label = e + "/"
			}
			if i == p.dIdx {
				lines = append(lines, selSty.Render(clip("▸ "+label, inner)))
			} else {
				lines = append(lines, clip("  "+label, inner))
			}
		}
	}
	lines = append(lines, footer...)

	// Never exceed the box: a cramped terminal drops middle rows, not the help.
	if len(lines) > content {
		keep := max(0, content-len(footer))
		lines = append(lines[:keep], footer...)
		if len(lines) > content {
			lines = lines[:content]
		}
	}
	for len(lines) < content {
		lines = append(lines, "")
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(T.Purple).
		Padding(0, 1).
		Width(w - 2).Height(content).
		Render(strings.Join(lines, "\n"))
}

func (p *Picker) help() string {
	if p.stage == stageBackend {
		return "↑↓ move · enter choose · esc cancel"
	}
	return "↑↓ move · enter open · ← up · esc back"
}
