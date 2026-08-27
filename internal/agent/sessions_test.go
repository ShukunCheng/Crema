package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The slug rule is measured against directories the CLI actually created.
func TestProjectSlugMatchesTheCLIsOwn(t *testing.T) {
	for dir, want := range map[string]string{
		`D:\Crema`:         "D--Crema",
		`D:\fsp-horizon-3`: "D--fsp-horizon-3",
		`C:\Users\S C\AppData\Local\Temp\crema.probe`: "C--Users-S-C-AppData-Local-Temp-crema-probe",
	} {
		if got := projectSlug(dir); got != want {
			t.Fatalf("projectSlug(%q) = %q, want %q", dir, got, want)
		}
	}
}

func TestSessionsListsNewestFirstWithPreviews(t *testing.T) {
	home := t.TempDir()
	prev := homeDir
	homeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { homeDir = prev })

	dir := filepath.Join(home, ".claude", "projects", "D--Crema")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"type":"user","isMeta":true,"message":{"role":"user","content":"a caveat nobody typed"}}
{"type":"user","message":{"role":"user","content":[{"tool_use_id":"t1","type":"tool_result","content":"noise"}]}}
{"type":"user","message":{"role":"user","content":"fix the login bug\nwith details"}}
`
	fresh := `{"type":"queue-operation"}
{"type":"user","message":{"role":"user","content":"add dark mode"}}
`
	for name, body := range map[string]string{"1111.jsonl": old, "2222.jsonl": fresh} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	then := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "1111.jsonl"), then, then); err != nil {
		t.Fatal(err)
	}

	got := NewClaude().Sessions(`D:\Crema`)
	if len(got) != 2 {
		t.Fatalf("sessions = %+v", got)
	}
	if got[0].ID != "2222" || got[1].ID != "1111" {
		t.Fatalf("want newest first: %+v", got)
	}
	if got[0].Preview != "add dark mode" {
		t.Fatalf("preview = %q", got[0].Preview)
	}
	// The first *typed* line: not the meta caveat, not a tool result, and
	// only its first line.
	if got[1].Preview != "fix the login bug" {
		t.Fatalf("preview = %q", got[1].Preview)
	}
}

func TestSessionsWithNothingSavedIsEmpty(t *testing.T) {
	home := t.TempDir()
	prev := homeDir
	homeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { homeDir = prev })
	if got := NewClaude().Sessions(`D:\nowhere`); len(got) != 0 {
		t.Fatalf("sessions = %+v", got)
	}
}
