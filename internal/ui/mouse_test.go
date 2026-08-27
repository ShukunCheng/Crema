package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func click(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

// clickPR sends a full press+release, which is what a real click is. The two
// text panes act on release so a press can still become a drag.
func clickPR(a *App, x, y int) {
	a.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	a.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
}

func wheel(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown}
}

// sidebarRowY is the screen row of a sidebar content row (1 accounts for the border).
func sidebarRowY(contentRow int) int { return contentRow + 1 }

// TestSidebarRowAtMatchesWhatIsDrawn locks the hit-testing to the renderer, so
// a layout change in one cannot silently misroute clicks in the other.
func TestSidebarRowAtMatchesWhatIsDrawn(t *testing.T) {
	sessions := sidebarSessions(t)
	lines := strings.Split(RenderSidebar(sessions, 0, noDrag, "⠋", SidebarWidth-2, 10), "\n")

	for row, ln := range lines {
		target, idx := SidebarRowAt(len(sessions), row)
		switch target {
		case SidebarSession:
			want := sessions[idx].Title()
			// the row is clipped, so compare on the directory name
			base := want[strings.LastIndex(want, " ")+1:]
			if !strings.Contains(ln, base) {
				t.Fatalf("row %d maps to session %d (%q) but renders %q", row, idx, want, ln)
			}
		case SidebarNewAgent:
			if !strings.Contains(ln, NewAgentRow) {
				t.Fatalf("row %d maps to the new-agent row but renders %q", row, ln)
			}
		default:
			for _, s := range sessions {
				b := s.Title()[strings.LastIndex(s.Title(), " ")+1:]
				if strings.Contains(ln, b) {
					t.Fatalf("row %d renders session %q but hit-tests as nothing", row, ln)
				}
			}
			if strings.Contains(ln, NewAgentRow) {
				t.Fatalf("row %d renders the new-agent row but hit-tests as nothing", row)
			}
		}
	}
}

func TestClickingASidebarRowSwitchesAgent(t *testing.T) {
	a := testApp(t)
	a.addSession(fastMock(), t.TempDir())
	a.addSession(fastMock(), t.TempDir())
	a.selectSession(0)

	a.Update(click(3, sidebarRowY(SidebarRowOf(2))))
	if a.active != 2 {
		t.Fatalf("clicking the third row should focus agent 3, got %d", a.active)
	}
	a.Update(click(3, sidebarRowY(SidebarRowOf(1))))
	if a.active != 1 {
		t.Fatalf("clicking the second row should focus agent 2, got %d", a.active)
	}
}

// Every agent's row carries a × at its right edge, and clicking it closes
// that agent — not the focused one.
func TestClickingTheCloseButtonClosesThatAgent(t *testing.T) {
	a := testApp(t)
	a.addSession(fastMock(), t.TempDir())
	a.addSession(fastMock(), t.TempDir())
	a.selectSession(2)
	gone := a.sessions[0].Title()

	if !strings.Contains(RenderSidebar(a.sessions, 2, noDrag, "⠋", SidebarWidth-2, 10), "×") {
		t.Fatal("each row needs a visible close button")
	}

	closeX := 1 + SidebarCloseCol(a.lay.SidebarW-2) // 1 for the border
	a.Update(click(closeX, sidebarRowY(SidebarRowOf(0))))

	if len(a.sessions) != 2 {
		t.Fatalf("expected one agent closed, have %d", len(a.sessions))
	}
	for _, s := range a.sessions {
		if s.Title() == gone {
			t.Fatalf("%q should have been the one closed", gone)
		}
	}
	// The agent being looked at is still the one being looked at, even though
	// its position moved down by one.
	if a.active != 1 {
		t.Fatalf("active = %d, want the same agent as before at its new index", a.active)
	}
	if !strings.Contains(a.note, "closed") {
		t.Fatalf("note = %q, want it to say what happened", a.note)
	}
}

