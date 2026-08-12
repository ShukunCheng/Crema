# Crema

A Crush-style terminal UI for the coding agents you already pay for.
Crema drives the **official** Claude Code and Codex CLIs in headless mode, so your
subscription login keeps working and **crema never sees an API key**.

Everything the agent does — every command, every edit, every tool result — is
rendered **fully expanded**. Nothing is folded into a card you have to click.
A live `git diff` panel sits on the right and refreshes as the agent works.

Open as many agents as you like. Each one has its own backend, its own working
directory, and its own conversation, and they **run at the same time** — fire off
a refactor in one project, switch to another agent while it works, and come back
when the sidebar says it's done.

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
| `enter` | send the message to the focused agent |
| `alt+enter` / `ctrl+j` | newline |
| `esc` | cancel the focused agent's turn (others keep running) |
| `ctrl+n` | new agent — pick a backend, then browse to a working directory |
| `ctrl+w` | close the focused agent (closing the last one quits) |
| `tab` / `shift+tab` | next / previous agent |
| `alt+1` … `alt+9` | jump straight to that agent |
| `ctrl+b` | show/hide the agent sidebar |
| `ctrl+p` | permissions and model for the focused agent |
| `ctrl+e` | release the mouse so you can select text (and recapture it) |
| `ctrl+t` | show/hide the diff pane |
| `ctrl+l` | switch between light and dark |
| `ctrl+r` | refresh the diff now |
| `ctrl+o` | move focus: input → timeline → diff |
| `pgup` / `pgdn` / `home` / `end` | scroll the focused pane |
| `ctrl+c` | quit |

Panes drop away as the terminal narrows so crema stays usable at 80×24: the diff
pane needs 124 columns, the sidebar needs 70. Above those floors `ctrl+t` and
`ctrl+b` decide. When the sidebar is hidden, the status bar still shows which
agent you're on and how many are running, e.g. `Claude Code [2/3] · 1 running`.

## Mouse

The whole interface is clickable:

| Click | Action |
|---|---|
| an agent in the sidebar | switch to it |
| `+ new agent` | open the picker |
| the `[ dark ]` / `[ light ]` chip in the status bar | switch theme |
| a row in the picker | choose that backend or folder |
| any pane | focus it (so `pgup`/`pgdn` go there) |
| a tool block's header line | fold or unfold that block |
| a file's header line in the diff | fold or unfold that file |
| scroll wheel | scrolls whichever pane is under the pointer, focused or not |

Folding is always yours to ask for and never happens on its own. A folded block
says exactly how much is behind it — `▸ ⏵ Bash — 12 lines hidden, click to
expand` — and folded diff files keep their state across refreshes, so an agent
writing files won't reopen everything you tidied away. Expanded files show `▾`.

Because crema captures the mouse, your terminal's own click-to-select is
suppressed. Hold **shift** while dragging usually works; where it doesn't, press
`ctrl+e` (or click the `[ click ]` chip) to release the mouse entirely — the chip
flips to `[ select ]`, clicking stops, and normal terminal selection works until
you press it again.

## What the status bar tells you

```
 ● Claude Code · acceptEdits · opus · $0.6396 · +12 −3 · ctx 14% · 5h 42% · resets in 2h14m   D:\Crema [ click ][ dark  ]
```

Left to right: the focused agent (and `[2/3]` when several are open), its
permission mode and model, spend so far, the diff totals, how full the model's
context window is, and the backend's usage window with a countdown to reset.

The context and usage figures come straight out of Claude Code's own event
stream — crema reads the `contextWindow` it reports and the rate-limit events it
emits, rather than estimating. Codex reports neither, so those two segments are
simply absent for Codex agents rather than guessed at.

## Multiple agents

`ctrl+n` opens a two-step picker: choose a backend, then browse to the folder
that agent should work in. The browser is plain terminal UI rather than a native
dialog, so it still works over SSH — `↑↓` to move, `enter` to open a folder,
`←` to go up, and the `[ use this directory ]` row to accept the current one.

Because each agent owns a directory, giving two agents separate projects (or
separate clones) keeps their edits from colliding. Pointing two agents at the
same directory is allowed, but they will overwrite each other's work without
warning — crema does not arbitrate.

The sidebar lists every open agent with its live state, so you can see at a
glance which ones are still working:

```
╭──────────────────────╮
│AGENTS                │
│▸ 1 claude · api  ⣾ 7s│
│  2 codex · web   idle│
│  3 claude · docs ⣷12s│
│                      │
│+ new agent  ^n       │
╰──────────────────────╯
```

## Memory between runs

Crema remembers your open agents. Quit with three agents going and the next
launch brings all three back — same backends, same directories, same
conversations, and each one continues its *agent* session too, so Claude Code or
Codex still has the context you built up rather than starting cold.

