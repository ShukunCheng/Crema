package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ShukunCheng/Crema/internal/crlog"
)

// One turn used to be one process: `claude -p --resume`, answer, exit. That
// is what headless mode offers by default, and it is expensive at size —
// every turn rebuilds the conversation from the transcript, and the rebuilt
// prefix stops matching what the API has cached. Measured on real traffic,
// the first call of a crema turn rewrote 59% of its cached prefix where the
// same work in an interactive session rewrote 11%; cache writes bill at
// 1.25x-2x of input where reads bill at 0.1x.
//
// The CLI also accepts turns on stdin — `--input-format stream-json` — and
// then one process serves a whole conversation, exactly as an interactive
// session does, holding its context instead of rebuilding it. Measured on a
// live three-turn run: the second turn rewrote 81 tokens against 26,334 read.
//
// Conversation is that long-lived process.
type Conversation interface {
	// Send starts a turn. The events for it arrive on Events.
	Send(prompt string) error
	// Events carries every event the process produces, across turns, until
	// the process ends and the channel closes.
	Events() <-chan Event
	// SessionID is the CLI's own id for the conversation, once it has said.
	SessionID() string
	// Close ends the process. Safe to call twice.
	Close() error
}

// Streamer is the optional interface for a backend that can hold a
// conversation open. A backend without it is driven a process per turn.
type Streamer interface {
	Open(ctx context.Context, opts RunOptions) (Conversation, error)
}

// claudeConv is one `claude -p --input-format stream-json` process.
type claudeConv struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	parser *ClaudeParser
	events chan Event
	tail   *tailBuffer

	mu     sync.Mutex
	closed bool
	reqN   int // control requests sent, for their ids
}

// Open starts a conversation process. It carries the same flags a single-turn
// run would, so the model, the permission mode and the resumed session are
// unchanged — only the prompt arrives differently.
func (c *Claude) Open(ctx context.Context, opts RunOptions) (Conversation, error) {
	path, err := exec.LookPath(c.bin)
	if err != nil {
		return nil, fmt.Errorf("%s not found in PATH: %w", c.bin, err)
	}
	args := []string{"-p", "--input-format", "stream-json",
		"--output-format", "stream-json", "--verbose"}
	if m := claudeMode(opts.Permission); m != "" {
		args = append(args, "--permission-mode", m)
	}
	if opts.Model != DefaultModel && opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	// Effort has no control request — measured against the CLI's own bundle,
	// which offers set_model and set_permission_mode but nothing for this — so
	// it can only travel on the command line, and changing it costs a process.
	if opts.Effort != DefaultEffort {
		args = append(args, "--effort", opts.Effort)
	}
	if opts.SessionID != "" {
		args = append(args, "--resume", opts.SessionID)
	}

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = opts.Dir
	if len(c.extraEnv) > 0 {
		cmd.Env = append(os.Environ(), c.extraEnv...)
	}
	setSysProcAttr(cmd)
	cmd.Cancel = func() error { return killTree(cmd) }
	cmd.WaitDelay = 5 * time.Second

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
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

	cv := &claudeConv{
		cmd: cmd, stdin: stdin, parser: &ClaudeParser{},
		events: make(chan Event, 64), tail: &tailBuffer{max: 8 * 1024},
	}
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(cv.tail, stderr)
		close(stderrDone)
	}()
	go cv.read(stdout, stderrDone)
	return cv, nil
}

