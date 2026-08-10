# Crema M1 MVP Implementation Plan

Work through the tasks in order. Each one ends with passing tests and a commit.

**Goal:** A working `crema` TUI that drives Claude Code and Codex through their official headless CLIs (subscription auth, zero API keys), renders every tool call and output fully expanded in a left timeline, and keeps a live git diff panel on the right — covering the spec's M0 (rebuild from scratch; the prototype code is not in this repo) plus all of M1 (Codex adapter, agent hot-switch, diff auto-refresh, turn cancel, error handling, README).

**Architecture:** A `tea.Model` app composes three panes (timeline, diff, input+status). Each backend implements a small `Agent` interface returning a channel of normalized events; adapters are split into a *pure line parser* (unit-tested against golden JSONL fixtures captured from the real CLIs on 2026-08-10) and a *shared subprocess runner* that handles spawning, stderr draining, process-tree kill, and the "always emit TurnEnd, then close" contract. Git state is collected by shelling out to `git` and parsing unified diffs; refresh is event-triggered and debounced.

**Tech Stack:** Go ≥1.23, `github.com/charmbracelet/bubbletea` **v1.x**, `bubbles` (viewport/textarea/spinner), `lipgloss` v1.x. No other runtime deps. Module path `github.com/ShukunCheng/Crema`.

## Global Constraints

Copied from the product spec — every task implicitly includes these:

- **Zero API keys:** never read, store, or pass API keys/tokens; authentication belongs entirely to the official CLIs (`claude`, `codex`). Never write to their config files.
- **Official headless interfaces only:** `claude -p --output-format stream-json --verbose` (resume: `--resume <session_id>`), `codex exec --json` (resume: `codex exec resume <thread_id>`). Nothing else touches the agents.
- **Transparency:** every command, edit, and tool output renders fully expanded by default. Any truncation MUST carry an explicit visible label (`… +N lines truncated (crema cap 4000)`). Never silently fold.
- **Tolerant parsers:** unknown event types/subtypes are skipped, never fatal. Each backend has schema snapshot (golden-file) tests. (Codex has already changed schema once: legacy `{"id","msg":{...}}` vs current `{"type":"item.*"}` — support both.)
- **Claude permission mode:** `--permission-mode acceptEdits`, surfaced prominently in the UI. Codex equivalent: `--full-auto`.
- **Terminal-first:** single static binary (`-ldflags "-s -w"`), < 15 MB (build script enforces), starts instantly, usable at 80×24 (diff pane collapses below 100 cols), works over SSH, Windows + Linux + macOS.
- **Install → first chat < 2 minutes:** README quickstart must achieve this.
- **Theme:** pink/purple dark palette, rounded borders, tool blocks with a left vertical rail.
- **Pin bubbletea to v1.x APIs** (NOT the v2 betas — different `tea.Model` signatures; all code below is v1).
- **Run `gofmt -w .` before every commit.** CI fails on unformatted files; the code blocks in this plan are not guaranteed to be gofmt-exact (alignment, import order).
- **Go ≥1.21 builtins:** `max`/`min` are builtins — do not redeclare them.
- **Scope:** M0+M1 only. M2 (themes, split diff, session persistence, /commands, keybinds) and M3 (Gemini, worktrees, hunk staging, brew/scoop) get separate plans later.

## Verified Ground Truth (captured 2026-08-10 on this machine)

- `claude` 2.1.226 and `codex-cli` 0.105.0 are installed; **Go is NOT installed** (Task 1 installs it).
- Real `claude -p "Reply with exactly: OK" --output-format stream-json --verbose` output captured; the trimmed fixture in Task 4 is that real capture. Notable realities: `system` events include subtypes `hook_started`, `hook_response`, `thinking_tokens`; a top-level `rate_limit_event` type exists; each `assistant` event carries **one new content block** (same `message.id` repeats across events — first `thinking`, then `text`); `result` carries `session_id`, `duration_ms`, `total_cost_usd`, `usage`.
- Real `codex exec --json` output captured: `thread.started` (`thread_id`), `turn.started`, `error`, `turn.failed`. On this machine codex currently fails with *"The 'gpt-5.2-codex' model is not supported when using Codex with a ChatGPT account"* — a real error our error path must render well (it's Task 7's error fixture verbatim).
- Codex wrote **344 KB to stderr** on that trivial run (tracing logs). Stderr MUST be drained concurrently or the child blocks on a full pipe (Task 5).
- `codex exec resume [OPTIONS] [SESSION_ID] [PROMPT]` exists in 0.105 (`--last` also exists but is racy if the user runs codex elsewhere — always resume by explicit id).

## File Structure

```
Crema/                              (module github.com/ShukunCheng/Crema)
├── go.mod / go.sum
├── cmd/crema/
│   ├── main.go                    — flags (--agent --dir --version --doctor), tea.NewProgram
│   ├── version.go                 — var Version, stamped by ldflags
│   ├── doctor.go                  — environment report
│   └── main_test.go
├── .gitignore
├── .github/workflows/ci.yml       — go vet + go test on ubuntu & windows
├── scripts/build.ps1, build.sh    — stripped cross-builds + 15 MB size gate
├── internal/agent/
│   ├── agent.go                   — Event/Kind/ToolCall/ToolOutput/TurnResult, Agent iface, RunOptions
│   ├── registry.go                — Registry: ordered agents, availability, cyclic Next()
│   ├── mock.go                    — scripted demo agent (also used by UI tests)
│   ├── proc.go                    — runCLI(): spawn, line-stream, stderr tail, TurnEnd contract
│   ├── kill_windows.go/kill_unix.go — process-tree kill
│   ├── claude.go / claude_parser.go — Claude adapter + pure parser
│   ├── codex.go  / codex_parser.go  — Codex adapter + stateful parser (new + legacy schema)
│   ├── *_test.go
│   └── testdata/*.jsonl           — golden fixtures (real captures, trimmed)
├── internal/gitdiff/
│   ├── gitdiff.go                 — Collect(dir) → DiffSet; unified-diff parser; untracked synth
│   └── gitdiff_test.go            — against throwaway real git repos (t.TempDir)
└── internal/ui/
    ├── theme.go                   — palette + lipgloss styles
    ├── blocks.go                  — pure renderers: user/assistant/thinking/tool/error/system/stats
    ├── timeline.go                — Timeline: blocks state + viewport + follow mode
    ├── diffpanel.go               — DiffSet → styled pane
    ├── input.go                   — textarea wrapper
    ├── statusbar.go               — one-line status segments
    ├── app.go                     — root model: layout, keys, event loop, cancel, switch, debounce
    └── *_test.go
```

Dependency order: T1 → T2 → {T3, T4, T8, T9} → T5 → T6/T7 → T10/T11 → T12 → T13.

---

### Task 1: Toolchain + project scaffold

**Files:**
- Create: `go.mod`, `cmd/crema/main.go`, `cmd/crema/version.go`, `cmd/crema/version_test.go`, `.gitignore`

**Interfaces:**
- Produces: module `github.com/ShukunCheng/Crema`; `var Version = "0.1.0-dev"` in package `main` under `cmd/crema` (ldflags-overridable at `main.Version`). Main lives in `cmd/crema` so the built and `go install`ed binary is named `crema`, not `Crema`.

- [ ] **Step 1: Install Go (Windows, this machine)**

```powershell
winget install --id GoLang.Go --accept-source-agreements --accept-package-agreements
```

Then **open a fresh shell** (PATH update) and verify: `go version` → expect `go1.2x` (≥1.23). If `go` is still not found, add `C:\Program Files\Go\bin` to PATH for the session: `$env:Path += ";C:\Program Files\Go\bin"`.

- [ ] **Step 2: Scaffold module and files**

```powershell
go mod init github.com/ShukunCheng/Crema
```

`.gitignore`:
```
crema
crema.exe
dist/
*.log
```

`cmd/crema/version.go`:
```go
package main

// Version is stamped by -ldflags "-X main.Version=v0.1.0" in release builds.
var Version = "0.1.0-dev"
```

`cmd/crema/main.go` (temporary CLI; Task 13 replaces the body with the real TUI wiring):
```go
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("crema", Version)
		os.Exit(0)
	}
	fmt.Println("crema", Version, "— TUI arrives in Task 13")
}
```

`cmd/crema/version_test.go`:
```go
package main

import "testing"

func TestVersionIsSet(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
}
```

- [ ] **Step 3: Verify build + test pass**

Run: `go build -o crema.exe ./cmd/crema && go test ./... && ./crema.exe --version`
Expected: builds, `ok` for tests, prints `crema 0.1.0-dev`.

- [ ] **Step 4: Commit**

```bash
git add go.mod .gitignore cmd/
git commit -m "feat: scaffold Go module with version flag"
```

---

### Task 2: Event model, Agent interface, registry

**Files:**
- Create: `internal/agent/agent.go`, `internal/agent/registry.go`, `internal/agent/registry_test.go`

**Interfaces:**
- Produces (used by every later task — exact names matter):

```go
package agent

type Kind int

const (
	KindText Kind = iota // assistant prose (Thinking=true → reasoning channel)
	KindToolCall
	KindToolOutput
	KindTurnEnd // ALWAYS the final event before channel close
	KindError   // non-fatal error surfaced mid-stream
)

type Event struct {
	Kind     Kind
	Text     string      // KindText, KindError
	Thinking bool        // KindText only
	Tool     *ToolCall   // KindToolCall
	Output   *ToolOutput // KindToolOutput
	Result   *TurnResult // KindTurnEnd
}

type ToolCall struct {
	ID    string // backend's tool/item id; matches ToolOutput.ToolID
	Name  string // "Bash", "shell", "apply_patch", …
	Input string // human-readable, pretty-printed; NEVER truncated here
}

type ToolOutput struct {
	ToolID  string
	Content string
	IsError bool
}

type TurnResult struct {
	SessionID    string  // pass back via RunOptions.SessionID to resume
	DurationMS   int64
	CostUSD      float64 // 0 when backend doesn't report USD (codex)
	InputTokens  int64
	OutputTokens int64
	Canceled     bool
	Err          string // non-empty ⇒ turn failed
}

type RunOptions struct {
	Prompt    string
	Dir       string // working directory for the agent subprocess
	SessionID string // "" = new session; else resume
}

type Agent interface {
	Name() string  // stable id: "claude" | "codex" | "mock"
	Label() string // display: "Claude Code" | "Codex" | "Mock"
	Available() error
	Run(ctx context.Context, opts RunOptions) (<-chan Event, error)
}
```

```go
// registry.go
type Registry struct {
	Agents []Agent // ordered; tests construct this directly
}

func NewRegistry() *Registry              // registers claude, codex, mock in that order (T6/T7 fill it in)
func (r *Registry) FirstAvailable() Agent // first with Available()==nil; mock is always available (last-resort fallback)
func (r *Registry) Next(cur string) Agent // cyclic hot-switch over available agents; nil if none
```

- [ ] **Step 1: Write failing registry tests** (`internal/agent/registry_test.go`)

```go
package agent

import (
	"context"
	"errors"
	"testing"
)

type fakeAgent struct {
	name string
	err  error
}

func (f fakeAgent) Name() string     { return f.name }
func (f fakeAgent) Label() string    { return f.name }
func (f fakeAgent) Available() error { return f.err }
func (f fakeAgent) Run(context.Context, RunOptions) (<-chan Event, error) {
	return nil, errors.New("not implemented")
}

func TestFirstAvailableSkipsUnavailable(t *testing.T) {
	r := &Registry{Agents: []Agent{
		fakeAgent{name: "claude", err: errors.New("not installed")},
		fakeAgent{name: "codex"},
	}}
	if got := r.FirstAvailable().Name(); got != "codex" {
		t.Fatalf("FirstAvailable = %q, want codex", got)
	}
}

func TestNextCyclesAndSkipsUnavailable(t *testing.T) {
	r := &Registry{Agents: []Agent{
		fakeAgent{name: "claude"},
		fakeAgent{name: "codex", err: errors.New("nope")},
		fakeAgent{name: "mock"},
	}}
	if got := r.Next("claude").Name(); got != "mock" {
		t.Fatalf("Next(claude) = %q, want mock (codex unavailable)", got)
	}
	if got := r.Next("mock").Name(); got != "claude" {
		t.Fatalf("Next(mock) = %q, want claude (wraps)", got)
	}
}

func TestFirstAvailableFallsBackToNilWhenEmpty(t *testing.T) {
	r := &Registry{}
	if r.FirstAvailable() != nil {
		t.Fatal("empty registry must return nil")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/agent/ -run 'TestFirstAvailable|TestNext' -v`
Expected: FAIL — `Registry` undefined.

- [ ] **Step 3: Implement `agent.go` (types above, verbatim) and `registry.go`**

```go
package agent

type Registry struct {
	Agents []Agent
}

func NewRegistry() *Registry {
	return &Registry{} // adapters are registered in Tasks 6 and 7
}

func (r *Registry) FirstAvailable() Agent {
	for _, a := range r.Agents {
		if a.Available() == nil {
			return a
		}
	}
	return nil
}

func (r *Registry) Next(cur string) Agent {
	n := len(r.Agents)
	if n == 0 {
		return nil
	}
	start := 0
	for i, a := range r.Agents {
		if a.Name() == cur {
			start = i
			break
		}
	}
	for off := 1; off <= n; off++ {
		a := r.Agents[(start+off)%n]
		if a.Available() == nil {
			return a
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/agent/ -v` — Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/agent/
git commit -m "feat: agent event model, interface, and registry"
```

---

### Task 3: Mock agent (contract reference + UI test double)

**Files:**
- Create: `internal/agent/mock.go`, `internal/agent/mock_test.go`

**Interfaces:**
- Consumes: Task 2 types.
- Produces: `func NewMock() *Mock` (implements `Agent`, Name `"mock"`). **Adapter contract established here, all adapters must obey it:** the event channel ALWAYS delivers exactly one `KindTurnEnd` as its final event, then closes — including on ctx cancel (with `Result.Canceled=true`).

- [ ] **Step 1: Write failing tests** (`mock_test.go`)

```go
package agent

import (
	"context"
	"testing"
	"time"
)

func collect(t *testing.T, ch <-chan Event) []Event {
	t.Helper()
	var evs []Event
	for ev := range ch {
		evs = append(evs, ev)
	}
	return evs
}

