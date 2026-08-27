package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as a fake CLI: when CREMA_FAKE_CLI is set the test binary
// re-executes itself as the subprocess under test instead of running tests.
func TestMain(m *testing.M) {
	if mode := os.Getenv("CREMA_FAKE_CLI"); mode != "" {
		fakeCLI(mode)
		os.Exit(0)
	}
	// Usage is read from a file in the user's config directory. Point it
	// somewhere harmless: a test must neither read what is really there nor
	// leave a fabricated percentage behind for the real crema to believe.
	tmp, err := os.MkdirTemp("", "crema-test-usage")
	if err != nil {
		panic(err)
	}
	usagePathOverride = filepath.Join(tmp, "usage.json")

	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

func fakeCLI(mode string) {
	switch mode {
	case "stream":
		data, _ := os.ReadFile(os.Getenv("CREMA_FAKE_FIXTURE"))
		os.Stdout.Write(data)
	case "hang":
		fmt.Println("first")
		time.Sleep(60 * time.Second)
	case "stderr-flood":
		os.Stderr.Write(bytes.Repeat([]byte("x"), 1<<20)) // 1 MB, like codex's tracing
		fmt.Println("first")
	case "fail":
		fmt.Fprintln(os.Stderr, "boom: credential expired")
		os.Exit(3)
	}
}

// stubParser maps any line to KindText; the line "END" becomes KindTurnEnd.
type stubParser struct{ sid string }

func (s *stubParser) SessionID() string { return s.sid }

func (s *stubParser) ParseLine(line []byte) []Event {
	txt := strings.TrimSpace(string(line))
	if txt == "END" {
		return []Event{{Kind: KindTurnEnd, Result: &TurnResult{SessionID: s.sid}}}
	}
	return []Event{{Kind: KindText, Text: txt}}
}

func fakeEnv(mode string, kv ...string) []string {
	return append([]string{"CREMA_FAKE_CLI=" + mode}, kv...)
}

func TestRunCLIStreamsAndClosesAfterTurnEnd(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(fixture, []byte("a\nb\nEND\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ch, err := runCLI(context.Background(), os.Args[0], nil, t.TempDir(),
		fakeEnv("stream", "CREMA_FAKE_FIXTURE="+fixture), &stubParser{sid: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	want := []Kind{KindText, KindText, KindTurnEnd}
	if len(evs) != 3 || evs[0].Kind != want[0] || evs[1].Kind != want[1] || evs[2].Kind != want[2] {
		t.Fatalf("events: %+v", evs)
	}
}

func TestRunCLISynthesizesTurnEndOnEarlyExit(t *testing.T) {
	ch, err := runCLI(context.Background(), os.Args[0], nil, t.TempDir(),
		fakeEnv("fail"), &stubParser{sid: "s2"})
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	last := evs[len(evs)-1]
	if last.Kind != KindTurnEnd || last.Result.Err == "" {
		t.Fatalf("want synthesized failing TurnEnd, got %+v", last)
	}
	if !strings.Contains(last.Result.Err, "boom: credential expired") {
		t.Fatalf("Err must carry the stderr tail, got: %s", last.Result.Err)
	}
}

func TestRunCLICancelKillsAndReportsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := runCLI(ctx, os.Args[0], nil, t.TempDir(), fakeEnv("hang"), &stubParser{})
	if err != nil {
		t.Fatal(err)
	}
	<-ch // "first" arrived; the process is now sleeping
	start := time.Now()
	cancel()
	var last Event
	for ev := range ch {
		last = ev
	}
	if time.Since(start) > 15*time.Second {
		t.Fatal("kill took too long — process tree not killed")
	}
	if last.Kind != KindTurnEnd || !last.Result.Canceled {
		t.Fatalf("want Canceled TurnEnd, got %+v", last)
	}
}

func TestRunCLISurvivesStderrFlood(t *testing.T) {
	ch, err := runCLI(context.Background(), os.Args[0], nil, t.TempDir(),
		fakeEnv("stderr-flood"), &stubParser{})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		n := 0
		for range ch {
			n++
		}
		done <- n
	}()
	select {
	case n := <-done:
		if n < 2 { // "first" + the synthesized TurnEnd
			t.Fatalf("got %d events, want ≥2", n)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("deadlock: stderr was not drained")
	}
}
