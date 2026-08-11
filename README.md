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

## Themes

Crema ships a pink/purple palette in both light and dark. It picks one by asking
your terminal about its background at startup; override with `--theme light` or
`--theme dark`, and flip at any time with `ctrl+l`.

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