func TestMockEmitsFullScriptEndingInTurnEnd(t *testing.T) {
	m := NewMock()
	ch, err := m.Run(context.Background(), RunOptions{Prompt: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	if len(evs) < 4 {
		t.Fatalf("want ≥4 events (text, toolcall, tooloutput, turnend), got %d", len(evs))
	}
	last := evs[len(evs)-1]
	if last.Kind != KindTurnEnd || last.Result == nil || last.Result.Err != "" {
		t.Fatalf("final event must be successful KindTurnEnd, got %+v", last)
	}
	var sawCall, sawOut bool
	for _, ev := range evs {
		sawCall = sawCall || ev.Kind == KindToolCall
		sawOut = sawOut || ev.Kind == KindToolOutput
	}
	if !sawCall || !sawOut {
		t.Fatal("script must include a tool call and its output")
	}
}

func TestMockCancelStillClosesWithCanceledTurnEnd(t *testing.T) {
	m := NewMock()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := m.Run(ctx, RunOptions{Prompt: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	<-ch // first event arrived; cancel mid-stream
	cancel()
	deadline := time.After(2 * time.Second)
	var last Event
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				if last.Kind != KindTurnEnd || last.Result == nil || !last.Result.Canceled {
					t.Fatalf("final event must be canceled TurnEnd, got %+v", last)
				}
				return
			}
			last = ev
		case <-deadline:
			t.Fatal("channel did not close after cancel")
		}
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/agent/ -run TestMock -v` → FAIL: `NewMock` undefined.

- [ ] **Step 3: Implement `mock.go`**

```go
package agent

import (
	"context"
	"time"
)

// Mock is a scripted agent used for demos (crema --agent mock) and UI tests.
type Mock struct {
	// StepDelay between events; tests can zero it.
	StepDelay time.Duration
}

func NewMock() *Mock { return &Mock{StepDelay: 400 * time.Millisecond} }

func (m *Mock) Name() string     { return "mock" }
func (m *Mock) Label() string    { return "Mock" }
func (m *Mock) Available() error { return nil }

func (m *Mock) Run(ctx context.Context, opts RunOptions) (<-chan Event, error) {
	ch := make(chan Event, 8)
	script := []Event{
		{Kind: KindText, Thinking: true, Text: "Planning: touch a file, then summarize."},
		{Kind: KindText, Text: "I'll create hello.txt to demonstrate the fully expanded tool view."},
		{Kind: KindToolCall, Tool: &ToolCall{ID: "m1", Name: "Bash", Input: "echo hello > hello.txt"}},
		{Kind: KindToolOutput, Output: &ToolOutput{ToolID: "m1", Content: "(no output, exit 0)"}},
		{Kind: KindToolCall, Tool: &ToolCall{ID: "m2", Name: "Bash", Input: "wc -c hello.txt"}},
		{Kind: KindToolOutput, Output: &ToolOutput{ToolID: "m2", Content: "6 hello.txt"}},
		{Kind: KindText, Text: "Done — hello.txt created. Check the diff panel on the right."},
	}
	go func() {
		defer close(ch)
		start := time.Now()
		for _, ev := range script {
			select {
			case <-ctx.Done():
				ch <- Event{Kind: KindTurnEnd, Result: &TurnResult{
					SessionID: "mock-session", Canceled: true,
					DurationMS: time.Since(start).Milliseconds(),
				}}
				return
			case <-time.After(m.StepDelay):
				ch <- ev
			}
		}
		ch <- Event{Kind: KindTurnEnd, Result: &TurnResult{
			SessionID: "mock-session", DurationMS: time.Since(start).Milliseconds(),
			InputTokens: 42, OutputTokens: 128,
		}}
	}()
	return ch, nil
}
```

In tests set `m.StepDelay = time.Millisecond` after `NewMock()` — add that line to both tests when implementing (keeps suite fast).

- [ ] **Step 4: Run tests** — `go test ./internal/agent/ -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/mock.go internal/agent/mock_test.go
git commit -m "feat: scripted mock agent establishing the TurnEnd contract"
```

---

### Task 4: Claude stream-json parser (pure, golden-file tested)

**Files:**
- Create: `internal/agent/claude_parser.go`, `internal/agent/claude_parser_test.go`, `internal/agent/testdata/claude_smoke.jsonl`, `internal/agent/testdata/claude_tools.jsonl`

**Interfaces:**
- Consumes: Task 2 event types.
- Produces: `type ClaudeParser struct{ sessionID string; Skipped int }` with `func (p *ClaudeParser) ParseLine(line []byte) []Event` and `func (p *ClaudeParser) SessionID() string`. (Task 5 defines the `lineParser` interface these satisfy.)

**Mapping rules** (derived from the real 2.1.226 capture):

| stream-json input | Event(s) |
|---|---|
| `system`/`init` | none; store `session_id` |
| `system`/anything else (`hook_started`, `hook_response`, `thinking_tokens`, …) | none (tolerated) |
| `rate_limit_event`, unknown top-level types, malformed JSON | none; `Skipped++` |
| `assistant` → each `content[]` block: `thinking` (non-empty) | `KindText{Thinking:true}` |
| `assistant` → `text` | `KindText` |
| `assistant` → `tool_use` | `KindToolCall{ID, Name, Input: pretty-printed JSON}` |
| `user` → `tool_result` | `KindToolOutput{ToolID: tool_use_id, Content: flattened, IsError}` |
| `result` | `KindTurnEnd{SessionID, DurationMS, CostUSD, tokens, Err: result-if-is_error}` |

`tool_result.content` is a string OR `[{"type":"text","text":…}]` OR absent — flatten all three. Same `message.id` repeats across `assistant` events, each carrying only its **new** block(s) — so just append blocks per event, no dedup.

- [ ] **Step 1: Create fixture `testdata/claude_smoke.jsonl`** — the real capture, trimmed (long arrays/signatures shortened; structure and every parsed key intact):

```jsonl
{"type":"system","subtype":"hook_started","hook_name":"SessionStart:startup","uuid":"7c82b548","session_id":"988f2fb1-09f3-40fc-b3ff-14edfeba92a4"}
{"type":"system","subtype":"init","cwd":"C:\\work\\demo","session_id":"988f2fb1-09f3-40fc-b3ff-14edfeba92a4","tools":["Task","Bash","Edit","Read","Write"],"model":"claude-fable-5","permissionMode":"acceptEdits","apiKeySource":"none","claude_code_version":"2.1.226","uuid":"d2be9a34"}
{"type":"system","subtype":"thinking_tokens","estimated_tokens":50,"estimated_tokens_delta":50,"uuid":"7c74b7bb","session_id":"988f2fb1-09f3-40fc-b3ff-14edfeba92a4"}
{"type":"assistant","message":{"model":"claude-fable-5","id":"msg_011","type":"message","role":"assistant","content":[{"type":"thinking","thinking":"","signature":"CAIS…"}],"usage":{"input_tokens":2,"output_tokens":6}},"parent_tool_use_id":null,"session_id":"988f2fb1-09f3-40fc-b3ff-14edfeba92a4","uuid":"20b7157f"}
{"type":"assistant","message":{"model":"claude-fable-5","id":"msg_011","type":"message","role":"assistant","content":[{"type":"text","text":"OK"}],"usage":{"input_tokens":2,"output_tokens":6}},"parent_tool_use_id":null,"session_id":"988f2fb1-09f3-40fc-b3ff-14edfeba92a4","uuid":"f7fa81f5"}
{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","utilization":0.97},"uuid":"59ae8d2d","session_id":"988f2fb1-09f3-40fc-b3ff-14edfeba92a4"}
{"is_error":false,"duration_api_ms":3809,"num_turns":1,"stop_reason":"end_turn","session_id":"988f2fb1-09f3-40fc-b3ff-14edfeba92a4","total_cost_usd":0.293986,"usage":{"input_tokens":2,"cache_creation_input_tokens":13537,"cache_read_input_tokens":20326,"output_tokens":58},"permission_denials":[],"subtype":"success","result":"OK","type":"result","duration_ms":7557,"uuid":"adf4d827"}
```

- [ ] **Step 2: Create fixture `testdata/claude_tools.jsonl`** — tool_use/tool_result shapes (hand-authored to the documented schema; Task 6 Step 5 re-verifies against a live tool-using run):

```jsonl
{"type":"system","subtype":"init","cwd":"/tmp/demo","session_id":"aaaa-1111","model":"claude-fable-5","permissionMode":"acceptEdits","uuid":"u1"}
{"type":"assistant","message":{"id":"msg_1","role":"assistant","content":[{"type":"text","text":"Creating the file now."}]},"session_id":"aaaa-1111","uuid":"u2"}
{"type":"assistant","message":{"id":"msg_1","role":"assistant","content":[{"type":"tool_use","id":"toolu_01","name":"Bash","input":{"command":"echo hi > hi.txt","description":"Create hi.txt"}}]},"session_id":"aaaa-1111","uuid":"u3"}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01","content":"","is_error":false}]},"session_id":"aaaa-1111","uuid":"u4"}
{"type":"assistant","message":{"id":"msg_2","role":"assistant","content":[{"type":"tool_use","id":"toolu_02","name":"Read","input":{"file_path":"/tmp/demo/hi.txt"}}]},"session_id":"aaaa-1111","uuid":"u5"}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_02","content":[{"type":"text","text":"hi"}],"is_error":false}]},"session_id":"aaaa-1111","uuid":"u6"}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_99","content":[{"type":"text","text":"command not found: frobnicate"}],"is_error":true}]},"session_id":"aaaa-1111","uuid":"u7"}
{"type":"result","subtype":"success","is_error":false,"duration_ms":9000,"num_turns":2,"result":"Created hi.txt","session_id":"aaaa-1111","total_cost_usd":0.05,"usage":{"input_tokens":10,"output_tokens":99}}
```

(`toolu_99` has no matching call — exercises orphan-output tolerance.)

- [ ] **Step 3: Write failing tests** (`claude_parser_test.go`)

```go
package agent

import (
	"bufio"
	"os"
	"testing"
)

func parseFixture(t *testing.T, p interface {
	ParseLine([]byte) []Event
}, path string) []Event {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var evs []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		evs = append(evs, p.ParseLine(sc.Bytes())...)
	}
	return evs
}

func kinds(evs []Event) []Kind {
	out := make([]Kind, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func TestClaudeSmokeFixture(t *testing.T) {
	p := &ClaudeParser{}
	evs := parseFixture(t, p, "testdata/claude_smoke.jsonl")
	// empty thinking block is dropped → text, turnend only
	want := []Kind{KindText, KindTurnEnd}
	if len(evs) != len(want) {
		t.Fatalf("got %d events %v, want %v", len(evs), kinds(evs), want)
	}
	if evs[0].Text != "OK" {
		t.Fatalf("text = %q", evs[0].Text)
	}
	r := evs[1].Result
	if r == nil || r.SessionID != "988f2fb1-09f3-40fc-b3ff-14edfeba92a4" ||
		r.CostUSD != 0.293986 || r.DurationMS != 7557 || r.Err != "" {
		t.Fatalf("bad TurnResult: %+v", r)
	}
	if p.SessionID() != "988f2fb1-09f3-40fc-b3ff-14edfeba92a4" {
		t.Fatalf("SessionID() = %q", p.SessionID())
	}
	if p.Skipped == 0 {
		t.Fatal("rate_limit_event should have been counted as skipped")
	}
}

func TestClaudeToolsFixture(t *testing.T) {
	p := &ClaudeParser{}
	evs := parseFixture(t, p, "testdata/claude_tools.jsonl")
	want := []Kind{KindText, KindToolCall, KindToolOutput, KindToolCall, KindToolOutput, KindToolOutput, KindTurnEnd}
	got := kinds(evs)
	if len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", got, want)
		}
	}
	tc := evs[1].Tool
	if tc.ID != "toolu_01" || tc.Name != "Bash" || tc.Input == "" {
		t.Fatalf("tool call: %+v", tc)
	}
	if evs[4].Output.Content != "hi" {
		t.Fatalf("array-form tool_result content = %q", evs[4].Output.Content)
	}
	if !evs[5].Output.IsError || evs[5].Output.ToolID != "toolu_99" {
		t.Fatalf("error tool_result: %+v", evs[5].Output)
	}
}

func TestClaudeGarbageAndUnknownLinesAreSkipped(t *testing.T) {
	p := &ClaudeParser{}
	for _, line := range []string{"not json at all", `{"type":"brand_new_event_2027","x":1}`, "", "   "} {
		if evs := p.ParseLine([]byte(line)); len(evs) != 0 {
			t.Fatalf("line %q produced events %v", line, evs)
		}
	}
	if p.Skipped != 2 { // blank lines don't count
		t.Fatalf("Skipped = %d, want 2", p.Skipped)
	}
}
```

- [ ] **Step 4: Run to verify failure** — `go test ./internal/agent/ -run TestClaude -v` → FAIL: `ClaudeParser` undefined.

- [ ] **Step 5: Implement `claude_parser.go`**

```go
package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type ClaudeParser struct {
	sessionID string
	Skipped   int
}

type claudeLine struct {
	Type         string         `json:"type"`
	Subtype      string         `json:"subtype"`
	SessionID    string         `json:"session_id"`
	Message      *claudeMessage `json:"message"`
	IsError      bool           `json:"is_error"`
	DurationMS   int64          `json:"duration_ms"`
	TotalCostUSD float64        `json:"total_cost_usd"`
	Result       string         `json:"result"`
	Usage        *claudeUsage   `json:"usage"`
}