Your theme choice is remembered as well.

State lives in `state.json` under your user config directory
(`%APPDATA%\crema\` on Windows, `~/.config/crema/` elsewhere), written with
owner-only permissions because saved conversations and tool output can contain
anything from your repositories. It is written atomically after every finished
turn, so a crash costs you at most the turn in flight.

To skip restoring and start with a single fresh agent:

```
crema --fresh
```

Naming `--dir` or `--agent` explicitly still restores everything and then
focuses (or opens) the one you named.

Two bounds keep the file small, and both announce themselves in the restored
conversation rather than quietly losing history: the last 300 entries per agent
are kept, and any single entry over 20,000 characters is cut with a counted
marker. Agents whose directory has since been deleted are skipped with a visible
reason instead of silently vanishing.

## Themes

Crema ships a pink/purple palette in both light and dark. It picks one by asking
your terminal about its background at startup; override with `--theme light` or
`--theme dark`.

Both themes paint their own background across the whole screen rather than
letting your terminal's show through, so dark mode is genuinely dark even in a
light terminal — and stays readable either way.

Crema also asks the terminal to adopt the theme background as its default
(OSC 11), so the emulator's own window padding matches instead of framing a dark
theme in white, and hands the color back on exit. Terminals that don't implement
that sequence ignore it; if yours does and crema is killed rather than quit, a
new tab clears it.

To switch while running, click the chip at the right end of the status bar or
press `ctrl+l`. The chip always shows the current mode and is the last thing the
status bar gives up as the terminal narrows, so it stays reachable:

```
 ● Claude Code · acceptEdits · $0.14 · +12 −3        D:\my-project [ dark  ]
                                                                  ^ click me
```

## Flags

```
crema [--agent claude|codex|mock] [--dir PATH] [--theme auto|light|dark]
      [--doctor] [--version]
```

`--agent` and `--dir` set up the first agent; everything after that is `ctrl+n`.

## How it works

| | |
|---|---|
| Claude Code | `claude -p <prompt> --output-format stream-json --verbose --permission-mode acceptEdits`, resumed with `--resume <session_id>` |
| Codex | `codex exec --json --full-auto <prompt>`, resumed with `codex exec resume <thread_id>` |
| Diff | `git diff --cached`, `git diff`, and `git ls-files --others --exclude-standard`, parsed in-process |

Crema spawns these CLIs as subprocesses and normalizes their JSON event streams
into one internal event type. It never reads their config files, never stores
tokens, and never talks to any model API directly.

## Permissions and model — `ctrl+p`

Headless CLIs cannot show an approval prompt, so a tool that *would* ask instead
**fails**. That is why the permission mode matters more here than in the
interactive CLIs, and why you'll see errors like *"This command requires
approval"* if the mode is too tight for what you asked.

`ctrl+p` opens per-agent settings. Each agent has its own mode and model:

| Mode | What the agent may do |
|---|---|
| `ask` | the CLI's own default — most tools that need approval will fail |
| `plan` | read-only; it plans but changes nothing (Claude Code only) |
| `edits` | **the default.** File edits apply; shell commands are still blocked |
| `full access` | no prompts at all; the agent can run any command |

If your agent keeps reporting that commands need approval, `full access` is the
mode that actually lets a headless coding agent work — at the cost of letting it
run anything. The status bar always shows the active mode, the diff pane shows
exactly what changed, and every mode change is written into the conversation so
the transcript explains why a later turn could or couldn't run something. Run
crema in a git repo with a clean tree so you can always `git checkout` back.

The same panel picks the model — Claude Code's `opus` / `sonnet` / `haiku` /
`fable` aliases, or the CLI's own default. Crema only offers modes and models
the focused backend can actually honor: Codex has no read-only planning mode, so
it isn't listed there, and Codex model choice is left to its own config because
availability depends on your plan.

Both settle at startup too, and are remembered per agent between runs:

```
crema --permission-mode full --model opus
```

Bringing real permission prompts into the TUI (via the Claude Agent SDK
`canUseTool` callback) is still the headline item for a later milestone.

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

## Development

```
go test ./...                 # unit tests, no subscription tokens spent
go vet ./...
pwsh -File scripts/build.ps1  # cross-build all targets, enforce the 15 MB budget
bash scripts/build.sh         # same, from a POSIX shell
```

`go test -tags live ./internal/agent/` runs a real one-turn smoke test against
your installed Claude Code. It costs a small amount of your subscription quota
and is never run by CI.

The implementation plan lives in [docs/plan-m1.md](docs/plan-m1.md).

## Status

M1 (MVP): Claude Code + Codex, agent hot-switch, live diff, turn cancel.
Next: themes, split diff view, session history, slash commands, in-TUI permission
prompts, then Gemini CLI and parallel agents on git worktrees.