// A click on the name is still a selection, not a close.
func TestClickingTheNameSelectsRatherThanCloses(t *testing.T) {
	a := testApp(t)
	a.addSession(fastMock(), t.TempDir())
	a.selectSession(0)
	a.Update(click(3, sidebarRowY(SidebarRowOf(1))))
	if len(a.sessions) != 2 {
		t.Fatalf("clicking the name must not close: %d agents left", len(a.sessions))
	}
	if a.active != 1 {
		t.Fatalf("active = %d, want the clicked row", a.active)
	}
}

// Closing the last agent quits, which is what ctrl+w has always done.
func TestClosingTheLastAgentQuits(t *testing.T) {
	a := testApp(t)
	closeX := 1 + SidebarCloseCol(a.lay.SidebarW-2)
	_, cmd := a.Update(click(closeX, sidebarRowY(SidebarRowOf(0))))
	if cmd == nil {
		t.Fatal("closing the only agent must quit")
	}
	if got, want := cmd(), tea.Quit(); got != want {
		t.Fatalf("returned %T, want %T", got, want)
	}
}

func TestClickingNewAgentOpensThePicker(t *testing.T) {
	a := testApp(t)
	if a.picker != nil {
		t.Fatal("picker should start closed")
	}
	row := sidebarTitleRows + len(a.sessions) + sidebarGapRows
	a.Update(click(3, sidebarRowY(row)))
	if a.picker == nil {
		t.Fatal("clicking + new agent must open the picker")
	}
}

func TestClicksOutsideTheSidebarDoNotSwitchAgents(t *testing.T) {
	a := testApp(t)
	a.addSession(fastMock(), t.TempDir())
	a.selectSession(0)
	// the timeline, well to the right of the sidebar
	a.Update(click(a.lay.SidebarW+10, 5))
	if a.active != 0 {
		t.Fatal("clicking the timeline must not change the focused agent")
	}
	// the sidebar's own top border
	a.Update(click(3, 0))
	if a.active != 0 {
		t.Fatal("clicking the border must do nothing")
	}
}

func TestWheelStillScrollsAndDoesNotSelect(t *testing.T) {
	a := testApp(t)
	a.addSession(fastMock(), t.TempDir())
	a.selectSession(0)
	// A wheel event is also an "Action press"; it must not be treated as a click.
	a.Update(wheel(3, sidebarRowY(SidebarRowOf(1))))
	if a.active != 0 {
		t.Fatal("the scroll wheel over the sidebar must not switch agents")
	}
}

func TestClickingAPickerRowChoosesIt(t *testing.T) {
	a := testApp(t)
	target := t.TempDir()
	a.Update(kmsg(tea.KeyCtrlN))

	x, y, _, _ := a.modalRect()
	// row 0 is the title, row 1 blank, row 2 the first backend
	a.Update(click(x+3, y+1+pickerBackendHeaderRows))
	if a.picker == nil || a.picker.Stage() != stageDir {
		t.Fatal("clicking a backend must advance to the directory step")
	}

	a.picker.loadDir(target)
	// the first directory row is "[ use this directory ]"
	a.Update(click(x+3, y+1+pickerDirHeaderRows))
	if a.picker != nil {
		t.Fatal("clicking 'use this directory' must close the picker")
	}
	if len(a.sessions) != 2 || a.sessions[1].Dir != target {
		t.Fatalf("a new agent should exist in %q: %+v", target, a.sessions)
	}
}

func TestClickingATimelineToolHeaderFoldsIt(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.tl.Append(Block{Kind: BlockTool, Name: "Bash", Text: "line one\nline two\nline three"})
	full := s.tl.Content()
	if !strings.Contains(full, "line two") {
		t.Fatal("the tool body should start expanded")
	}

	// find the header's screen row: content line of the last block's first line
	header := strings.Count(strings.TrimRight(s.tl.Content(), "\n"), "\n")
	for i := 0; i <= header; i++ {
		if s.tl.HeaderBlockAt(i) == s.tl.Len()-1 {
			header = i
			break
		}
	}
	clickPR(a, a.lay.SidebarW+2, header-s.tl.YOffset())

	folded := s.tl.Content()
	if strings.Contains(folded, "line two") {
		t.Fatalf("clicking the header should fold the body:\n%s", folded)
	}
	if !strings.Contains(folded, "▸ Ran 1 shell command") {
		t.Fatalf("a folded block leaves a summary line behind:\n%s", folded)
	}
	clickPR(a, a.lay.SidebarW+2, header-s.tl.YOffset())
	if !strings.Contains(s.tl.Content(), "line two") {
		t.Fatal("clicking again must expand it")
	}
}