type claudeMessage struct {
	Content []claudeBlock `json:"content"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   json.RawMessage `json:"content"`
}

type claudeUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

func (p *ClaudeParser) SessionID() string { return p.sessionID }

func (p *ClaudeParser) ParseLine(line []byte) []Event {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}
	var l claudeLine
	if err := json.Unmarshal(line, &l); err != nil {
		p.Skipped++
		return nil
	}
	if l.SessionID != "" {
		p.sessionID = l.SessionID
	}
	switch l.Type {
	case "system":
		return nil // init recorded session id above; other subtypes tolerated
	case "assistant", "user":
		if l.Message == nil {
			return nil
		}
		var evs []Event
		for _, b := range l.Message.Content {
			switch b.Type {
			case "thinking":
				if b.Thinking != "" {
					evs = append(evs, Event{Kind: KindText, Thinking: true, Text: b.Thinking})
				}
			case "text":
				if b.Text != "" {
					evs = append(evs, Event{Kind: KindText, Text: b.Text})
				}
			case "tool_use":
				evs = append(evs, Event{Kind: KindToolCall, Tool: &ToolCall{
					ID: b.ID, Name: b.Name, Input: prettyJSON(b.Input),
				}})
			case "tool_result":
				evs = append(evs, Event{Kind: KindToolOutput, Output: &ToolOutput{
					ToolID: b.ToolUseID, Content: flattenContent(b.Content), IsError: b.IsError,
				}})
			}
		}
		return evs
	case "result":
		res := TurnResult{SessionID: p.sessionID, DurationMS: l.DurationMS, CostUSD: l.TotalCostUSD}
		if l.Usage != nil {
			res.InputTokens, res.OutputTokens = l.Usage.InputTokens, l.Usage.OutputTokens
		}
		if l.IsError {
			res.Err = l.Result
			if res.Err == "" {
				res.Err = "claude reported an error result"
			}
		}
		return []Event{{Kind: KindTurnEnd, Result: &res}}
	default:
		p.Skipped++
		return nil
	}
}

// prettyJSON renders tool input for humans: 2-space indent, raw string on failure.
func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}

// flattenContent handles tool_result.content being a string, a block array, or absent.
func flattenContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var buf bytes.Buffer
		for i, b := range blocks {
			if i > 0 {
				buf.WriteByte('\n')
			}
			buf.WriteString(b.Text)
		}
		return buf.String()
	}
	return fmt.Sprintf("%s", raw)
}
```

- [ ] **Step 6: Run tests** — `go test ./internal/agent/ -run TestClaude -v` → PASS (3 tests).

- [ ] **Step 7: Commit**

```bash
git add internal/agent/claude_parser.go internal/agent/claude_parser_test.go internal/agent/testdata/
git commit -m "feat: claude stream-json parser with golden fixtures from real 2.1.226 capture"
```

---

### Task 5: Shared subprocess runner (stderr drain, kill tree, TurnEnd contract)

**Files:**
- Create: `internal/agent/proc.go`, `internal/agent/kill_windows.go`, `internal/agent/kill_unix.go`, `internal/agent/proc_test.go`

**Interfaces:**
- Consumes: Task 2 types; Task 4's parser satisfies `lineParser`.
- Produces: `type lineParser interface { ParseLine([]byte) []Event; SessionID() string }` and `func runCLI(ctx context.Context, bin string, args []string, dir string, extraEnv []string, p lineParser) (<-chan Event, error)`. Tasks 6/7 call `runCLI`.

**Why each piece exists (verified on this machine):** codex wrote 344 KB of tracing to stderr on a trivial run — an undrained stderr pipe fills at 64 KB and deadlocks the child. `claude`/`codex` spawn node/helper children — killing only the direct process leaks the tree, so Windows uses `taskkill /T /F` and Unix uses process groups. Single JSON lines can exceed `bufio.Scanner`'s token limit (a `result` event embedding big tool outputs), so we use `ReadBytes('\n')`.

- [ ] **Step 1: Write failing tests** (`proc_test.go`) — fake-CLI via self-re-exec (`os.Args[0]` + env), no real agent CLIs needed:

```go
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

func TestMain(m *testing.M) {
	if mode := os.Getenv("CREMA_FAKE_CLI"); mode != "" {
		fakeCLI(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
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

// stubParser: any line → KindText; the line "END" → KindTurnEnd.
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
	os.WriteFile(fixture, []byte("a\nb\nEND\n"), 0o644)
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
		t.Fatalf("Err must carry stderr tail, got: %s", last.Result.Err)
	}
}

func TestRunCLICancelKillsAndReportsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := runCLI(ctx, os.Args[0], nil, t.TempDir(), fakeEnv("hang"), &stubParser{})
	if err != nil {
		t.Fatal(err)
	}
	<-ch // "first" arrived; process is now sleeping
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
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/agent/ -run TestRunCLI -v` → FAIL: `runCLI` undefined.

- [ ] **Step 3: Implement `proc.go`**

```go
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

// runCLI spawns bin with args in dir and enforces the adapter contract:
// the returned channel always ends with exactly one KindTurnEnd, then closes.
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
```

`kill_windows.go`:

```go
//go:build windows

package agent

import (
	"fmt"
	"os/exec"
	"strconv"
)

func setSysProcAttr(cmd *exec.Cmd) {}

// claude/codex spawn node & helper children; taskkill /T takes the whole tree.
func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	if out, err := kill.CombinedOutput(); err != nil {
		return fmt.Errorf("taskkill: %v (%s)", err, out)
	}
	return nil
}
```

`kill_unix.go`:

```go
//go:build !windows

package agent

import (
	"os/exec"
	"syscall"
)

func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
```

- [ ] **Step 4: Run tests on Windows** — `go test ./internal/agent/ -run TestRunCLI -v` → PASS (4 tests; the cancel test proves `taskkill` works).

- [ ] **Step 5: Commit**

```bash
git add internal/agent/proc.go internal/agent/kill_windows.go internal/agent/kill_unix.go internal/agent/proc_test.go
git commit -m "feat: subprocess runner with stderr drain, tree kill, and TurnEnd guarantee"
```

---

### Task 6: Claude Code agent (Run + resume + live verification)

**Files:**
- Create: `internal/agent/claude.go`, `internal/agent/claude_test.go`, `internal/agent/live_test.go`
- Modify: `internal/agent/registry.go` (append `NewClaude()` in `NewRegistry`)

**Interfaces:**
- Consumes: `runCLI` (T5), `ClaudeParser` (T4), `Registry` (T2).
- Produces: `func NewClaude() *Claude` implementing `Agent` (Name `"claude"`, Label `"Claude Code"`).

- [ ] **Step 1: Write failing tests** (`claude_test.go`)

```go
package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeArgs(t *testing.T) {
	c := NewClaude()
	got := c.args(RunOptions{Prompt: "fix the bug"})
	want := []string{"-p", "fix the bug", "--output-format", "stream-json", "--verbose", "--permission-mode", "acceptEdits"}
	if len(got) != len(want) {
		t.Fatalf("args = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

func TestClaudeArgsResume(t *testing.T) {
	c := NewClaude()
	got := c.args(RunOptions{Prompt: "continue", SessionID: "sid-1"})
	last2 := got[len(got)-2:]
	if last2[0] != "--resume" || last2[1] != "sid-1" {
		t.Fatalf("resume args missing: %v", got)
	}
}

func TestClaudeRunAgainstFakeBinary(t *testing.T) {
	src, _ := os.ReadFile("testdata/claude_tools.jsonl")
	fixture := filepath.Join(t.TempDir(), "f.jsonl")
	os.WriteFile(fixture, src, 0o644)
	c := NewClaude()
	c.bin = os.Args[0]
	c.extraEnv = fakeEnv("stream", "CREMA_FAKE_FIXTURE="+fixture)
	ch, err := c.Run(context.Background(), RunOptions{Prompt: "x", Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	evs := collect(t, ch)
	last := evs[len(evs)-1]
	if last.Kind != KindTurnEnd || last.Result.SessionID != "aaaa-1111" || last.Result.CostUSD != 0.05 {
		t.Fatalf("end: %+v", last)
	}
}

func TestClaudeUnavailableWhenBinaryMissing(t *testing.T) {
	c := NewClaude()
	c.bin = "definitely-not-a-real-binary-xyz"
	if c.Available() == nil {
		t.Fatal("want availability error")
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/agent/ -run TestClaude -v` → FAIL: fields/methods undefined.

- [ ] **Step 3: Implement `claude.go`**

```go
package agent

import (
	"context"
	"fmt"
	"os/exec"
)

// Claude drives the official Claude Code CLI headlessly. Auth stays inside the
// CLI (subscription login); crema never reads or passes any credential.
type Claude struct {
	bin      string
	extraEnv []string
}

func NewClaude() *Claude { return &Claude{bin: "claude"} }

func (c *Claude) Name() string  { return "claude" }
func (c *Claude) Label() string { return "Claude Code" }

func (c *Claude) Available() error {
	if _, err := exec.LookPath(c.bin); err != nil {
		return fmt.Errorf("claude CLI not found — install Claude Code and run `claude` once to log in")
	}
	return nil
}

func (c *Claude) args(opts RunOptions) []string {
	a := []string{
		"-p", opts.Prompt,
		"--output-format", "stream-json",
		"--verbose", // required by the CLI when combining -p with stream-json
		"--permission-mode", "acceptEdits",
	}
	if opts.SessionID != "" {
		a = append(a, "--resume", opts.SessionID)
	}
	return a
}

func (c *Claude) Run(ctx context.Context, opts RunOptions) (<-chan Event, error) {
	return runCLI(ctx, c.bin, c.args(opts), opts.Dir, c.extraEnv, &ClaudeParser{})
}
```

In `registry.go`, change `NewRegistry` to:

```go
func NewRegistry() *Registry {
	return &Registry{Agents: []Agent{NewClaude(), NewMock()}} // NewCodex() added in Task 7
}
```

- [ ] **Step 4: Run tests** — `go test ./internal/agent/ -v` → PASS (whole package).

- [ ] **Step 5: Live schema verification** (`live_test.go`, build-tagged; costs one tiny subscription turn — run it ONCE manually, never in CI):

```go
//go:build live

package agent

import (
	"context"
	"testing"
	"time"
)

// go test -tags live -run TestLiveClaude ./internal/agent/ -v -timeout 300s
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
```

Run: `go test -tags live -run TestLiveClaude ./internal/agent/ -v -timeout 300s`
Expected: PASS. If the real CLI has drifted from the fixtures, update `testdata/*.jsonl` from fresh output of `claude -p "Reply with exactly: OK" --output-format stream-json --verbose` — that file update IS the schema-snapshot process.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/claude.go internal/agent/claude_test.go internal/agent/live_test.go internal/agent/registry.go
git commit -m "feat: claude code adapter with resume and live schema check"
```

---

### Task 7: Codex adapter (new + legacy schema, real error fixture)

**Files:**
- Create: `internal/agent/codex_parser.go`, `internal/agent/codex.go`, `internal/agent/codex_test.go`, `internal/agent/testdata/codex_new.jsonl`, `internal/agent/testdata/codex_legacy.jsonl`, `internal/agent/testdata/codex_error.jsonl`
- Modify: `internal/agent/registry.go` (`Agents: []Agent{NewClaude(), NewCodex(), NewMock()}`)

**Interfaces:**
- Consumes: `runCLI` (T5), Task 2 types.
- Produces: `NewCodex() *Codex` (Name `"codex"`, Label `"Codex"`); `CodexParser` (stateful: dedupes `item.started`/`item.completed` pairs).

**Mapping rules:**

| codex input | Event(s) |
|---|---|
| `thread.started` | none; store `thread_id` as session id |
| `turn.started`, `item.updated`, `todo_list` items, unknown types | none (tolerated) |
| `item.started` `command_execution` | `KindToolCall{ID:item.id, Name:"shell", Input:command}` |
| `item.completed` `command_execution` | `KindToolOutput{ToolID, Content:aggregated_output, IsError:exit_code≠0}` (emit the ToolCall first if no `item.started` was seen) |
| `item.completed` `agent_message` | `KindText` |
| `item.completed` `reasoning` | `KindText{Thinking:true}` |
| `item.completed` `file_change` | `KindToolCall{Name:"apply_patch", Input:changes JSON}` + `KindToolOutput{Content:status}` |
| `item.completed` `mcp_tool_call` / `web_search` | `KindToolCall` (+ output if present) |
| `item.completed` `error` | `KindError` |
| top-level `error` | `KindError{Text:message}` |
| `turn.completed` | `KindTurnEnd{SessionID, tokens from usage}` |
| `turn.failed` | `KindTurnEnd{SessionID, Err:error.message}` |
| legacy `{"id","msg":{…}}`: `agent_message`→Text; `agent_reasoning`→Thinking; `exec_command_begin/end`→ToolCall/ToolOutput (by `call_id`); `token_count`→remember tokens; `task_complete`→TurnEnd; `error`→KindError | |

- [ ] **Step 1: Create fixtures.**

`testdata/codex_error.jsonl` — the REAL capture from this machine (2026-08-10, codex 0.105.0), verbatim:

```jsonl
{"type":"thread.started","thread_id":"019feacf-f403-7f90-a8d0-d5ca79578c8c"}
{"type":"turn.started"}
{"type":"error","message":"{\"detail\":\"The 'gpt-5.2-codex' model is not supported when using Codex with a ChatGPT account.\"}"}
{"type":"turn.failed","error":{"message":"{\"detail\":\"The 'gpt-5.2-codex' model is not supported when using Codex with a ChatGPT account.\"}"}}
```

`testdata/codex_new.jsonl` — hand-authored success flow (item shapes per the official `codex exec --json` docs; re-verify live once the user's codex model config is fixed):

```jsonl
{"type":"thread.started","thread_id":"0199a213-aaaa-bbbb-cccc-000000000001"}
{"type":"turn.started"}
{"type":"item.started","item":{"id":"item_0","item_type":"reasoning","text":"Planning"}}
{"type":"item.completed","item":{"id":"item_0","item_type":"reasoning","text":"Planning the change"}}
{"type":"item.started","item":{"id":"item_1","item_type":"command_execution","command":"ls -la","aggregated_output":"","status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_1","item_type":"command_execution","command":"ls -la","aggregated_output":"total 8\n-rw-r--r-- 1 u u 6 a.txt\n","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_2","item_type":"agent_message","text":"Listed the files."}}
{"type":"item.completed","item":{"id":"item_3","item_type":"file_change","changes":[{"path":"a.txt","kind":"add"}],"status":"completed"}}
{"type":"turn.completed","usage":{"input_tokens":1234,"cached_input_tokens":1000,"output_tokens":45}}
```

`testdata/codex_legacy.jsonl` — pre-0.44 schema (tolerance target):

```jsonl
{"id":"0","msg":{"type":"task_started"}}
{"id":"0","msg":{"type":"agent_reasoning","text":"Thinking about it"}}
{"id":"0","msg":{"type":"agent_message","message":"Hello from legacy codex"}}
{"id":"0","msg":{"type":"exec_command_begin","call_id":"c1","command":["bash","-lc","ls"],"cwd":"/tmp"}}
{"id":"0","msg":{"type":"exec_command_end","call_id":"c1","stdout":"a.txt\n","stderr":"","exit_code":0}}
{"id":"0","msg":{"type":"token_count","input_tokens":100,"output_tokens":20}}
{"id":"0","msg":{"type":"task_complete","last_agent_message":"done"}}
```

- [ ] **Step 2: Write failing tests** (`codex_test.go`)

```go
package agent

import (
	"strings"
	"testing"
)

func TestCodexRealErrorCapture(t *testing.T) {
	p := &CodexParser{}
	evs := parseFixture(t, p, "testdata/codex_error.jsonl")
	if len(evs) != 2 {
		t.Fatalf("events: %+v", evs)
	}
	if evs[0].Kind != KindError || !strings.Contains(evs[0].Text, "gpt-5.2-codex") {
		t.Fatalf("error event: %+v", evs[0])
	}
	end := evs[1]
	if end.Kind != KindTurnEnd || end.Result.Err == "" ||
		end.Result.SessionID != "019feacf-f403-7f90-a8d0-d5ca79578c8c" {
		t.Fatalf("turn end: %+v", end)
	}
}

func TestCodexNewSchemaSuccess(t *testing.T) {
	p := &CodexParser{}
	evs := parseFixture(t, p, "testdata/codex_new.jsonl")
	want := []Kind{KindText, KindToolCall, KindToolOutput, KindText, KindToolCall, KindToolOutput, KindTurnEnd}
	got := kinds(evs)
	if len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", got, want)
		}
	}
	if !evs[0].Thinking {
		t.Fatal("reasoning must map to thinking text")
	}
	if evs[1].Tool.Name != "shell" || evs[1].Tool.Input != "ls -la" {
		t.Fatalf("tool call: %+v", evs[1].Tool)
	}
	if !strings.Contains(evs[2].Output.Content, "a.txt") || evs[2].Output.ToolID != "item_1" {
		t.Fatalf("tool output: %+v", evs[2].Output)
	}
	end := evs[len(evs)-1].Result
	if end.InputTokens != 1234 || end.OutputTokens != 45 || end.SessionID == "" {
		t.Fatalf("turn end: %+v", end)
	}
}

func TestCodexLegacySchema(t *testing.T) {
	p := &CodexParser{}
	evs := parseFixture(t, p, "testdata/codex_legacy.jsonl")
	want := []Kind{KindText, KindText, KindToolCall, KindToolOutput, KindTurnEnd}
	got := kinds(evs)
	if len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	if !evs[0].Thinking || evs[1].Text != "Hello from legacy codex" {
		t.Fatalf("texts: %+v", evs[:2])
	}
	if evs[2].Tool.Input != "bash -lc ls" {
		t.Fatalf("legacy command join: %q", evs[2].Tool.Input)
	}
	if evs[len(evs)-1].Result.InputTokens != 100 {
		t.Fatalf("token_count must flow into TurnEnd: %+v", evs[len(evs)-1].Result)
	}
}

func TestCodexArgs(t *testing.T) {
	c := NewCodex()
	got := c.args(RunOptions{Prompt: "do it"})
	want := []string{"exec", "--json", "--full-auto", "do it"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
	res := c.args(RunOptions{Prompt: "more", SessionID: "tid-9"})
	wantRes := []string{"exec", "resume", "tid-9", "--json", "--full-auto", "more"}
	for i := range wantRes {
		if res[i] != wantRes[i] {
			t.Fatalf("resume args = %v, want %v", res, wantRes)
		}
	}
}
```

- [ ] **Step 3: Run to verify failure** — `go test ./internal/agent/ -run TestCodex -v` → FAIL: `CodexParser`/`NewCodex` undefined.

- [ ] **Step 4: Implement `codex_parser.go`**

```go
package agent

import (
	"bytes"
	"encoding/json"
	"strings"
)

type CodexParser struct {
	threadID    string
	started     map[string]bool // item ids whose ToolCall we already emitted
	inTok, outTok int64         // legacy token_count carry-over
	Skipped     int
}

type codexLine struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Message  string          `json:"message"` // top-level error
	Item     *codexItem      `json:"item"`
	Usage    *codexUsage     `json:"usage"`
	Error    *codexErr       `json:"error"`
	Msg      json.RawMessage `json:"msg"` // legacy schema
}

type codexItem struct {
	ID       string `json:"id"`
	ItemType string `json:"item_type"`
	Text     string `json:"text"`
	// Command is raw because codex has shipped it both as a string and as an
	// argv array; a typed field would make the whole event unparseable.
	Command          json.RawMessage `json:"command"`
	AggregatedOutput string          `json:"aggregated_output"`
	ExitCode         *int            `json:"exit_code"`
	Status           string          `json:"status"`
	Changes          json.RawMessage `json:"changes"`
	Server           string          `json:"server"`
	Tool             string          `json:"tool"`
	Query            string          `json:"query"`
}

type codexUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
}

type codexErr struct {
	Message string `json:"message"`
}

type codexLegacyMsg struct {
	Type             string   `json:"type"`
	Message          string   `json:"message"`
	Text             string   `json:"text"`
	CallID           string   `json:"call_id"`
	Command          []string `json:"command"`
	Stdout           string   `json:"stdout"`
	Stderr           string   `json:"stderr"`
	ExitCode         int      `json:"exit_code"`
	InputTokens      int64    `json:"input_tokens"`
	OutputTokens     int64    `json:"output_tokens"`
	LastAgentMessage string   `json:"last_agent_message"`
}

func (p *CodexParser) SessionID() string { return p.threadID }