// read pumps the process's output into the events channel for as long as it
// lives. Unlike a single-turn run this does not synthesise a closing
// TurnEnd per turn — the CLI's own result line ends each turn — but a
// process that dies mid-turn still owes the caller one, so the channel's
// last word is an error TurnEnd when it stops unexpectedly.
func (cv *claudeConv) read(stdout io.Reader, stderrDone <-chan struct{}) {
	defer close(cv.events)
	r := bufio.NewReaderSize(stdout, 1<<20)
	inTurn := false
	for {
		line, rerr := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			for _, ev := range cv.parser.ParseLine(line) {
				switch ev.Kind {
				case KindTurnEnd:
					inTurn = false
				case KindText, KindToolCall, KindToolOutput:
					inTurn = true
				}
				cv.events <- ev
			}
		}
		if rerr != nil {
			break
		}
	}
	werr := cv.cmd.Wait()
	<-stderrDone
	if !inTurn {
		// Nothing was owed for a turn — but a process that died with
		// something to say should still say it. A CLI that cannot
		// authenticate prints the reason to stderr and exits before it
		// produces a single event, so inTurn was false, this returned in
		// silence, and the one sentence that explained the failure was
		// thrown away. All crema could offer was "the agent stream ended
		// without finishing the turn", which is true and useless.
		if !cv.isClosed() {
			if msg := cv.deathNote(werr); msg != "" {
				crlog.Log("proc.died", "in_turn", false, "err", fmt.Sprint(werr),
					"stderr", strings.TrimSpace(cv.tail.String()))
				cv.events <- Event{Kind: KindError, Text: msg}
			}
		}
		return
	}
	res := TurnResult{SessionID: cv.parser.SessionID()}
	if cv.isClosed() {
		res.Canceled = true
	} else {
		crlog.Log("proc.died", "in_turn", true, "err", fmt.Sprint(werr),
			"stderr", strings.TrimSpace(cv.tail.String()))
		res.Err = fmt.Sprintf("claude exited mid-turn: %v", werr)
		if s := strings.TrimSpace(cv.tail.String()); s != "" {
			res.Err += "\n─ stderr tail ─\n" + s
			if hint := loginHint(s); hint != "" {
				res.Err += "\n\n" + hint
			}
		}
	}
	cv.events <- Event{Kind: KindTurnEnd, Result: &res}
}

// deathNote is what to say about a process that ended on its own, or empty
// when it ended cleanly with nothing to report.
func (cv *claudeConv) deathNote(werr error) string {
	tail := strings.TrimSpace(cv.tail.String())
	if tail == "" && werr == nil {
		return "" // it simply finished; the next message opens another
	}
	msg := "the agent's process ended"
	if werr != nil {
		msg += ": " + werr.Error()
	}
	if tail != "" {
		msg += "\n─ stderr tail ─\n" + tail
	}
	if hint := loginHint(tail); hint != "" {
		msg += "\n\n" + hint
	}
	return msg
}

// loginHint recognises the one failure crema can tell you how to fix. The
// subscription login is an OAuth pair — a short-lived access token and a
// refresh token to renew it — and when the refresh fails there is nothing
// crema can do about it from here, because crema never handles credentials.
// Signing in again is the whole of the answer, so it says so rather than
// leaving you with the CLI's sentence and no next step.
func loginHint(stderr string) string {
	low := strings.ToLower(stderr)
	for _, sign := range []string{"oauth", "authenticate", "unauthorized", "log in", "login", "credentials"} {
		if strings.Contains(low, sign) {
			return "crema never handles credentials — this is the CLI's own login. " +
				"Run `claude` in a terminal once to sign in again, then send this message again."
		}
	}
	return ""
}

// userMessage is one turn as the CLI wants it on stdin.
type userMessage struct {
	Type    string `json:"type"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
}

func (cv *claudeConv) Send(prompt string) error {
	cv.mu.Lock()
	defer cv.mu.Unlock()
	if cv.closed {
		return errors.New("conversation is closed")
	}
	var m userMessage
	m.Type = "user"
	m.Message.Role = "user"
	m.Message.Content = prompt
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if _, err := cv.stdin.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

func (cv *claudeConv) Events() <-chan Event { return cv.events }

func (cv *claudeConv) SessionID() string { return cv.parser.SessionID() }

func (cv *claudeConv) isClosed() bool {
	cv.mu.Lock()
	defer cv.mu.Unlock()
	return cv.closed
}

// Close ends the process. Closing stdin is the polite way — the CLI exits
// when its input runs out — and the kill is there for a turn in flight,
// which is what cancelling one means.
func (cv *claudeConv) Close() error {
	cv.mu.Lock()
	if cv.closed {
		cv.mu.Unlock()
		return nil
	}
	cv.closed = true
	cv.mu.Unlock()

	_ = cv.stdin.Close()
	done := make(chan struct{})
	go func() {
		for range cv.events { // let the reader finish rather than block it
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = killTree(cv.cmd)
		<-done
	}
	return nil
}
