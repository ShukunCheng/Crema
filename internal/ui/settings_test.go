package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestSettingsListsOnlyWhatTheBackendSupports(t *testing.T) {
	// codex has no plan mode and defers model choice to its own config
	s := NewSession(1, agent.NewCodex(), t.TempDir())
	out := NewSettings(s).View(72, 20)
	if strings.Contains(out, "plan only") {
		t.Fatalf("codex has no plan mode; it must not be offered:\n%s", out)
	}
	if !strings.Contains(out, "full access") {
		t.Fatalf("codex does support full access:\n%s", out)
	}
}

func TestSettingsShowsTheActiveChoices(t *testing.T) {
	s := NewSession(1, agent.NewMock(), t.TempDir())
	s.Permission = agent.PermissionFull
	out := NewSettings(s).View(72, 20)
	// exactly one marked permission and one marked model
	if got := strings.Count(out, "●"); got != 2 {
		t.Fatalf("want one active permission and one active model, got %d markers:\n%s", got, out)
	}
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "●") && strings.Contains(ln, "edits") {
			t.Fatalf("the marker is on the wrong permission: %q", ln)
		}
	}
}

func TestSettingsAppliesPermissionAndModel(t *testing.T) {
	tempState(t)
	a := testApp(t)
	a.Update(kmsg(tea.KeyCtrlP))
	if a.settings == nil {
		t.Fatal("ctrl+p must open settings")
	}
	// walk to "full access" and apply
	for i, r := range a.settings.rows {
		if r.perm == agent.PermissionFull {
			a.settings.idx = i
			break
		}
	}
	a.Update(kmsg(tea.KeyEnter))
	if a.cur().Permission != agent.PermissionFull {
		t.Fatalf("permission not applied: %q", a.cur().Permission)
	}
	if !strings.Contains(a.cur().tl.Content(), "full access") {
		t.Fatal("a permission change must be recorded in the conversation")
	}
	if a.settings == nil {
		t.Fatal("the modal stays open so several settings can be changed at once")
	}

	for i, r := range a.settings.rows {
		if r.kind == settingsModel && r.model == "demo-fast" {
			a.settings.idx = i
			break
		}
	}
	a.Update(kmsg(tea.KeyEnter))
	if a.cur().Model != "demo-fast" {
		t.Fatalf("model not applied: %q", a.cur().Model)
	}
	a.Update(kmsg(tea.KeyEsc))
	if a.settings != nil {
		t.Fatal("esc must close settings")
	}
}

func TestDownFromTheInputOpensSettings(t *testing.T) {
	a := testApp(t)
	if a.focus != focusInput {
		t.Fatal("focus starts on the input")
	}
	a.Update(kmsg(tea.KeyDown))
	if a.settings == nil {
		t.Fatal("down from the input must open the model/permission picker")
	}
	// the cursor lands on the active permission, so another down moves the list
	before := a.settings.idx
	a.Update(kmsg(tea.KeyDown))
	if a.settings.idx == before {
		t.Fatal("the panel navigates with the same key that opened it")
	}
}

func TestDownStillMovesTheCursorInAMultiLineDraft(t *testing.T) {
	a := testApp(t)
	a.in.ta.SetValue("first line\nsecond line")
	a.Update(kmsg(tea.KeyDown))
	if a.settings != nil {
		t.Fatal("down must stay cursor movement while a multi-line draft is open")
	}
}

func TestDownOutsideTheInputDoesNotOpenSettings(t *testing.T) {
	a := testApp(t)
	a.focus = focusTimeline
	a.Update(kmsg(tea.KeyDown))
	if a.settings != nil {
		t.Fatal("down scrolls the focused pane; only the input opens settings")
	}
}

func TestSettingsChoiceReachesTheBackend(t *testing.T) {
	a := testApp(t)
	a.cur().Permission = agent.PermissionFull
	a.cur().Model = "demo-fast"
	a.in.ta.SetValue("go")
	a.Update(kmsg(tea.KeyEnter))
	if got := a.cur().lastOpts.Permission; got != agent.PermissionFull {
		t.Fatalf("permission not passed to the run: %q", got)
	}
	if got := a.cur().lastOpts.Model; got != "demo-fast" {
		t.Fatalf("model not passed to the run: %q", got)
	}
}

func TestSettingsSurviveARestart(t *testing.T) {
	tempState(t)
	mk := fastMock()
	reg := regWith(mk)
	dir := t.TempDir()

	a := NewApp(reg, mk, dir)
	a.resize(140, 30)
	a.cur().SetPermission(agent.PermissionFull)
	a.cur().SetModel("demo-slow")
	a.persist()

	b := NewAppRestored(reg, LoadState(), mk, dir)
	if b.cur().Permission != agent.PermissionFull || b.cur().Model != "demo-slow" {
		t.Fatalf("settings not restored: %q / %q", b.cur().Permission, b.cur().Model)
	}
}

func TestRestoreDropsAModeTheBackendCannotHonor(t *testing.T) {
	tempState(t)
	mk := fastMock()
	dir := t.TempDir()
	a := NewApp(regWith(mk), mk, dir)
	a.resize(140, 30)
	a.persist()

	// hand-edit the saved state to a mode this backend never offers
	st := LoadState()
	st.Sessions[0].Permission = agent.PermissionPlan // mock has no plan mode
	if err := SaveState(st); err != nil {
		t.Fatal(err)
	}
	b := NewAppRestored(regWith(mk), LoadState(), mk, dir)
	if b.cur().Permission == agent.PermissionPlan {
		t.Fatal("an unsupported mode must not be restored")
	}
}

func TestSettingsViewIsExactlyTheRequestedBox(t *testing.T) {
	s := NewSession(1, agent.NewMock(), t.TempDir())
	st := NewSettings(s)
	for _, dims := range [][2]int{{72, 23}, {60, 14}, {40, 9}} {
		out := st.View(dims[0], dims[1])
		lines := strings.Split(out, "\n")
		if len(lines) != dims[1] {
			t.Fatalf("%dx%d: %d lines, want %d", dims[0], dims[1], len(lines), dims[1])
		}
		for _, ln := range lines {
			if lipgloss.Width(ln) > dims[0] {
				t.Fatalf("width %d exceeded (%d): %q", dims[0], lipgloss.Width(ln), ln)
			}
		}
	}
}

// TestSettingsRowAtMatchesWhatIsDrawn locks click targets to the rendering.
func TestSettingsRowAtMatchesWhatIsDrawn(t *testing.T) {
	s := NewSession(1, agent.NewMock(), t.TempDir())
	st := NewSettings(s)
	lines := strings.Split(stripANSIFull(st.View(72, 24)), "\n")
	// content starts one row in, past the box border
	for row := 0; row < len(lines)-2; row++ {
		i, ok := st.RowAt(row)
		if !ok {
			continue
		}
		if !strings.Contains(lines[row+1], st.rows[i].label) {
			t.Fatalf("row %d hit-tests to %q but renders %q", row, st.rows[i].label, lines[row+1])
		}
	}
}

func TestClickingASettingsRowAppliesIt(t *testing.T) {
	tempState(t)
	a := testApp(t)
	a.Update(kmsg(tea.KeyCtrlP))
	x, y, _, _ := a.modalRect()

	target := -1
	for i, r := range a.settings.rows {
		if r.perm == agent.PermissionFull {
			target = i
			break
		}
	}
	row := target + settingsHeaderRows
	a.Update(click(x+3, y+1+row))
	if a.cur().Permission != agent.PermissionFull {
		t.Fatalf("clicking a permission row must apply it, got %q", a.cur().Permission)
	}
}