func (p *CodexParser) ParseLine(line []byte) []Event {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}
	var l codexLine
	if err := json.Unmarshal(line, &l); err != nil {
		p.Skipped++
		return nil
	}
	if len(l.Msg) > 0 {
		return p.parseLegacy(l.Msg)
	}
	switch l.Type {
	case "thread.started":
		p.threadID = l.ThreadID
		return nil
	case "turn.started", "item.updated":
		return nil
	case "item.started":
		return p.itemStarted(l.Item)
	case "item.completed":
		return p.itemCompleted(l.Item)
	case "error":
		return []Event{{Kind: KindError, Text: l.Message}}
	case "turn.completed":
		res := TurnResult{SessionID: p.threadID}
		if l.Usage != nil {
			res.InputTokens, res.OutputTokens = l.Usage.InputTokens, l.Usage.OutputTokens
		}
		return []Event{{Kind: KindTurnEnd, Result: &res}}
	case "turn.failed":
		res := TurnResult{SessionID: p.threadID, Err: "codex turn failed"}
		if l.Error != nil && l.Error.Message != "" {
			res.Err = l.Error.Message
		}
		return []Event{{Kind: KindTurnEnd, Result: &res}}
	default:
		p.Skipped++
		return nil
	}
}

func (p *CodexParser) itemStarted(it *codexItem) []Event {
	if it == nil {
		return nil
	}
	if it.ItemType == "command_execution" {
		if p.started == nil {
			p.started = map[string]bool{}
		}
		p.started[it.ID] = true
		return []Event{{Kind: KindToolCall, Tool: &ToolCall{ID: it.ID, Name: "shell", Input: commandString(it.Command)}}}
	}
	return nil // reasoning/agent_message render once, on completion
}

// commandString accepts either "ls -la" or ["bash","-lc","ls -la"].
func commandString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var argv []string
	if json.Unmarshal(raw, &argv) == nil {
		return strings.Join(argv, " ")
	}
	return string(raw)
}

func (p *CodexParser) itemCompleted(it *codexItem) []Event {
	if it == nil {
		return nil
	}
	switch it.ItemType {
	case "agent_message":
		return []Event{{Kind: KindText, Text: it.Text}}
	case "reasoning":
		return []Event{{Kind: KindText, Thinking: true, Text: it.Text}}
	case "command_execution":
		var evs []Event
		if !p.started[it.ID] {
			evs = append(evs, Event{Kind: KindToolCall, Tool: &ToolCall{ID: it.ID, Name: "shell", Input: commandString(it.Command)}})
		}
		isErr := it.ExitCode != nil && *it.ExitCode != 0
		evs = append(evs, Event{Kind: KindToolOutput, Output: &ToolOutput{ToolID: it.ID, Content: it.AggregatedOutput, IsError: isErr}})
		return evs
	case "file_change":
		return []Event{
			{Kind: KindToolCall, Tool: &ToolCall{ID: it.ID, Name: "apply_patch", Input: prettyJSON(it.Changes)}},
			{Kind: KindToolOutput, Output: &ToolOutput{ToolID: it.ID, Content: it.Status}},
		}
	case "mcp_tool_call":
		name := strings.TrimPrefix(it.Server+"."+it.Tool, ".")
		return []Event{{Kind: KindToolCall, Tool: &ToolCall{ID: it.ID, Name: name, Input: it.Text}}}
	case "web_search":
		return []Event{{Kind: KindToolCall, Tool: &ToolCall{ID: it.ID, Name: "web_search", Input: it.Query}}}
	case "error":
		return []Event{{Kind: KindError, Text: it.Text}}
	default: // todo_list and future item types
		return nil
	}
}

func (p *CodexParser) parseLegacy(raw json.RawMessage) []Event {
	var m codexLegacyMsg
	if err := json.Unmarshal(raw, &m); err != nil {
		p.Skipped++
		return nil
	}
	switch m.Type {
	case "agent_message":
		return []Event{{Kind: KindText, Text: m.Message}}
	case "agent_reasoning":
		return []Event{{Kind: KindText, Thinking: true, Text: m.Text}}
	case "exec_command_begin":
		return []Event{{Kind: KindToolCall, Tool: &ToolCall{ID: m.CallID, Name: "shell", Input: strings.Join(m.Command, " ")}}}
	case "exec_command_end":
		out := m.Stdout
		if m.Stderr != "" {
			out += m.Stderr
		}
		return []Event{{Kind: KindToolOutput, Output: &ToolOutput{ToolID: m.CallID, Content: out, IsError: m.ExitCode != 0}}}
	case "token_count":
		p.inTok, p.outTok = m.InputTokens, m.OutputTokens
		return nil
	case "task_complete":
		return []Event{{Kind: KindTurnEnd, Result: &TurnResult{
			SessionID: p.threadID, InputTokens: p.inTok, OutputTokens: p.outTok}}}
	case "error":
		return []Event{{Kind: KindError, Text: m.Message}}
	case "task_started":
		return nil
	default:
		p.Skipped++
		return nil
	}
}
```

- [ ] **Step 5: Implement `codex.go`**

```go
package agent

import (
	"context"
	"fmt"
	"os/exec"
)

// Codex drives the official OpenAI Codex CLI headlessly (`codex exec --json`).
// Resume is by explicit thread id — never `--last`, which races with any codex
// the user runs elsewhere.
type Codex struct {
	bin      string
	extraEnv []string
}

func NewCodex() *Codex { return &Codex{bin: "codex"} }

func (c *Codex) Name() string  { return "codex" }
func (c *Codex) Label() string { return "Codex" }

func (c *Codex) Available() error {
	if _, err := exec.LookPath(c.bin); err != nil {
		return fmt.Errorf("codex CLI not found — install Codex and run `codex` once to log in")
	}
	return nil
}

func (c *Codex) args(opts RunOptions) []string {
	if opts.SessionID != "" {
		return []string{"exec", "resume", opts.SessionID, "--json", "--full-auto", opts.Prompt}
	}
	return []string{"exec", "--json", "--full-auto", opts.Prompt}
}

func (c *Codex) Run(ctx context.Context, opts RunOptions) (<-chan Event, error) {
	return runCLI(ctx, c.bin, c.args(opts), opts.Dir, c.extraEnv, &CodexParser{})
}
```

Update `NewRegistry`: `Agents: []Agent{NewClaude(), NewCodex(), NewMock()}`.

- [ ] **Step 6: Verify the `--full-auto` flag is accepted by `exec resume`**

Run: `codex exec resume --help` and check `--full-auto` is listed.
If it is NOT: replace it in `args` with `-c sandbox_mode="workspace-write"` for the resume path only, and adjust `TestCodexArgs` accordingly.

- [ ] **Step 7: Run tests** — `go test ./internal/agent/ -v` → PASS (entire package, both adapters).

- [ ] **Step 8: Commit**

```bash
git add internal/agent/codex_parser.go internal/agent/codex.go internal/agent/codex_test.go internal/agent/testdata/ internal/agent/registry.go
git commit -m "feat: codex adapter parsing both current item.* and legacy msg schemas"
```

---

### Task 8: Git diff collection and unified-diff parsing

**Files:**
- Create: `internal/gitdiff/gitdiff.go`, `internal/gitdiff/gitdiff_test.go`

**Interfaces:**
- Produces (used by Tasks 11 and 12):

```go
package gitdiff

type LineKind int
const (
	LineContext LineKind = iota
	LineAdd
	LineDel
)

type Line struct { Kind LineKind; Text string } // Text has the +/-/space marker stripped
type Hunk struct { Header string; Lines []Line }

type File struct {
	Path      string // new path; for deletions, the deleted path
	OldPath   string // set when renamed
	Status    string // "added" | "modified" | "deleted" | "renamed" | "untracked"
	Staged    bool
	Binary    bool
	Additions int
	Deletions int
	Hunks     []Hunk
	Note      string // e.g. "binary file", "untracked, 1.2 MB — not rendered"
}

type DiffSet struct {
	Repo      string // git toplevel; "" when not a repo
	Files     []File
	Additions int
	Deletions int
	Err       string // human-readable; UI shows it instead of the file list
}

func Collect(dir string) DiffSet                       // runs git; never panics
func ParseUnified(raw string, staged bool) []File      // pure
```

- [ ] **Step 1: Write failing tests** (`gitdiff_test.go`)

```go
package gitdiff

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.name", "Crema Test")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func find(ds DiffSet, path string, staged bool) *File {
	for i := range ds.Files {
		if ds.Files[i].Path == path && ds.Files[i].Staged == staged {
			return &ds.Files[i]
		}
	}
	return nil
}

func TestCollectOutsideRepoReportsError(t *testing.T) {
	ds := Collect(t.TempDir())
	if ds.Err == "" {
		t.Fatal("want an Err for a non-repo directory")
	}
	if len(ds.Files) != 0 {
		t.Fatalf("want no files, got %d", len(ds.Files))
	}
}

func TestCollectStagedUnstagedUntracked(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\ntwo\n")
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-qm", "init")

	write(t, dir, "a.txt", "one\ntwo\nthree\n") // unstaged modification
	write(t, dir, "b.txt", "brand new\n")
	gitRun(t, dir, "add", "b.txt") // staged addition
	write(t, dir, "c.txt", "untracked\n")

	ds := Collect(dir)
	if ds.Err != "" {
		t.Fatalf("unexpected Err: %s", ds.Err)
	}
	if ds.Repo == "" {
		t.Fatal("Repo must be set inside a repository")
	}
	a := find(ds, "a.txt", false)
	if a == nil || a.Status != "modified" || a.Additions != 1 {
		t.Fatalf("a.txt unstaged: %+v", a)
	}
	if len(a.Hunks) == 0 || !strings.HasPrefix(a.Hunks[0].Header, "@@") {
		t.Fatalf("a.txt hunks: %+v", a.Hunks)
	}
	b := find(ds, "b.txt", true)
	if b == nil || b.Status != "added" || b.Additions != 1 {
		t.Fatalf("b.txt staged: %+v", b)
	}
	c := find(ds, "c.txt", false)
	if c == nil || c.Status != "untracked" || c.Additions != 1 {
		t.Fatalf("c.txt untracked: %+v", c)
	}
	if c.Hunks[0].Lines[0].Kind != LineAdd || c.Hunks[0].Lines[0].Text != "untracked" {
		t.Fatalf("untracked body: %+v", c.Hunks[0].Lines)
	}
	if ds.Additions != 3 {
		t.Fatalf("total additions = %d, want 3", ds.Additions)
	}
}

func TestCollectCleanRepoIsEmpty(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.txt", "x\n")
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-qm", "init")
	ds := Collect(dir)
	if ds.Err != "" || len(ds.Files) != 0 || ds.Additions != 0 {
		t.Fatalf("clean repo should be empty: %+v", ds)
	}
}

