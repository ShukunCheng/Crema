<p align="center">
  <img src="Crema.png" alt="Crema" width="128">
</p>

<h1 align="center">Crema</h1>

<p align="center">
  <b>A terminal home for the coding agents you already pay for.</b><br>
  Run Claude Code and Codex side by side, watch every change land, and never hand over an API key.
</p>

<p align="center">
  <a href="https://github.com/ShukunCheng/Crema/actions/workflows/ci.yml"><img src="https://github.com/ShukunCheng/Crema/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <img src="https://img.shields.io/badge/go-1.24-00ADD8?logo=go&logoColor=white" alt="Go 1.24">
  <img src="https://img.shields.io/badge/platform-windows%20%7C%20linux%20%7C%20macOS-8A2BE2" alt="Windows, Linux, macOS">
</p>

<p align="center">
  <a href="#quickstart">Quickstart</a> ·
  <a href="#features">Features</a> ·
  <a href="docs/guide.md">Guide</a> ·
  <a href="docs/development.md">Development</a>
</p>

---

```
╭ AGENTS ──────────────╮ Refactor the order dialog to use the new grid   ╭ changes  +42 −17 ── side  full  off ╮
│▸ 1 api      ⣾ 1m 35s │ ▸ Read 3 files · Searched 2 times                │ ── UNSTAGED                         │
│  2 web          idle │ ▾ Edit  src/OrderDialog.tsx                      │ ▾ ● src/OrderDialog.tsx  +38 −15    │
│  3 docs     ⣷   12s  │ │ -  <div className="flex">                      │ @@ -40,7 +40,9 @@                   │
│                      │ │ +  <div className="grid grid-cols-3 gap-2">    │ -  <div className="flex">           │
│+ new agent  ^n       │ Moved the columns onto the grid; tests pass.     │ +  <div className="grid …">         │
╰──────────────────────╯                                                  ╰─────────────────────────────────────╯
╭──────────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│❯ ask the agent…                                                                                                  │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
 ● Claude Code [1/3] │ [opus] │ D:\api git:(main* +42 −17) │ $0.84
 Context ████░░░░ 41%  │ 5h ██░░░░░░ 21% (resets in 1h 10m) │ 7d ░░░░░░░░ 3%
 ▸▸ full access (ctrl+p to change) · 1 agent (↓ to see)
```

## Why Crema

Coding agents are at their best when you can keep several going and see
exactly what each one did. Crema gives the official CLIs a proper workspace:

- **Your subscription, untouched.** Crema drives the real `claude` and `codex`
  CLIs in headless mode. Your login stays theirs. Crema never sees a key, a
  token, or a password.
- **Many agents, at once.** Each agent has its own project, conversation and
  model, and they all run in parallel. The sidebar tells you which ones are done.
- **Judged by what changed.** File edits arrive open as diffs. The reading,
  searching and thinking fold into one line you can click, and a live
  `git diff` sits beside the conversation.

## Features

| | |
|---|---|
| 🧵 **Parallel agents** | Open as many as you like with `ctrl+n`, switch with `ctrl+↑`/`ctrl+↓`. Quit and they all come back next time, conversations included. |
| 🔍 **Live diff** | Staged, unstaged and untracked changes, refreshed as the agent works. Beside the conversation or full screen and side by side, with search and sideways scrolling. |
| 🤖 **Subagents you can see** | Every subagent and background shell gets a live row: what it is doing, for how long, what it has written. Open one to read its own conversation, or stop it. |
| 🎛️ **Settings a keypress away** | Model, permission mode and effort are one `↓` from the message box. |
| 📊 **Usage at a glance** | Context window, 5-hour and 7-day allowance with their reset times, and spend per agent. |
| 💬 **The CLI's own commands** | `/clear`, `/compact`, `/resume`, `/model`, every skill and plugin command, plus `@` to mention files and `ctrl+v` to paste screenshots. |
| 🧭 **It tells you why** | When a turn or a subagent stops, Crema says what stopped it. A local diagnostic log (`/log`) costs no tokens. |
| 🖱️ **Mouse everywhere** | Click to switch, fold and open links. Drag to select and copy, double-click a word. Light and dark themes. |

## Quickstart

**1. Sign in to an agent CLI** (Crema uses its login, never its own):

- [Claude Code](https://claude.com/claude-code): install it, then run `claude` once to sign in
- [Codex](https://github.com/openai/codex): `npm i -g @openai/codex`, then run `codex` once to sign in

**2. Install Crema** (needs Go 1.24+):

```sh
go install github.com/ShukunCheng/Crema/cmd/crema@latest
```

On Windows, `pwsh -File scripts/install.ps1 -Build` from a clone also adds
Start menu and desktop shortcuts. See [Development](docs/development.md).

**3. Run it** in a project:

```sh
crema --doctor   # checks that everything is in place
cd your-project
crema
```

No agent CLI yet? `crema --agent mock` runs a scripted demo that costs nothing.

## First steps

| Key | |
|---|---|
| `enter` | send. While the agent works, the message waits its turn |
| `esc` | cancel the turn |
| `↓` | model, permissions, effort, then the running subagents |
| `ctrl+n` · `ctrl+w` | new agent · close this one |
| `ctrl+↑` · `ctrl+↓` | previous · next agent |
| `ctrl+t` | the diff: hidden, beside, full screen |
| `/` · `@` | commands and skills · files of the project |
| `ctrl+q` | quit |

Everything else is in the **[guide](docs/guide.md)**: every key and click,
every command, and how Crema drives the CLIs and what it costs.

## How it works

Crema starts the official CLI for each agent as a subprocess:
`claude -p --input-format stream-json --output-format stream-json`, or
`codex exec --json`. It reads their event streams and draws them. It never
calls a model API itself and never stores a credential. `/login` hands the
terminal to the CLI's own sign-in. The diff comes from plain `git`.

## Contributing

Issues and pull requests are welcome. `go test ./...` runs the whole suite
without spending any subscription quota. Building, installing and the Windows
details are in [docs/development.md](docs/development.md).
