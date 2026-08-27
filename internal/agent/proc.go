package agent

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

type lineParser interface {
	ParseLine(line []byte) []Event
	SessionID() string
}

// runCLI spawns bin with args in dir and enforces the adapter contract: at
// least one KindTurnEnd arrives before the channel closes. More than one is
// real: a turn that launched an async task is continued when the task ends,
// and each leg closes with its own result.
func runCLI(ctx context.Context, bin string, args []string, dir string, extraEnv []string, p lineParser) (<-chan Event, error) {
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("%s not found in PATH: %w", bin, err)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Stdin = nil // the CLIs read stdin when no prompt arg is given; never let them wait on us
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	setSysProcAttr(cmd)
	cmd.Cancel = func() error { return killTree(cmd) }
	cmd.WaitDelay = 5 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	ch := make(chan Event, 64)
	tail := &tailBuffer{max: 8 * 1024}
	stderrDone := make(chan struct{})
	go func() { // codex writes hundreds of KB of tracing here; must drain or the child blocks
		_, _ = io.Copy(tail, stderr)
		close(stderrDone)
	}()

	go func() {
		defer close(ch)
		sawEnd := false
		r := bufio.NewReaderSize(stdout, 1<<20)
		for {
			line, rerr := r.ReadBytes('\n')
			if len(bytes.TrimSpace(line)) > 0 {
				for _, ev := range p.ParseLine(line) {
					if ev.Kind == KindTurnEnd {
						sawEnd = true
					}
					ch <- ev
				}
			}
			if rerr != nil {
				break
			}
		}
		werr := cmd.Wait()
		<-stderrDone
		if !sawEnd {
			res := TurnResult{SessionID: p.SessionID()}
			if ctx.Err() != nil {
				res.Canceled = true
			} else {
				res.Err = fmt.Sprintf("%s exited before finishing the turn: %v", bin, werr)
				if s := tail.String(); s != "" {
					res.Err += "\n─ stderr tail ─\n" + s
				}
			}
			ch <- Event{Kind: KindTurnEnd, Result: &res}
		}
	}()
	return ch, nil
}

// tailBuffer keeps only the last max bytes written to it.
type tailBuffer struct {
	buf []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return string(bytes.TrimSpace(t.buf)) }
