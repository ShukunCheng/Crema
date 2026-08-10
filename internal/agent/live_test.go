//go:build live

package agent

import (
	"context"
	"testing"
	"time"
)

// Schema snapshot check against the real CLI. Costs a small amount of your
// subscription quota, so it is behind a build tag and never runs in CI:
//
//	go test -tags live -run TestLiveClaude ./internal/agent/ -v -timeout 300s
//
// If this fails after a CLI upgrade, refresh testdata/claude_*.jsonl from
// fresh `claude -p ... --output-format stream-json --verbose` output.
func TestLiveClaudeSmoke(t *testing.T) {
	c := NewClaude()
	if err := c.Available(); err != nil {
		t.Skip(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	ch, err := c.Run(ctx, RunOptions{Prompt: "Reply with exactly: OK", Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	last := evs[len(evs)-1]
	if last.Kind != KindTurnEnd || last.Result.Err != "" || last.Result.SessionID == "" {
		t.Fatalf("live end event: %+v", last)
	}
	t.Logf("parsed %d events; skipped lines are tolerated by design", len(evs))
}
