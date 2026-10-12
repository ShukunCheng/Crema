# Developing crema

```
go test ./...                 # unit tests, no subscription tokens spent
go vet ./...
pwsh -File scripts/build.ps1  # cross-build all targets, enforce the 15 MB budget
bash scripts/build.sh         # same, from a POSIX shell
```

`go test -tags live ./internal/agent/` runs a real one-turn smoke test against
your installed Claude Code. It costs a small amount of your subscription quota
and is never run by CI.

## Installing it

```
pwsh -File scripts/install.ps1 -Build      # build, install, shortcuts, PATH
pwsh -File scripts/install.ps1 -Uninstall  # take it all back off
```

The install puts a copy of the binary in `%LOCALAPPDATA%\Programs\Crema`,
points the shortcuts at that copy, and adds the folder to your PATH so `crema`
works from any terminal too. The copy is the point: the application stops
depending on the working tree, so a `git clean`, a moved checkout or a
half-finished `go build` can't break it. The cost is that it no longer follows
your dev builds — run the script again to update it. It never touches your
agents and settings in `%APPDATA%\crema`, including on uninstall.

### Rebuilding without a terminal

```
pwsh -File scripts/install.ps1 -RebuildShortcut
```

adds a second Desktop shortcut, **Rebuild Crema**, that does that update in one
click: it opens a window, builds this working tree, installs the result over the
copy the Crema shortcut launches, prints the before and after hash so you can
see whether anything actually changed, and closes itself. Press a key while it
is counting down and it stays open instead.

It is the only shortcut that knows where the source is, so it stops working if
you move the checkout — regenerate it from the new location. If crema is running
when you click it, it says so and waits: Windows locks a running binary, and
quitting crema properly with `ctrl+q` is what saves your open agents, so it will
not close it for you. `-Uninstall` removes this one along with the rest.

## A taskbar button of its own

**Start crema from its shortcut, not by typing `crema` in a terminal.**

```
pwsh -File scripts/shortcut.ps1                    # Desktop and Start menu
pwsh -File scripts/shortcut.ps1 -Dir D:\my-project # opened on a folder
```

That is the whole trick, and it is not a preference: a program typed into
Windows Terminal is drawing inside *the terminal's* window, so the taskbar
button is the terminal's, shared with every other tab on it. No guest program
can repaint or split its host's button. The shortcut instead runs crema through
`conhost.exe`, the classic console host, which gives it a window nobody else is
sharing.

Given a window of its own, crema claims it:

| | |
|---|---|
| the title | `Crema — <folder>`, which is what the button and alt-tab read |
| the icon | set from crema's own embedded resources, so the button shows the artwork rather than a console glyph |
| the identity | the window declares `AppUserModelID = Gomocha.Crema`, which is what the taskbar groups by — so it can never fold into Windows Terminal's button, or another console app's |

The shortcut carries that same identity, so a pinned Crema and a running Crema
are one button rather than two. Windows does not allow a program to pin itself;
right-click the running button and choose *Pin to taskbar*.

Setting **Settings → System → For developers → Terminal** to Windows Console
Host does the same thing without a shortcut, but it changes the host for every
console program on the machine.

## The icon

`Crema.png` is the source of the product icon. The Windows binary carries it as
a resource, which is what Explorer, the taskbar and a shortcut show. Both
generated files are committed, so an ordinary `go build` produces an
icon-bearing exe with nothing installed; regenerate them after changing the
artwork:

```
go run ./scripts/icon                     # Crema.png -> assets/crema.ico
go run github.com/akavel/rsrc@v0.10.2 -ico assets/crema.ico \
    -arch amd64 -o cmd/crema/rsrc_windows_amd64.syso
```

The Go linker picks up any `.syso` in the main package's directory; the
`_windows_amd64` suffix keeps it out of the Linux and macOS builds. The
generator is stdlib-only — it downsizes the artwork to the seven sizes Windows
picks between, storing the small ones as bitmaps and the two large ones as PNG.

The implementation plan lives in [plan-m1.md](plan-m1.md).