func TestCollectUntrackedBinaryIsNotedNotRendered(t *testing.T) {
	dir := newRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{0x00, 0x01, 0x02, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	ds := Collect(dir)
	f := find(ds, "blob.bin", false)
	if f == nil || !f.Binary || f.Note == "" || len(f.Hunks) != 0 {
		t.Fatalf("binary untracked: %+v", f)
	}
}

func TestParseUnifiedRename(t *testing.T) {
	raw := "diff --git a/old.txt b/new.txt\nsimilarity index 100%\nrename from old.txt\nrename to new.txt\n"
	files := ParseUnified(raw, true)
	if len(files) != 1 {
		t.Fatalf("files = %+v", files)
	}
	f := files[0]
	if f.Status != "renamed" || f.OldPath != "old.txt" || f.Path != "new.txt" || !f.Staged {
		t.Fatalf("rename: %+v", f)
	}
}

func TestParseUnifiedBinaryAndDeleted(t *testing.T) {
	raw := "diff --git a/img.png b/img.png\nnew file mode 100644\nindex 0000000..abc1234\nBinary files /dev/null and b/img.png differ\n" +
		"diff --git a/gone.txt b/gone.txt\ndeleted file mode 100644\nindex 1234567..0000000\n--- a/gone.txt\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-bye\n-now\n"
	files := ParseUnified(raw, false)
	if len(files) != 2 {
		t.Fatalf("files = %+v", files)
	}
	if !files[0].Binary || files[0].Status != "added" || files[0].Path != "img.png" {
		t.Fatalf("binary: %+v", files[0])
	}
	g := files[1]
	if g.Status != "deleted" || g.Path != "gone.txt" || g.Deletions != 2 || g.Additions != 0 {
		t.Fatalf("deleted: %+v", g)
	}
}

func TestParseUnifiedDoesNotMistakeBodyLinesForHeaders(t *testing.T) {
	// A removed line whose content starts with "-- " renders as "--- " in the body.
	raw := "diff --git a/x.md b/x.md\nindex 1..2 100644\n--- a/x.md\n+++ b/x.md\n@@ -1,3 +1,3 @@\n context\n--- signature\n+++ new signature\n"
	files := ParseUnified(raw, false)
	f := files[0]
	if f.Path != "x.md" || f.Additions != 1 || f.Deletions != 1 {
		t.Fatalf("counts: %+v", f)
	}
	lines := f.Hunks[0].Lines
	if len(lines) != 3 || lines[1].Kind != LineDel || lines[1].Text != "-- signature" {
		t.Fatalf("body lines: %+v", lines)
	}
	if lines[2].Kind != LineAdd || lines[2].Text != "++ new signature" {
		t.Fatalf("add line: %+v", lines[2])
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/gitdiff/ -v` → FAIL: undefined `Collect`.

- [ ] **Step 3: Implement `gitdiff.go`**

```go
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
```

- [ ] **Step 4: Run tests** — `go test ./internal/gitdiff/ -v` → PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/gitdiff/
git commit -m "feat: git diff collection with unified-diff parser and untracked synthesis"
```

---

### Task 9: Theme and pure block renderers

**Files:**
- Create: `internal/ui/theme.go`, `internal/ui/blocks.go`, `internal/ui/blocks_test.go`
- Modify: `go.mod` (add lipgloss, termenv)

**Interfaces:**
- Consumes: `agent` types.
- Produces: `Theme` struct + `var T = defaultTheme()`; renderers used by Tasks 10–12:
  `RenderUser(text string, w int) string`, `RenderAssistant`, `RenderThinking`, `RenderTool(name, input string, w int) string`, `RenderToolOutput(content string, isErr bool, w int) string`, `RenderError(text string, w int) string`, `RenderSystem(text string, w int) string`, `RenderStats(r *agent.TurnResult, w int) string`, `Truncate(s string, max int) (string, int)`, `const MaxOutputLines = 4000`.

- [ ] **Step 1: Add dependencies**

```bash
go get github.com/charmbracelet/lipgloss@v1.1.0
go get github.com/charmbracelet/bubbletea@v1.3.10
go get github.com/charmbracelet/bubbles@v0.21.0
go get github.com/muesli/termenv@latest
go get github.com/charmbracelet/x/ansi@latest
```

(Task 10+ need bubbletea/bubbles; installing now keeps `go.mod` churn in one commit. Verify none of these resolved to a v2 pre-release: `go list -m all | grep charmbracelet` must show v1.x for bubbletea and lipgloss.)

- [ ] **Step 2: Write failing tests** (`blocks_test.go`)

```go
package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii) // strip ANSI so assertions see plain text
	os.Exit(m.Run())
}

func TestRenderToolShowsNameAndFullInput(t *testing.T) {
	out := RenderTool("Bash", "go test ./...\ngo vet ./...", 60)
	if !strings.Contains(out, "Bash") {
		t.Fatalf("missing tool name:\n%s", out)
	}
	for _, want := range []string{"go test ./...", "go vet ./..."} {
		if !strings.Contains(out, want) {
			t.Fatalf("input line %q missing:\n%s", want, out)
		}
	}
	for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if lipgloss.Width(ln) > 60 {
			t.Fatalf("line exceeds width 60 (%d): %q", lipgloss.Width(ln), ln)
		}
	}
}

func TestRenderToolOutputNeverSilentlyFolds(t *testing.T) {
	body := strings.Repeat("line\n", 50)
	out := RenderToolOutput(body, false, 60)
	if strings.Count(out, "line") != 50 {
		t.Fatalf("all 50 lines must render, got %d", strings.Count(out, "line"))
	}
	if strings.Contains(out, "truncated") {
		t.Fatal("must not claim truncation when nothing was truncated")
	}
}

func TestRenderToolOutputLabelsTruncation(t *testing.T) {
	body := strings.Repeat("x\n", MaxOutputLines+25) // trailing \n is trimmed → 4025 lines
	out := RenderToolOutput(body, false, 60)
	if !strings.Contains(out, "truncated") {
		t.Fatal("truncation must be announced")
	}
	if !strings.Contains(out, "+25 lines truncated") {
		t.Fatal("the truncation label must carry the exact dropped-line count")
	}
}

func TestTruncateCountsRemainder(t *testing.T) {
	s, n := Truncate("a\nb\nc\nd", 2)
	if s != "a\nb" || n != 2 {
		t.Fatalf("Truncate = %q, %d", s, n)
	}
	if s2, n2 := Truncate("a\nb", 5); s2 != "a\nb" || n2 != 0 {
		t.Fatalf("under cap must be untouched: %q, %d", s2, n2)
	}
}

func TestRenderStatsIncludesCostAndDuration(t *testing.T) {
	out := RenderStats(&agent.TurnResult{DurationMS: 7557, CostUSD: 0.293986, InputTokens: 2, OutputTokens: 58}, 60)
	for _, want := range []string{"7.6s", "$0.2940", "58"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stats missing %q:\n%s", want, out)
		}
	}
}

func TestRenderStatsOmitsZeroCost(t *testing.T) {
	out := RenderStats(&agent.TurnResult{DurationMS: 1000, OutputTokens: 5}, 60)
	if strings.Contains(out, "$") {
		t.Fatalf("codex reports no USD; must omit the cost segment:\n%s", out)
	}
}

func TestRenderErrorIsVisiblyMarked(t *testing.T) {
	out := RenderError("credential expired", 60)
	if !strings.Contains(out, "credential expired") || !strings.Contains(strings.ToLower(out), "error") {
		t.Fatalf("error block: %s", out)
	}
}

func TestRenderersHandleTinyWidth(t *testing.T) {
	for _, w := range []int{0, 1, 8} {
		_ = RenderUser("hello there friend", w)
		_ = RenderTool("Bash", "echo hi", w)
		_ = RenderToolOutput("some output", true, w)
		_ = RenderThinking("pondering", w)
	} // must not panic
}
```

- [ ] **Step 3: Run to verify failure** — `go test ./internal/ui/ -v` → FAIL: undefined renderers.

- [ ] **Step 4: Implement `theme.go`**

```go
package ui

import "github.com/charmbracelet/lipgloss"

// Theme is the pink/purple dark palette. M2 will make this swappable.
type Theme struct {
	Pink, Magenta, Purple, Lilac, Muted, Fg lipgloss.Color
	Green, Red, Yellow                      lipgloss.Color
	Surface                                 lipgloss.Color
}

var T = Theme{
	Pink:    lipgloss.Color("#f5a9d0"),
	Magenta: lipgloss.Color("#e06bb8"),
	Purple:  lipgloss.Color("#b47cf0"),
	Lilac:   lipgloss.Color("#d9c7f0"),
	Muted:   lipgloss.Color("#8b7fa0"),
	Fg:      lipgloss.Color("#efe6f7"),
	Green:   lipgloss.Color("#8fe0a8"),
	Red:     lipgloss.Color("#ff8f9e"),
	Yellow:  lipgloss.Color("#ffd9a0"),
	Surface: lipgloss.Color("#211a2b"),
}
```

(`max` used throughout the `ui` package is the Go 1.21 builtin — do not define one.)

- [ ] **Step 5: Implement `blocks.go`**

```go
package ui

import (
	"fmt"
	"strings"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/charmbracelet/lipgloss"
)

// MaxOutputLines caps a single tool output. Anything beyond is dropped with a
// visible, counted label — crema never folds output silently.
const MaxOutputLines = 4000

func body(w int) lipgloss.Style {
	return lipgloss.NewStyle().Width(max(1, w))
}

// rail prefixes every line with a colored vertical guide.
func rail(s string, c lipgloss.Color) string {
	bar := lipgloss.NewStyle().Foreground(c).Render("│")
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, ln := range lines {
		lines[i] = bar + " " + ln
	}
	return strings.Join(lines, "\n")
}

func Truncate(s string, maxLines int) (string, int) {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= maxLines {
		return strings.TrimRight(s, "\n"), 0
	}
	return strings.Join(lines[:maxLines], "\n"), len(lines) - maxLines
}

func RenderUser(text string, w int) string {
	head := lipgloss.NewStyle().Foreground(T.Pink).Bold(true).Render("❯ you")
	return head + "\n" + body(w).Foreground(T.Fg).Render(text) + "\n"
}

func RenderAssistant(text string, w int) string {
	return body(w).Foreground(T.Fg).Render(text) + "\n"
}

func RenderThinking(text string, w int) string {
	head := lipgloss.NewStyle().Foreground(T.Muted).Italic(true).Render("✳ thinking")
	txt := body(max(1, w-2)).Foreground(T.Muted).Italic(true).Render(text)
	return head + "\n" + rail(txt, T.Muted) + "\n"
}

func RenderTool(name, input string, w int) string {
	head := lipgloss.NewStyle().Foreground(T.Purple).Bold(true).Render("⏵ " + name)
	if strings.TrimSpace(input) == "" {
		return head + "\n"
	}
	txt := body(max(1, w-2)).Foreground(T.Lilac).Render(input)
	return head + "\n" + rail(txt, T.Purple) + "\n"
}

func RenderToolOutput(content string, isErr bool, w int) string {
	fg, railC := T.Muted, T.Purple
	if isErr {
		fg, railC = T.Red, T.Red
	}
	shown, cut := Truncate(content, MaxOutputLines)
	if strings.TrimSpace(shown) == "" && cut == 0 {
		return rail(lipgloss.NewStyle().Foreground(T.Muted).Render("(no output)"), railC) + "\n"
	}
	txt := body(max(1, w-2)).Foreground(fg).Render(shown)
	if cut > 0 {
		label := fmt.Sprintf("… +%d lines truncated (crema cap %d)", cut, MaxOutputLines)
		txt += "\n" + lipgloss.NewStyle().Foreground(T.Yellow).Bold(true).Render(label)
	}
	return rail(txt, railC) + "\n"
}

func RenderError(text string, w int) string {
	head := lipgloss.NewStyle().Foreground(T.Red).Bold(true).Render("✖ error")
	txt := body(max(1, w-2)).Foreground(T.Red).Render(text)
	return head + "\n" + rail(txt, T.Red) + "\n"
}

func RenderSystem(text string, w int) string {
	return body(w).Foreground(T.Muted).Italic(true).Render("· "+text) + "\n"
}

func RenderStats(r *agent.TurnResult, w int) string {
	if r == nil {
		return ""
	}
	segs := []string{fmt.Sprintf("%.1fs", float64(r.DurationMS)/1000)}
	if r.CostUSD > 0 {
		segs = append(segs, fmt.Sprintf("$%.4f", r.CostUSD))
	}
	if r.InputTokens > 0 || r.OutputTokens > 0 {
		segs = append(segs, fmt.Sprintf("%d↑ %d↓ tok", r.InputTokens, r.OutputTokens))
	}
	mark := "✔"
	c := T.Green
	switch {
	case r.Canceled:
		mark, c = "⨯ canceled", T.Yellow
	case r.Err != "":
		mark, c = "✖ failed", T.Red
	}
	line := mark + " " + strings.Join(segs, " · ")
	return body(w).Foreground(c).Render(line) + "\n"
}
```

- [ ] **Step 6: Run tests** — `go test ./internal/ui/ -v` → PASS (8 tests).

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/ui/theme.go internal/ui/blocks.go internal/ui/blocks_test.go
git commit -m "feat: pink/purple theme and always-expanded block renderers"
```

---

### Task 10: Timeline pane (block state, render cache, follow mode)

**Files:**
- Create: `internal/ui/timeline.go`, `internal/ui/timeline_test.go`

**Interfaces:**
- Consumes: renderers from Task 9, `agent.Event` from Task 2.
- Produces:

```go
type BlockKind int
const (
	BlockUser BlockKind = iota
	BlockAssistant
	BlockThinking
	BlockTool
	BlockToolOutput
	BlockError
	BlockSystem
	BlockStats
)

type Block struct {
	Kind   BlockKind
	Name   string // tool name for BlockTool
	Text   string
	IsErr  bool
	Result *agent.TurnResult // BlockStats
}

type Timeline struct { /* unexported */ }

func NewTimeline(w, h int) *Timeline
func (t *Timeline) SetSize(w, h int)
func (t *Timeline) Append(b Block)
func (t *Timeline) AppendEvent(ev agent.Event) // agent.Event → Block
func (t *Timeline) Content() string            // joined rendered blocks
func (t *Timeline) View() string
func (t *Timeline) Update(msg tea.Msg) tea.Cmd
func (t *Timeline) Len() int
func (t *Timeline) Following() bool
```

**Note:** `TestMain` already exists in `blocks_test.go` (same package) — do not add another.

- [ ] **Step 1: Write failing tests** (`timeline_test.go`)

```go
package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

func TestAppendEventMapsEveryKind(t *testing.T) {
	tl := NewTimeline(60, 10)
	tl.AppendEvent(agent.Event{Kind: agent.KindText, Text: "hello"})
	tl.AppendEvent(agent.Event{Kind: agent.KindText, Thinking: true, Text: "hmm"})
	tl.AppendEvent(agent.Event{Kind: agent.KindToolCall, Tool: &agent.ToolCall{Name: "Bash", Input: "ls -la"}})
	tl.AppendEvent(agent.Event{Kind: agent.KindToolOutput, Output: &agent.ToolOutput{Content: "a.txt"}})
	tl.AppendEvent(agent.Event{Kind: agent.KindError, Text: "went wrong"})
	tl.AppendEvent(agent.Event{Kind: agent.KindTurnEnd, Result: &agent.TurnResult{DurationMS: 1000}})
	if tl.Len() != 6 {
		t.Fatalf("Len = %d, want 6", tl.Len())
	}
	c := tl.Content()
	for _, want := range []string{"hello", "hmm", "Bash", "ls -la", "a.txt", "went wrong"} {
		if !strings.Contains(c, want) {
			t.Fatalf("content missing %q:\n%s", want, c)
		}
	}
}

func TestContentRerendersOnWidthChange(t *testing.T) {
	tl := NewTimeline(80, 10)
	long := strings.Repeat("word ", 40)
	tl.Append(Block{Kind: BlockAssistant, Text: long})
	wide := len(strings.Split(tl.Content(), "\n"))
	tl.SetSize(30, 10)
	narrow := len(strings.Split(tl.Content(), "\n"))
	if narrow <= wide {
		t.Fatalf("narrower width must wrap into more lines: wide=%d narrow=%d", wide, narrow)
	}
}

func TestFollowStaysAtBottomUntilUserScrollsUp(t *testing.T) {
	tl := NewTimeline(40, 5)
	for i := 0; i < 40; i++ {
		tl.Append(Block{Kind: BlockAssistant, Text: "line"})
	}
	if !tl.Following() {
		t.Fatal("should auto-follow new output")
	}
	tl.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if tl.Following() {
		t.Fatal("scrolling up must stop follow mode")
	}
	tl.Append(Block{Kind: BlockAssistant, Text: "more"})
	if tl.Following() {
		t.Fatal("appending must not yank a scrolled-back user to the bottom")
	}
	tl.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !tl.Following() {
		t.Fatal("End must resume follow mode")
	}
}

func TestViewFitsRequestedHeight(t *testing.T) {
	tl := NewTimeline(40, 6)
	for i := 0; i < 30; i++ {
		tl.Append(Block{Kind: BlockAssistant, Text: "x"})
	}
	if got := len(strings.Split(tl.View(), "\n")); got != 6 {
		t.Fatalf("View height = %d, want 6", got)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/ui/ -run 'TestAppend|TestContent|TestFollow|TestViewFits' -v` → FAIL: `NewTimeline` undefined.

- [ ] **Step 3: Implement `timeline.go`**

```go
package ui

import (
	"strings"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type BlockKind int

const (
	BlockUser BlockKind = iota
	BlockAssistant
	BlockThinking
	BlockTool
	BlockToolOutput
	BlockError
	BlockSystem
	BlockStats
)

type Block struct {
	Kind   BlockKind
	Name   string
	Text   string
	IsErr  bool
	Result *agent.TurnResult
}

// Timeline owns the conversation blocks and their scroll state. Rendered text
// is cached per block and invalidated only when the width changes, so a long
// session doesn't re-render everything on every event.
type Timeline struct {
	blocks   []Block
	rendered []string
	vp       viewport.Model
	width    int
	follow   bool
}

func NewTimeline(w, h int) *Timeline {
	vp := viewport.New(max(1, w), max(1, h))
	return &Timeline{vp: vp, width: max(1, w), follow: true}
}

func (t *Timeline) SetSize(w, h int) {
	w, h = max(1, w), max(1, h)
	if w != t.width {
		t.width = w
		t.rendered = t.rendered[:0]
		for _, b := range t.blocks {
			t.rendered = append(t.rendered, renderBlock(b, t.width))
		}
	}
	t.vp.Width, t.vp.Height = w, h
	t.sync()
}

func (t *Timeline) Len() int         { return len(t.blocks) }
func (t *Timeline) Following() bool  { return t.follow }

func (t *Timeline) Append(b Block) {
	t.blocks = append(t.blocks, b)
	t.rendered = append(t.rendered, renderBlock(b, t.width))
	t.sync()
}

func (t *Timeline) AppendEvent(ev agent.Event) {
	switch ev.Kind {
	case agent.KindText:
		if ev.Thinking {
			t.Append(Block{Kind: BlockThinking, Text: ev.Text})
			return
		}
		t.Append(Block{Kind: BlockAssistant, Text: ev.Text})
	case agent.KindToolCall:
		if ev.Tool != nil {
			t.Append(Block{Kind: BlockTool, Name: ev.Tool.Name, Text: ev.Tool.Input})
		}
	case agent.KindToolOutput:
		if ev.Output != nil {
			t.Append(Block{Kind: BlockToolOutput, Text: ev.Output.Content, IsErr: ev.Output.IsError})
		}
	case agent.KindError:
		t.Append(Block{Kind: BlockError, Text: ev.Text})
	case agent.KindTurnEnd:
		if ev.Result != nil && ev.Result.Err != "" {
			t.Append(Block{Kind: BlockError, Text: ev.Result.Err})
		}
		t.Append(Block{Kind: BlockStats, Result: ev.Result})
	}
}

func (t *Timeline) Content() string { return strings.Join(t.rendered, "") }

func (t *Timeline) sync() {
	t.vp.SetContent(t.Content())
	if t.follow {
		t.vp.GotoBottom()
	}
}

func (t *Timeline) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	t.vp, cmd = t.vp.Update(msg)
	t.follow = t.vp.AtBottom()
	return cmd
}

func (t *Timeline) View() string { return t.vp.View() }

func renderBlock(b Block, w int) string {
	switch b.Kind {
	case BlockUser:
		return RenderUser(b.Text, w)
	case BlockThinking:
		return RenderThinking(b.Text, w)
	case BlockTool:
		return RenderTool(b.Name, b.Text, w)
	case BlockToolOutput:
		return RenderToolOutput(b.Text, b.IsErr, w)
	case BlockError:
		return RenderError(b.Text, w)
	case BlockSystem:
		return RenderSystem(b.Text, w)
	case BlockStats:
		return RenderStats(b.Result, w)
	default:
		return RenderAssistant(b.Text, w)
	}
}
```

Note: `viewport`'s default key map binds PgUp/PgDn/Home/End — `End` reaching the bottom is what re-enables follow via `AtBottom()`.

- [ ] **Step 4: Run tests** — `go test ./internal/ui/ -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/timeline.go internal/ui/timeline_test.go
git commit -m "feat: timeline pane with cached block rendering and follow mode"
```

---

### Task 11: Diff pane, input box, status bar

**Files:**
- Create: `internal/ui/diffpanel.go`, `internal/ui/input.go`, `internal/ui/statusbar.go`, `internal/ui/diffpanel_test.go`, `internal/ui/statusbar_test.go`

**Interfaces:**
- Consumes: `gitdiff.DiffSet` (T8), theme (T9).
- Produces:

```go
func RenderDiffSet(ds gitdiff.DiffSet, w int) string // pure
type DiffPanel struct{ /* … */ }
func NewDiffPanel(w, h int) *DiffPanel
func (d *DiffPanel) SetSize(w, h int)
func (d *DiffPanel) SetDiff(ds gitdiff.DiffSet)
func (d *DiffPanel) Update(msg tea.Msg) tea.Cmd
func (d *DiffPanel) View() string

type Input struct{ /* … */ }
func NewInput(w int) *Input
func (i *Input) SetWidth(w int)
func (i *Input) Value() string
func (i *Input) Reset()
func (i *Input) Focus() tea.Cmd
func (i *Input) Blur()
func (i *Input) Update(msg tea.Msg) tea.Cmd
func (i *Input) View() string
const InputHeight = 3

type StatusData struct {
	Agent, Mode, Dir, Spin, Note string
	Busy                         bool
	ElapsedSec                   float64
	Cost                         float64
	Adds, Dels                   int
}
func RenderStatus(s StatusData, w int) string // always exactly w columns, 1 line
```

- [ ] **Step 1: Write failing tests** (`diffpanel_test.go`)

```go
package ui

import (
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/gitdiff"
	"github.com/charmbracelet/lipgloss"
)

func sampleDiff() gitdiff.DiffSet {
	return gitdiff.DiffSet{
		Repo: "/repo",
		Files: []gitdiff.File{
			{Path: "staged.go", Status: "added", Staged: true, Additions: 1,
				Hunks: []gitdiff.Hunk{{Header: "@@ -0,0 +1 @@", Lines: []gitdiff.Line{{Kind: gitdiff.LineAdd, Text: "package main"}}}}},
			{Path: "work.go", Status: "modified", Additions: 1, Deletions: 1,
				Hunks: []gitdiff.Hunk{{Header: "@@ -1,2 +1,2 @@", Lines: []gitdiff.Line{
					{Kind: gitdiff.LineContext, Text: "ctx"},
					{Kind: gitdiff.LineDel, Text: "old"},
					{Kind: gitdiff.LineAdd, Text: "new"},
				}}}},
			{Path: "notes.md", Status: "untracked", Note: "untracked binary file", Binary: true},
		},
		Additions: 2, Deletions: 1,
	}
}

func TestRenderDiffSetGroupsAndMarksLines(t *testing.T) {
	out := RenderDiffSet(sampleDiff(), 50)
	for _, want := range []string{"STAGED", "UNSTAGED", "UNTRACKED", "staged.go", "work.go", "notes.md",
		"@@ -1,2 +1,2 @@", "+new", "-old", "untracked binary file"} {
		if !strings.Contains(out, want) {
			t.Fatalf("diff render missing %q:\n%s", want, out)
		}
	}
}

func TestRenderDiffSetNeverExceedsWidth(t *testing.T) {
	ds := sampleDiff()
	ds.Files[1].Hunks[0].Lines[0].Text = strings.Repeat("verylongtoken", 40)
	for _, w := range []int{20, 34, 80} {
		for _, ln := range strings.Split(RenderDiffSet(ds, w), "\n") {
			if lipgloss.Width(ln) > w {
				t.Fatalf("width %d exceeded (%d): %q", w, lipgloss.Width(ln), ln)
			}
		}
	}
}

func TestRenderDiffSetEmptyAndError(t *testing.T) {
	if out := RenderDiffSet(gitdiff.DiffSet{Repo: "/r"}, 40); !strings.Contains(out, "clean") {
		t.Fatalf("empty diff should say clean: %q", out)
	}
	out := RenderDiffSet(gitdiff.DiffSet{Err: "not a git repository (diff panel needs one)"}, 40)
	if !strings.Contains(out, "not a git repository") {
		t.Fatalf("error must surface: %q", out)
	}
}
```

`statusbar_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderStatusIsExactlyOneLineOfWidth(t *testing.T) {
	s := StatusData{Agent: "Claude Code", Mode: "acceptEdits", Dir: "D:/Crema",
		Busy: true, Spin: "⠋", ElapsedSec: 12.5, Cost: 0.42, Adds: 10, Dels: 3}
	for _, w := range []int{80, 100, 40, 20} {
		out := RenderStatus(s, w)
		if strings.Contains(out, "\n") {
			t.Fatalf("status must be one line at w=%d", w)
		}
		if lipgloss.Width(out) != w {
			t.Fatalf("width = %d, want %d: %q", lipgloss.Width(out), w, out)
		}
	}
}

func TestRenderStatusShowsAgentAndPermissionMode(t *testing.T) {
	out := RenderStatus(StatusData{Agent: "Codex", Mode: "full-auto", Adds: 1}, 100)
	if !strings.Contains(out, "Codex") || !strings.Contains(out, "full-auto") {
		t.Fatalf("status: %q", out)
	}
}

func TestRenderStatusIdleHidesTimer(t *testing.T) {
	out := RenderStatus(StatusData{Agent: "Codex", Mode: "full-auto", ElapsedSec: 9}, 100)
	if strings.Contains(out, "9.0s") {
		t.Fatalf("idle status must not show a running timer: %q", out)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/ui/ -run 'TestRenderDiffSet|TestRenderStatus' -v` → FAIL: undefined.

- [ ] **Step 3: Implement `diffpanel.go`**

```go
package ui

import (
	"fmt"
	"strings"

	"github.com/ShukunCheng/Crema/internal/gitdiff"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// clip hard-cuts a line to w display columns, marking the cut with "›" so a
// clipped line is never mistaken for the whole line. Width-aware, so CJK and
// emoji in paths don't overflow the pane.
func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = strings.ReplaceAll(s, "\t", "    ")
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "›")
}

func RenderDiffSet(ds gitdiff.DiffSet, w int) string {
	if w <= 0 {
		return ""
	}
	var b strings.Builder
	dim := lipgloss.NewStyle().Foreground(T.Muted)
	if ds.Err != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(T.Yellow).Render(clip(ds.Err, w)) + "\n")
		return b.String()
	}
	if len(ds.Files) == 0 {
		b.WriteString(dim.Render(clip("working tree clean", w)) + "\n")
		return b.String()
	}
	sections := []struct {
		title string
		pick  func(gitdiff.File) bool
	}{
		{"STAGED", func(f gitdiff.File) bool { return f.Staged }},
		{"UNSTAGED", func(f gitdiff.File) bool { return !f.Staged && f.Status != "untracked" }},
		{"UNTRACKED", func(f gitdiff.File) bool { return f.Status == "untracked" }},
	}
	for _, sec := range sections {
		var files []gitdiff.File
		for _, f := range ds.Files {
			if sec.pick(f) {
				files = append(files, f)
			}
		}
		if len(files) == 0 {
			continue
		}
		b.WriteString(lipgloss.NewStyle().Foreground(T.Magenta).Bold(true).
			Render(clip("── "+sec.title+" ", w)) + "\n")
		for _, f := range files {
			b.WriteString(renderDiffFile(f, w))
		}
	}
	return b.String()
}

func renderDiffFile(f gitdiff.File, w int) string {
	var b strings.Builder
	name := f.Path
	if f.Status == "renamed" && f.OldPath != "" {
		name = f.OldPath + " → " + f.Path
	}
	head := fmt.Sprintf("%s %s  +%d −%d", statusGlyph(f.Status), name, f.Additions, f.Deletions)
	b.WriteString(lipgloss.NewStyle().Foreground(T.Pink).Bold(true).Render(clip(head, w)) + "\n")
	if f.Note != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(T.Yellow).Render(clip("  "+f.Note, w)) + "\n")
	}
	add := lipgloss.NewStyle().Foreground(T.Green)
	del := lipgloss.NewStyle().Foreground(T.Red)
	ctx := lipgloss.NewStyle().Foreground(T.Muted)
	hdr := lipgloss.NewStyle().Foreground(T.Purple)
	for _, h := range f.Hunks {
		b.WriteString(hdr.Render(clip(h.Header, w)) + "\n")
		for _, ln := range h.Lines {
			switch ln.Kind {
			case gitdiff.LineAdd:
				b.WriteString(add.Render(clip("+"+ln.Text, w)) + "\n")
			case gitdiff.LineDel:
				b.WriteString(del.Render(clip("-"+ln.Text, w)) + "\n")
			default:
				b.WriteString(ctx.Render(clip(" "+ln.Text, w)) + "\n")
			}
		}
	}
	return b.String()
}

func statusGlyph(status string) string {
	switch status {
	case "added":
		return "✚"
	case "deleted":
		return "✖"
	case "renamed":
		return "➜"
	case "untracked":
		return "?"
	default:
		return "●"
	}
}

type DiffPanel struct {
	ds    gitdiff.DiffSet
	vp    viewport.Model
	width int
}

func NewDiffPanel(w, h int) *DiffPanel {
	return &DiffPanel{vp: viewport.New(max(1, w), max(1, h)), width: max(1, w)}
}

func (d *DiffPanel) SetSize(w, h int) {
	d.width = max(1, w)
	d.vp.Width, d.vp.Height = max(1, w), max(1, h)
	d.vp.SetContent(RenderDiffSet(d.ds, d.width))
}

// SetDiff replaces the content, keeping the scroll offset when possible so a
// background refresh doesn't jump the pane under the user.
func (d *DiffPanel) SetDiff(ds gitdiff.DiffSet) {
	off := d.vp.YOffset
	d.ds = ds
	d.vp.SetContent(RenderDiffSet(ds, d.width))
	d.vp.SetYOffset(off)
}

func (d *DiffPanel) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	d.vp, cmd = d.vp.Update(msg)
	return cmd
}

func (d *DiffPanel) View() string { return d.vp.View() }
```

- [ ] **Step 4: Implement `input.go`**

```go
package ui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// InputHeight is the total rows the input box occupies (border + one row).
const InputHeight = 3

type Input struct {
	ta    textarea.Model
	width int
}

func NewInput(w int) *Input {
	ta := textarea.New()
	ta.Placeholder = "ask the agent…  (enter to send · alt+enter newline · tab switch agent · esc cancel)"
	ta.Prompt = "❯ "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(1)
	ta.SetWidth(max(1, w-2))
	// Enter is reserved for "send" by the app; newline moves to alt+enter/ctrl+j.
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("alt+enter", "ctrl+j"), key.WithHelp("alt+enter", "newline"))
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(T.Pink)
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(T.Muted)
	ta.Focus()
	return &Input{ta: ta, width: max(1, w)}
}