func TestClickingATimelineBodyLineDoesNotFold(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.tl.Append(Block{Kind: BlockTool, Name: "Bash", Text: "aaa\nbbb\nccc"})
	before := s.tl.Content()
	// row 0 of the pane is the very first content line, which is a system block
	clickPR(a, a.lay.SidebarW+2, 0)
	if s.tl.Content() != before {
		t.Fatal("clicking a non-collapsible block must not change anything")
	}
}

func TestClickingADiffFileHeaderOpensThatFile(t *testing.T) {
	a := testApp(t)
	s := a.cur()
	s.dp.SetDiff(sampleDiff())
	if strings.Contains(joinRows(s.dp.rows), "package main") {
		t.Fatal("files arrive folded")
	}

	row := -1
	for i := range s.dp.rows {
		if s.dp.rows[i].header {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatal("no file header row found")
	}
	if key := s.dp.HeaderFileAt(row); key == "" {
		t.Fatal("the header row must hit-test to a file")
	}

	diffX := a.lay.SidebarW + a.lay.TimelineW + 1
	clickPR(a, diffX, row-s.dp.YOffset()+2) // border, then the pane's own header

	if !strings.Contains(joinRows(s.dp.rows), "package main") {
		t.Fatal("clicking the file header should show its hunks")
	}
	if !strings.Contains(joinRows(s.dp.rows), "▾") {
		t.Fatal("an open file's header must be marked open")
	}
	clickPR(a, diffX, row-s.dp.YOffset()+2)
	if strings.Contains(joinRows(s.dp.rows), "package main") {
		t.Fatal("clicking again must fold it back")
	}
}

// What the user opened stays open when the agent touches the tree again.
func TestOpenedFilesSurviveADiffRefresh(t *testing.T) {
	d := NewDiffPanel(50, 20)
	d.SetDiff(sampleDiff())
	if strings.Contains(joinRows(d.rows), "package main") {
		t.Fatal("files arrive folded")
	}
	d.ToggleCollapse(DiffFileKey(sampleDiff().Files[0]))
	if !strings.Contains(joinRows(d.rows), "package main") {
		t.Fatal("the file should be open")
	}
	d.SetDiff(sampleDiff()) // the agent touched something; the pane refreshes
	if !strings.Contains(joinRows(d.rows), "package main") {
		t.Fatal("a refresh must not fold away what the user opened")
	}
}

func TestClickingAPaneMovesFocus(t *testing.T) {
	a := testApp(t)
	if a.focus != focusInput {
		t.Fatal("focus starts on the input")
	}
	clickPR(a, a.lay.SidebarW+2, 2)
	if a.focus != focusTimeline {
		t.Fatal("clicking the timeline should focus it")
	}
	clickPR(a, a.lay.SidebarW+a.lay.TimelineW+3, 3)
	if a.focus != focusDiff {
		t.Fatal("clicking the diff pane should focus it")
	}
	a.Update(click(10, a.lay.PaneH+1))
	if a.focus != focusInput {
		t.Fatal("clicking the input box should focus it")
	}
}

func TestClickingOutsideThePickerBoxIsIgnored(t *testing.T) {
	a := testApp(t)
	a.Update(kmsg(tea.KeyCtrlN))
	x, y, _, _ := a.modalRect()
	a.Update(click(max(0, x-3), y+1+pickerBackendHeaderRows)) // left of the box
	if a.picker == nil {
		t.Fatal("a click outside the modal must not choose anything")
	}
	if a.picker.Stage() != stageBackend {
		t.Fatal("the stage must not advance from an outside click")
	}
}