func (i *Input) SetWidth(w int) {
	i.width = max(1, w)
	i.ta.SetWidth(max(1, w-2))
}

func (i *Input) Value() string     { return i.ta.Value() }
func (i *Input) Reset()            { i.ta.Reset() }
func (i *Input) Focus() tea.Cmd    { return i.ta.Focus() }
func (i *Input) Blur()             { i.ta.Blur() }
func (i *Input) Focused() bool     { return i.ta.Focused() }

func (i *Input) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	i.ta, cmd = i.ta.Update(msg)
	return cmd
}

func (i *Input) View() string {
	c := T.Purple
	if !i.ta.Focused() {
		c = T.Muted
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(c).
		Width(max(1, i.width - 2)).
		Render(i.ta.View())
}
```

- [ ] **Step 5: Implement `statusbar.go`**

```go
package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type StatusData struct {
	Agent, Mode, Dir, Spin, Note string
	Busy                         bool
	ElapsedSec                   float64
	Cost                         float64
	Adds, Dels                   int
}

// RenderStatus returns exactly one line of exactly w columns.
func RenderStatus(s StatusData, w int) string {
	if w <= 0 {
		return ""
	}
	left := []string{}
	if s.Busy {
		left = append(left, s.Spin+" "+s.Agent, fmt.Sprintf("%.1fs", s.ElapsedSec))
	} else {
		left = append(left, "● "+s.Agent)
	}
	left = append(left, s.Mode)
	if s.Cost > 0 {
		left = append(left, fmt.Sprintf("$%.4f", s.Cost))
	}
	left = append(left, fmt.Sprintf("+%d −%d", s.Adds, s.Dels))
	if s.Note != "" {
		left = append(left, s.Note)
	}
	line := " " + strings.Join(left, " · ")
	if s.Dir != "" {
		right := s.Dir + " "
		if pad := w - lipgloss.Width(line) - lipgloss.Width(right); pad > 1 {
			line += strings.Repeat(" ", pad) + right
		}
	}
	line = clip(line, w)
	if pad := w - lipgloss.Width(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return lipgloss.NewStyle().Foreground(T.Lilac).Background(T.Surface).Render(line)
}
```

- [ ] **Step 6: Run tests** — `go test ./internal/ui/ -v` → PASS (all 6 new tests plus earlier ones).

- [ ] **Step 7: Commit**

```bash
git add internal/ui/diffpanel.go internal/ui/input.go internal/ui/statusbar.go internal/ui/diffpanel_test.go internal/ui/statusbar_test.go
git commit -m "feat: diff pane, input box, and status bar widgets"
```

---

### Task 12: Root app model — layout, streaming, cancel, hot-switch, debounced diff

**Files:**
- Create: `internal/ui/app.go`, `internal/ui/app_test.go`

**Interfaces:**
- Consumes: everything from Tasks 2, 3, 8–11.
- Produces: `func NewApp(reg *agent.Registry, cur agent.Agent, dir string) *App` implementing `tea.Model`; `func ComputeLayout(w, h int, wantDiff bool) Layout`.

**Keys (documented in the README in Task 13):** `enter` send · `alt+enter`/`ctrl+j` newline · `esc` cancel turn · `ctrl+c` quit · `tab` switch agent · `ctrl+t` toggle diff pane · `ctrl+r` refresh diff · `ctrl+o` cycle focus · `pgup`/`pgdn`/`home`/`end` scroll focused pane · mouse wheel scrolls the pane under the cursor.

- [ ] **Step 1: Write failing tests** (`app_test.go`)

```go
package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func testApp(t *testing.T) *App {
	t.Helper()
	mk := agent.NewMock()
	mk.StepDelay = time.Millisecond
	reg := &agent.Registry{Agents: []agent.Agent{mk}}
	a := NewApp(reg, mk, t.TempDir())
	a.resize(120, 40)
	return a
}

// pump executes cmd and feeds every resulting message back into the model,
// exactly like the bubbletea runtime, until the queue drains.
func pump(t *testing.T, a *App, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 500 {
			t.Fatal("command loop did not settle")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		if msg == nil {
			continue
		}
		if _, ok := msg.(spinner.TickMsg); ok {
			continue // the spinner re-ticks forever; not under test
		}
		_, next := a.Update(msg)
		queue = append(queue, next)
	}
}

func TestComputeLayoutHidesDiffOnNarrowTerminals(t *testing.T) {
	l := ComputeLayout(80, 24, true)
	if l.ShowDiff {
		t.Fatal("80 cols must not show the diff pane")
	}
	if l.TimelineW != 80 || l.PaneH != 20 {
		t.Fatalf("layout = %+v, want timeline 80 / paneH 20", l)
	}
}

func TestComputeLayoutSplitsWideTerminals(t *testing.T) {
	l := ComputeLayout(120, 40, true)
	if !l.ShowDiff || l.TimelineW+l.DiffW != 120 {
		t.Fatalf("layout = %+v", l)
	}
	if l.DiffW < 34 || l.DiffW > 70 {
		t.Fatalf("diff width out of bounds: %+v", l)
	}
	if off := ComputeLayout(120, 40, false); off.ShowDiff || off.TimelineW != 120 {
		t.Fatalf("toggled-off layout = %+v", off)
	}
}

func TestViewFillsExactlyTheTerminalHeight(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {100, 30}} {
		a := testApp(t)
		a.resize(size[0], size[1])
		got := len(strings.Split(a.View(), "\n"))
		if got != size[1] {
			t.Fatalf("at %dx%d View has %d lines, want %d", size[0], size[1], got, size[1])
		}
	}
}

func TestFullTurnFlowsThroughTheTimeline(t *testing.T) {
	a := testApp(t)
	a.in.ta.SetValue("make a file")
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !a.busy {
		t.Fatal("sending must mark the app busy")
	}
	if a.in.Value() != "" {
		t.Fatal("input must clear on send")
	}
	pump(t, a, cmd)
	if a.busy {
		t.Fatal("TurnEnd must clear busy")
	}
	c := a.tl.Content()
	for _, want := range []string{"make a file", "hello.txt", "Bash"} {
		if !strings.Contains(c, want) {
			t.Fatalf("timeline missing %q:\n%s", want, c)
		}
	}
	if a.sessions["mock"] != "mock-session" {
		t.Fatalf("session id not stored: %+v", a.sessions)
	}
}

func TestSecondTurnResumesTheStoredSession(t *testing.T) {
	a := testApp(t)
	a.sessions["mock"] = "mock-session"
	a.in.ta.SetValue("again")
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if a.lastOpts.SessionID != "mock-session" {
		t.Fatalf("resume not requested: %+v", a.lastOpts)
	}
}

func TestEnterWhileBusyDoesNotStartASecondTurn(t *testing.T) {
	a := testApp(t)
	a.in.ta.SetValue("first")
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	id := a.streamID
	a.in.ta.SetValue("second")
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if a.streamID != id {
		t.Fatal("a second turn must not start while busy")
	}
	if a.note == "" {
		t.Fatal("the user must be told why nothing happened")
	}
}

func TestEscapeCancelsTheTurn(t *testing.T) {
	a := testApp(t)
	a.in.ta.SetValue("long task")
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	pump(t, a, cmd)
	if a.busy {
		t.Fatal("cancel must end the turn")
	}
	if !strings.Contains(a.tl.Content(), "canceling") {
		t.Fatalf("cancel must be visible in the timeline:\n%s", a.tl.Content())
	}
}

func TestStaleStreamEventsAreDropped(t *testing.T) {
	a := testApp(t)
	before := a.tl.Len()
	a.Update(agentEventMsg{id: a.streamID + 99, ev: agent.Event{Kind: agent.KindText, Text: "ghost"}})
	if a.tl.Len() != before {
		t.Fatal("events from a superseded stream must be ignored")
	}
}

func TestTabSwitchesAgentOnlyWhenIdle(t *testing.T) {
	mk := agent.NewMock()
	mk.StepDelay = time.Millisecond
	other := agent.NewMock() // second instance stands in for a second backend
	reg := &agent.Registry{Agents: []agent.Agent{mk, other}}
	a := NewApp(reg, mk, t.TempDir())
	a.resize(120, 40)
	a.busy = true
	a.Update(tea.KeyMsg{Type: tea.KeyTab})
	if a.cur != agent.Agent(mk) {
		t.Fatal("must not switch mid-turn")
	}
	a.busy = false
	a.Update(tea.KeyMsg{Type: tea.KeyTab})
	if a.cur != agent.Agent(other) {
		t.Fatal("tab must switch agents when idle")
	}
}

func TestDiffPaneTogglesWithCtrlT(t *testing.T) {
	a := testApp(t)
	if !a.lay.ShowDiff {
		t.Fatal("diff should start visible at 120 cols")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if a.lay.ShowDiff {
		t.Fatal("ctrl+t must hide the diff pane")
	}
	if got := len(strings.Split(a.View(), "\n")); got != 40 {
		t.Fatalf("View height after toggle = %d, want 40", got)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/ui/ -run 'TestCompute|TestFullTurn|TestEscape|TestTab|TestDiffPane|TestStale|TestSecondTurn|TestEnterWhile|TestViewFills' -v` → FAIL: `NewApp` undefined.

- [ ] **Step 3: Implement `app.go`**

```go
package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/ShukunCheng/Crema/internal/gitdiff"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	statusHeight = 1
	diffMinCols  = 100 // narrower than this, the diff pane hides so the timeline stays readable
	diffDebounce = 350 * time.Millisecond
)

type Layout struct {
	TimelineW, DiffW, PaneH int
	ShowDiff                bool
}

func ComputeLayout(w, h int, wantDiff bool) Layout {
	paneH := h - InputHeight - statusHeight
	if paneH < 3 {
		paneH = 3
	}
	l := Layout{PaneH: paneH, TimelineW: max(1, w)}
	if wantDiff && w >= diffMinCols {
		dw := w * 42 / 100
		if dw > 70 {
			dw = 70
		}
		if dw < 34 {
			dw = 34
		}
		l.ShowDiff, l.DiffW, l.TimelineW = true, dw, w-dw
	}
	return l
}

type (
	agentEventMsg   struct {
		id int
		ev agent.Event
	}
	streamStartedMsg struct {
		id  int
		ch  <-chan agent.Event
		err error
	}
	streamClosedMsg struct{ id int }
	diffMsg         struct {
		seq int
		ds  gitdiff.DiffSet
	}
	diffTickMsg struct{ seq int }
)

type focusTarget int

const (
	focusInput focusTarget = iota
	focusTimeline
	focusDiff
)

type App struct {
	reg *agent.Registry
	cur agent.Agent
	dir string

	tl *Timeline
	dp *DiffPanel
	in *Input
	sp spinner.Model

	w, h     int
	lay      Layout
	wantDiff bool
	focus    focusTarget

	busy      bool
	turnStart time.Time
	stream    <-chan agent.Event
	streamID  int
	cancel    context.CancelFunc
	lastOpts  agent.RunOptions // last Run options, for tests and the status line

	sessions    map[string]string
	sessionCost float64
	diff        gitdiff.DiffSet
	diffSeq     int
	diffApplied int
	note        string
}

func NewApp(reg *agent.Registry, cur agent.Agent, dir string) *App {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = lipgloss.NewStyle().Foreground(T.Pink)
	a := &App{
		reg: reg, cur: cur, dir: dir,
		tl: NewTimeline(80, 20), dp: NewDiffPanel(40, 20), in: NewInput(80),
		sp: sp, wantDiff: true, sessions: map[string]string{},
	}
	a.tl.Append(Block{Kind: BlockSystem, Text: fmt.Sprintf(
		"crema · %s · %s · every command and edit is shown in full",
		cur.Label(), permissionNote(cur))})
	return a
}

func permissionNote(a agent.Agent) string {
	switch a.Name() {
	case "claude":
		return "permission mode acceptEdits — file edits apply without asking"
	case "codex":
		return "sandbox full-auto — file edits apply without asking"
	default:
		return "demo agent, no real work performed"
	}
}

func modeLabel(a agent.Agent) string {
	switch a.Name() {
	case "claude":
		return "acceptEdits"
	case "codex":
		return "full-auto"
	default:
		return "demo"
	}
}

func (a *App) Init() tea.Cmd { return tea.Batch(a.in.Focus(), a.collectDiff()) }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.resize(msg.Width, msg.Height)
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(msg)

	case tea.MouseMsg:
		if a.lay.ShowDiff && msg.X >= a.lay.TimelineW {
			return a, a.dp.Update(msg)
		}
		return a, a.tl.Update(msg)

	case spinner.TickMsg:
		if !a.busy {
			return a, nil
		}
		var cmd tea.Cmd
		a.sp, cmd = a.sp.Update(msg)
		return a, cmd

	case streamStartedMsg:
		if msg.id != a.streamID {
			if msg.ch != nil {
				go drain(msg.ch) // superseded stream: let its goroutine finish
			}
			return a, nil
		}
		if msg.err != nil {
			a.tl.Append(Block{Kind: BlockError, Text: msg.err.Error()})
			a.endTurn(nil)
			return a, nil
		}
		a.stream = msg.ch
		return a, waitForEvent(msg.ch, msg.id)

	case agentEventMsg:
		if msg.id != a.streamID {
			return a, nil
		}
		a.tl.AppendEvent(msg.ev)
		cmds := []tea.Cmd{waitForEvent(a.stream, msg.id)}
		switch msg.ev.Kind {
		case agent.KindToolOutput:
			cmds = append(cmds, a.scheduleDiff())
		case agent.KindTurnEnd:
			a.endTurn(msg.ev.Result)
			cmds = append(cmds, a.collectDiff())
		}
		return a, tea.Batch(cmds...)

	case streamClosedMsg:
		if msg.id != a.streamID {
			return a, nil
		}
		if a.busy { // adapters guarantee a TurnEnd; this is the belt-and-braces path
			a.tl.Append(Block{Kind: BlockError, Text: "the agent stream ended without finishing the turn"})
			a.endTurn(nil)
		}
		return a, nil

	case diffTickMsg:
		if msg.seq != a.diffSeq {
			return a, nil // a newer edit landed; that tick will do the work
		}
		return a, a.collectDiff()

	case diffMsg:
		if msg.seq < a.diffApplied {
			return a, nil // out-of-order result
		}
		a.diffApplied = msg.seq
		a.diff = msg.ds
		a.dp.SetDiff(msg.ds)
		return a, nil
	}
	return a, a.routeToFocus(msg)
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if a.cancel != nil {
			a.cancel()
		}
		return a, tea.Quit
	case "esc":
		if a.busy && a.cancel != nil {
			a.cancel()
			a.tl.Append(Block{Kind: BlockSystem, Text: "canceling the current turn…"})
		}
		return a, nil
	case "tab":
		return a, a.switchAgent()
	case "ctrl+t":
		a.wantDiff = !a.wantDiff
		a.resize(a.w, a.h)
		return a, nil
	case "ctrl+r":
		return a, a.collectDiff()
	case "ctrl+o":
		return a, a.cycleFocus()
	case "enter":
		if a.focus != focusInput {
			return a, nil
		}
		text := strings.TrimSpace(a.in.Value())
		if text == "" {
			return a, nil
		}
		if a.busy {
			a.note = "turn in progress — esc to cancel"
			return a, nil
		}
		a.in.Reset()
		return a, a.startTurn(text)
	}
	return a, a.routeToFocus(msg)
}

func (a *App) startTurn(prompt string) tea.Cmd {
	if err := a.cur.Available(); err != nil {
		a.tl.Append(Block{Kind: BlockError, Text: err.Error()})
		return nil
	}
	a.tl.Append(Block{Kind: BlockUser, Text: prompt})
	a.streamID++
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.busy = true
	a.note = ""
	a.turnStart = time.Now()
	a.lastOpts = agent.RunOptions{Prompt: prompt, Dir: a.dir, SessionID: a.sessions[a.cur.Name()]}
	return tea.Batch(startStream(a.cur, ctx, a.lastOpts, a.streamID), a.sp.Tick)
}

func (a *App) endTurn(r *agent.TurnResult) {
	a.busy = false
	a.stream = nil
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	if r != nil {
		if r.SessionID != "" {
			a.sessions[a.cur.Name()] = r.SessionID
		}
		a.sessionCost += r.CostUSD
	}
}

func (a *App) switchAgent() tea.Cmd {
	if a.busy {
		a.note = "finish or cancel the current turn before switching"
		return nil
	}
	next := a.reg.Next(a.cur.Name())
	if next == nil || next == a.cur {
		a.tl.Append(Block{Kind: BlockSystem, Text: "no other agent is available"})
		return nil
	}
	a.cur = next
	a.note = ""
	a.tl.Append(Block{Kind: BlockSystem, Text: fmt.Sprintf("switched to %s · %s · %s",
		next.Label(), permissionNote(next), resumeNote(a.sessions[next.Name()]))})
	return nil
}

func resumeNote(sid string) string {
	if sid == "" {
		return "new session"
	}
	return "resuming session " + sid
}

func (a *App) cycleFocus() tea.Cmd {
	switch a.focus {
	case focusInput:
		a.focus = focusTimeline
		a.in.Blur()
		return nil
	case focusTimeline:
		if a.lay.ShowDiff {
			a.focus = focusDiff
			return nil
		}
	}
	a.focus = focusInput
	return a.in.Focus()
}

func (a *App) routeToFocus(msg tea.Msg) tea.Cmd {
	switch a.focus {
	case focusTimeline:
		return a.tl.Update(msg)
	case focusDiff:
		return a.dp.Update(msg)
	default:
		return a.in.Update(msg)
	}
}

func (a *App) resize(w, h int) {
	a.w, a.h = w, h
	a.lay = ComputeLayout(w, h, a.wantDiff)
	a.tl.SetSize(a.lay.TimelineW, a.lay.PaneH)
	if a.lay.ShowDiff {
		a.dp.SetSize(a.lay.DiffW-2, a.lay.PaneH-2) // inside the rounded border
	} else if a.focus == focusDiff {
		a.focus = focusInput
		a.in.Focus()
	}
	a.in.SetWidth(w)
}

func (a *App) View() string {
	if a.w == 0 || a.h == 0 {
		return "starting crema…"
	}
	main := a.tl.View()
	if a.lay.ShowDiff {
		c := T.Muted
		if a.focus == focusDiff {
			c = T.Purple
		}
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(c).
			Width(a.lay.DiffW - 2).Height(a.lay.PaneH - 2).
			Render(a.dp.View())
		main = lipgloss.JoinHorizontal(lipgloss.Top, a.tl.View(), box)
	}
	return strings.Join([]string{main, a.in.View(), a.statusLine()}, "\n")
}

func (a *App) statusLine() string {
	s := StatusData{
		Agent: a.cur.Label(), Mode: modeLabel(a.cur), Dir: a.dir,
		Busy: a.busy, Spin: a.sp.View(), Cost: a.sessionCost,
		Adds: a.diff.Additions, Dels: a.diff.Deletions, Note: a.note,
	}
	if a.busy {
		s.ElapsedSec = time.Since(a.turnStart).Seconds()
	}
	return RenderStatus(s, a.w)
}

func startStream(ag agent.Agent, ctx context.Context, opts agent.RunOptions, id int) tea.Cmd {
	return func() tea.Msg {
		ch, err := ag.Run(ctx, opts)
		return streamStartedMsg{id: id, ch: ch, err: err}
	}
}

func waitForEvent(ch <-chan agent.Event, id int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamClosedMsg{id: id}
		}
		return agentEventMsg{id: id, ev: ev}
	}
}

func drain(ch <-chan agent.Event) {
	for range ch {
	}
}

func (a *App) collectDiff() tea.Cmd {
	a.diffSeq++
	seq, dir := a.diffSeq, a.dir
	return func() tea.Msg { return diffMsg{seq: seq, ds: gitdiff.Collect(dir)} }
}

// scheduleDiff debounces refreshes: only the newest schedule survives, so a
// burst of edits costs one `git diff` instead of one per file.
func (a *App) scheduleDiff() tea.Cmd {
	a.diffSeq++
	seq := a.diffSeq
	return tea.Tick(diffDebounce, func(time.Time) tea.Msg { return diffTickMsg{seq: seq} })
}
```

- [ ] **Step 4: Run tests** — `go test ./internal/ui/ -v` → PASS (all tests, ~2s).

- [ ] **Step 5: Run the whole suite + vet**

Run: `go vet ./... && go test ./... -count=1`
Expected: no vet findings; all packages `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "feat: root app model with streaming, cancel, agent switch, debounced diff"
```

---

### Task 13: CLI entry point, doctor, build scripts, README, CI

**Files:**
- Modify: `cmd/crema/main.go` (replace the Task 1 placeholder body)
- Create: `cmd/crema/doctor.go`, `cmd/crema/main_test.go`, `scripts/build.ps1`, `scripts/build.sh`, `.github/workflows/ci.yml`, `README.md` (replace the one-line stub)

**Interfaces:**
- Consumes: `agent.NewRegistry`, `ui.NewApp`, `gitdiff.Collect`.
- Produces: the `crema` binary; `pick(reg, name)` and `doctorReport(reg, dir)` (package `main`, unit-tested).

- [ ] **Step 1: Write failing tests** (`cmd/crema/main_test.go`)

```go
package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ShukunCheng/Crema/internal/agent"
)

type stubAgent struct {
	name string
	err  error
}

func (s stubAgent) Name() string     { return s.name }
func (s stubAgent) Label() string    { return strings.ToUpper(s.name) }
func (s stubAgent) Available() error { return s.err }
func (s stubAgent) Run(context.Context, agent.RunOptions) (<-chan agent.Event, error) {
	return nil, errors.New("stub")
}

func TestPickDefaultsToFirstAvailable(t *testing.T) {
	reg := &agent.Registry{Agents: []agent.Agent{
		stubAgent{name: "claude", err: errors.New("missing")},
		stubAgent{name: "codex"},
	}}
	got, err := pick(reg, "")
	if err != nil || got.Name() != "codex" {
		t.Fatalf("pick = %v, %v", got, err)
	}
}

func TestPickNamedUnavailableExplainsWhy(t *testing.T) {
	reg := &agent.Registry{Agents: []agent.Agent{stubAgent{name: "claude", err: errors.New("claude CLI not found")}}}
	if _, err := pick(reg, "claude"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestPickUnknownNameListsChoices(t *testing.T) {
	reg := &agent.Registry{Agents: []agent.Agent{stubAgent{name: "claude"}}}
	_, err := pick(reg, "gemini")
	if err == nil || !strings.Contains(err.Error(), "claude") {
		t.Fatalf("err = %v", err)
	}
}

func TestDoctorReportFlagsMissingAgents(t *testing.T) {
	reg := &agent.Registry{Agents: []agent.Agent{
		stubAgent{name: "claude", err: errors.New("claude CLI not found")},
		stubAgent{name: "mock"},
	}}
	out, ok := doctorReport(reg, t.TempDir())
	if ok {
		t.Fatal("mock alone must not count as a usable setup")
	}
	for _, want := range []string{"claude CLI not found", "✓ MOCK", "not a git repository"} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	}
}

func TestDoctorReportOKWithRealAgent(t *testing.T) {
	reg := &agent.Registry{Agents: []agent.Agent{stubAgent{name: "codex"}}}
	if _, ok := doctorReport(reg, t.TempDir()); !ok {
		t.Fatal("an available real agent must report ok")
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./cmd/crema/ -v` → FAIL: `pick`/`doctorReport` undefined.

- [ ] **Step 3: Implement `cmd/crema/doctor.go`**

```go
package main

import (
	"fmt"
	"strings"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/ShukunCheng/Crema/internal/gitdiff"
)

// doctorReport describes the environment. ok is true only when a real agent
// (not the demo mock) is installed and logged in.
func doctorReport(reg *agent.Registry, dir string) (string, bool) {
	var b strings.Builder
	fmt.Fprintf(&b, "crema %s\n\nagents:\n", Version)
	ok := false
	for _, ag := range reg.Agents {
		if err := ag.Available(); err != nil {
			fmt.Fprintf(&b, "  ✗ %s — %v\n", ag.Label(), err)
			continue
		}
		fmt.Fprintf(&b, "  ✓ %s\n", ag.Label())
		if ag.Name() != "mock" {
			ok = true
		}
	}
	b.WriteString("\nworkspace:\n")
	ds := gitdiff.Collect(dir)
	if ds.Err != "" {
		fmt.Fprintf(&b, "  ✗ %s — crema still runs, the diff pane just stays empty\n", ds.Err)
	} else {
		fmt.Fprintf(&b, "  ✓ git repo at %s — %d changed files, +%d −%d\n",
			ds.Repo, len(ds.Files), ds.Additions, ds.Deletions)
	}
	if !ok {
		b.WriteString("\nno coding agent found. install one and sign in with your subscription:\n" +
			"  Claude Code  https://claude.com/claude-code   then run: claude\n" +
			"  Codex        npm i -g @openai/codex           then run: codex\n" +
			"crema never asks for an API key — the CLI owns your login.\n")
	}
	return b.String(), ok
}
```

- [ ] **Step 4: Replace `cmd/crema/main.go`**

```go
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ShukunCheng/Crema/internal/agent"
	"github.com/ShukunCheng/Crema/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	agentName := flag.String("agent", "", "agent to start with: claude | codex | mock (default: first available)")
	dir := flag.String("dir", ".", "working directory the agent runs in")
	doctor := flag.Bool("doctor", false, "check the environment and exit")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("crema", Version)
		return
	}

	abs, err := filepath.Abs(*dir)
	if err != nil {
		fail(err)
	}
	reg := agent.NewRegistry()

	if *doctor {
		report, ok := doctorReport(reg, abs)
		fmt.Print(report)
		if !ok {
			os.Exit(1)
		}
		return
	}

	cur, err := pick(reg, *agentName)
	if err != nil {
		fail(err)
	}

	p := tea.NewProgram(ui.NewApp(reg, cur, abs), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fail(err)
	}
}

func pick(reg *agent.Registry, name string) (agent.Agent, error) {
	if name == "" {
		if a := reg.FirstAvailable(); a != nil {
			return a, nil
		}
		return nil, errors.New("no agent available — run `crema --doctor` to see what's missing")
	}
	var known []string
	for _, a := range reg.Agents {
		known = append(known, a.Name())
		if a.Name() == name {
			if err := a.Available(); err != nil {
				return nil, err
			}
			return a, nil
		}
	}
	return nil, fmt.Errorf("unknown agent %q (available: %v)", name, known)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "crema:", err)
	os.Exit(1)
}
```

- [ ] **Step 5: Run tests** — `go test ./cmd/crema/ -v` → PASS (5 tests).

- [ ] **Step 6: Create `scripts/build.ps1`**

```powershell
param([string]$Version = "dev")
$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force dist | Out-Null
$targets = @(
    @{ GOOS = "windows"; GOARCH = "amd64"; Ext = ".exe" },
    @{ GOOS = "linux";   GOARCH = "amd64"; Ext = "" },
    @{ GOOS = "darwin";  GOARCH = "arm64"; Ext = "" }
)
foreach ($t in $targets) {
    $out = "dist/crema-$($t.GOOS)-$($t.GOARCH)$($t.Ext)"
    $env:GOOS = $t.GOOS
    $env:GOARCH = $t.GOARCH
    $env:CGO_ENABLED = "0"
    go build -trimpath -ldflags "-s -w -X main.Version=$Version" -o $out ./cmd/crema
    if ($LASTEXITCODE -ne 0) { throw "build failed for $($t.GOOS)/$($t.GOARCH)" }
    $mb = [math]::Round((Get-Item $out).Length / 1MB, 1)
    "{0,-36} {1} MB" -f $out, $mb
    if ($mb -gt 15) { throw "$out is $mb MB — over the 15 MB budget" }
}
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
"all targets built and under budget"
```

- [ ] **Step 7: Create `scripts/build.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail
VERSION="${1:-dev}"
mkdir -p dist
for target in "windows/amd64/.exe" "linux/amd64/" "darwin/arm64/"; do
  IFS=/ read -r goos goarch ext <<<"$target"
  out="dist/crema-${goos}-${goarch}${ext}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "-s -w -X main.Version=${VERSION}" -o "$out" ./cmd/crema
  bytes=$(wc -c <"$out")
  mb=$(( bytes / 1048576 ))
  printf '%-36s %s MB\n' "$out" "$mb"
  if [ "$mb" -gt 15 ]; then
    echo "$out is ${mb} MB — over the 15 MB budget" >&2
    exit 1
  fi
done
echo "all targets built and under budget"
```

- [ ] **Step 8: Run the build and confirm the size budget**

Run: `pwsh -File scripts/build.ps1 0.1.0`
Expected: three binaries listed, each under 15 MB, final line `all targets built and under budget`. (A pure-Go bubbletea binary lands around 6–9 MB stripped. If any target exceeds 15 MB, the cause is a stray dependency — run `go mod why <module>` before raising the cap.)

- [ ] **Step 9: Create `.github/workflows/ci.yml`**

```yaml
name: ci
on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      - name: gofmt
        if: matrix.os == 'ubuntu-latest'
        run: test -z "$(gofmt -l .)" || (gofmt -l . && exit 1)
      - run: go vet ./...
      - run: go test ./... -count=1
      - name: build and check size budget
        if: matrix.os == 'ubuntu-latest'
        run: bash scripts/build.sh ci
```

CI never runs the `live` build tag, so no subscription turns are ever spent by automation.

- [ ] **Step 10: Write `README.md`** (replaces the stub)

````markdown
# Crema

A Crush-style terminal UI for the coding agents you already pay for.
Crema drives the **official** Claude Code and Codex CLIs in headless mode, so your
subscription login keeps working and **crema never sees an API key**.

Everything the agent does — every command, every edit, every tool result — is
rendered **fully expanded**. Nothing is folded into a card you have to click.
A live `git diff` panel sits on the right and refreshes as the agent works.

## Quickstart (under two minutes)

1. Install and sign in to at least one agent CLI (crema never handles credentials):
   - Claude Code — https://claude.com/claude-code, then run `claude` once to log in
   - Codex — `npm i -g @openai/codex`, then run `codex` once to log in
2. Install crema:
   ```
   go install github.com/ShukunCheng/Crema/cmd/crema@latest
   ```
   or grab a binary from the releases page.
3. Check your setup and start:
   ```
   crema --doctor
   cd your-project
   crema
   ```

No agent installed yet? `crema --agent mock` runs a scripted demo so you can see
the interface without spending anything.

## Keys

| Key | Action |
|---|---|
| `enter` | send the message |
| `alt+enter` / `ctrl+j` | newline |
| `esc` | cancel the running turn |
| `tab` | switch agent (between turns) |
| `ctrl+t` | show/hide the diff pane |
| `ctrl+r` | refresh the diff now |
| `ctrl+o` | move focus: input → timeline → diff |
| `pgup` / `pgdn` / `home` / `end` | scroll the focused pane |
| `ctrl+c` | quit |

The diff pane hides automatically below 100 columns; crema stays usable at 80×24.

## Flags

```
crema [--agent claude|codex|mock] [--dir PATH] [--doctor] [--version]
```

## How it works

| | |
|---|---|
| Claude Code | `claude -p <prompt> --output-format stream-json --verbose --permission-mode acceptEdits`, resumed with `--resume <session_id>` |
| Codex | `codex exec --json --full-auto <prompt>`, resumed with `codex exec resume <thread_id>` |
| Diff | `git diff --cached`, `git diff`, and `git ls-files --others --exclude-standard`, parsed in-process |

Crema spawns these CLIs as subprocesses and normalizes their JSON event streams
into one internal event type. It never reads their config files, never stores
tokens, and never talks to any model API directly.

## Permission mode — read this

Headless CLIs cannot show an interactive permission prompt, so crema starts them
in an auto-approving mode (`acceptEdits` for Claude Code, `--full-auto` for Codex).
**The agent will edit files without asking.** The status bar always shows the
active mode, and the diff pane shows exactly what changed. Run crema in a git
repo with a clean tree so you can always `git checkout` your way back.
Bringing permission prompts into the TUI (via the Claude Agent SDK `canUseTool`
callback) is the headline item for the next milestone.

## Truncation policy

A single tool output is capped at 4000 lines. When that happens, crema prints an
explicit, counted label — `… +N lines truncated (crema cap 4000)`. Long diff lines
are clipped to the pane width and marked with `›`. Crema never hides output
without telling you.

## Troubleshooting

- **`crema --doctor` says an agent is missing** — install its CLI and run it once
  interactively to complete login. Crema uses whatever session that CLI stores.
- **Codex fails with "The 'gpt-5.x-codex' model is not supported when using Codex
  with a ChatGPT account"** — that's Codex's own error, not crema's. Pick a model
  your plan allows (`codex --model …` or `~/.codex/config.toml`) and retry.
- **The diff pane says "not a git repository"** — crema works fine, but the pane
  needs a repo. Start crema with `--dir` pointing at one.
- **Nothing appears for a while after sending** — the CLIs buffer their first
  event until the model responds. The status bar timer tells you it's alive; `esc`
  cancels.

## Status

M1 (MVP): Claude Code + Codex, agent hot-switch, live diff, turn cancel.
Next: themes, split diff view, session history, slash commands, in-TUI permission
prompts, then Gemini CLI and parallel agents on git worktrees.
````

- [ ] **Step 11: Manual smoke test — mock agent**

```bash
go build -o crema.exe ./cmd/crema
mkdir smoke && cd smoke && git init -q && cd ..
./crema.exe --agent mock --dir smoke
```

Verify by eye, then `ctrl+c`:
1. The intro system line names the agent and the permission mode.
2. Typing a message and pressing enter shows `❯ you`, then thinking, tool blocks with the left rail, and a `✔ 0.0s` stats line.
3. `ctrl+t` hides and shows the diff pane; the layout stays exactly the terminal height.
4. `tab` reports "no other agent is available" (mock is alone unless claude/codex are installed).
5. Resize the terminal to 80×24 — the diff pane disappears, nothing wraps badly.

- [ ] **Step 12: Manual smoke test — real agent, real diff**

```bash
./crema.exe --dir smoke
```

Send: `create a file called hello.txt containing the word crema, then show me its contents`
Verify: the Bash/Write tool call and its output both render **in full**; within ~a second of the file being written, the diff pane shows `UNTRACKED` → `hello.txt` with a green `+crema`; the status bar shows elapsed time, then cost after the turn; sending a second message continues the same session (the agent remembers the file).

Then test cancel: send `count slowly from 1 to 100, one line at a time`, press `esc` mid-turn. Verify the timeline shows `canceling the current turn…` followed by a `⨯ canceled` stats line, the app returns to idle within a couple of seconds, and no orphan `claude`/`node` process remains (`Get-Process node,claude -ErrorAction SilentlyContinue`).

- [ ] **Step 13: Full verification**

Run: `go vet ./... && go test ./... -count=1 && pwsh -File scripts/build.ps1 0.1.0`
Expected: vet clean, every package `ok`, three binaries under 15 MB.

- [ ] **Step 14: Commit**

```bash
git add cmd/crema/ scripts/ .github/ README.md
git commit -m "feat: cli entry point, doctor, cross-platform build scripts, README, CI"
```

---

## Spec coverage map

| Spec requirement | Where it lands |
|---|---|
| 透明优先 — every command/edit/tool call fully visible, truncation explicitly labeled | Task 9 renderers + `MaxOutputLines` label; Task 11 `clip` marks clipped diff lines with `›` |
| 订阅优先 — official CLI headless interfaces only, never touch keys | Tasks 6 & 7 adapters; Global Constraints; README "How it works" |
| 终端优先 — single binary, fast start, SSH-friendly | Task 13 build scripts with the 15 MB gate; no network or config I/O at startup |
| `Agent` interface: Name / Available / Run → event stream | Task 2 |
| Normalized events: text, tool call, tool output, turn end | Task 2 `Kind` (plus `KindError` for mid-stream failures) |
| Claude `-p --output-format stream-json --verbose`, `--resume` | Task 6 |
| Codex `exec --json`, resume, both item.* and legacy msg schemas | Task 7 |
| Left timeline, fully expanded, tool blocks with a left rail | Tasks 9 (`rail`) & 10 |
| Right diff pane: staged + unstaged + untracked, auto-refresh | Tasks 8, 11, and 12 (`scheduleDiff` debounce) |
| Bottom input bar + status bar (agent, elapsed, cost) | Task 11 `Input`, `RenderStatus` |
| Pink/purple dark theme, rounded borders | Task 9 `theme.go`; rounded borders on the input and diff panes |
| M1: Codex, agent hot-switch, diff refresh, cancel, error handling, README | Tasks 7, 12 (`tab`, `esc`), 12, 5+12, 13 |
| Risk: unstable upstream schemas | Tolerant parsers with `Skipped` counters, golden fixtures, live schema test (Task 6 Step 5) |
| Risk: no interactive permission prompt in headless mode | `acceptEdits` / `--full-auto` + intro block, status-bar mode, README warning; `canUseTool` deferred to M2 |
| 成功标准: <2 min to first chat / no clicks / 80×24 / <15 MB | README quickstart; transparency renderers; `ComputeLayout` tests; build-script size gate |

**Documented deviation from the spec:** the spec suggests `codex exec resume --last`. This plan resumes by explicit thread id (`codex exec resume <thread_id>`) because `--last` picks the newest session recorded on the machine — if the user runs codex in another terminal, crema would silently hijack that conversation.

## Definition of Done (M1)

- [ ] `crema --doctor` reports agent and repo status and exits non-zero when nothing is installed.
- [ ] Claude Code and Codex both drive a full turn, with multi-turn resume per agent.
- [ ] Every tool call and tool output renders fully expanded; the only truncation is the labeled 4000-line cap.
- [ ] The diff pane shows staged, unstaged, and untracked changes and refreshes within ~0.4 s of the agent touching a file.
- [ ] `esc` cancels a turn and leaves no orphan process; `tab` switches agents between turns.
- [ ] Usable at 80×24; `View()` output is exactly the terminal height at every size tested.
- [ ] `go vet ./...` and `go test ./...` pass on Windows and Linux; binaries are under 15 MB.
- [ ] README quickstart takes a new user from zero to first message in under two minutes.

## Deferred (do not build in M1)

M2 — theme system, split diff view, session persistence, `/clear` `/model` `/cost`, custom keybindings, `canUseTool` permission prompts via the Claude Agent SDK.
M3 — Gemini CLI adapter, parallel agents on git worktrees, per-hunk stage/revert in the diff pane, brew/scoop packaging.
