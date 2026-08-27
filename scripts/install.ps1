# Installs crema as an application: a copy of the binary somewhere stable, the
# shortcuts pointing at that copy, and the folder on PATH.
#
#   pwsh -File scripts/install.ps1            # install what is already built
#   pwsh -File scripts/install.ps1 -Build     # build it first, then install
#   pwsh -File scripts/install.ps1 -RebuildShortcut  # ...and a button to redo it
#   pwsh -File scripts/install.ps1 -Uninstall # take it all back off again
#
# The point of the copy is that the application stops depending on the working
# tree: a `git clean`, a moved checkout or a half-finished `go build` can't
# break the shortcut any more. The cost is that it no longer follows your dev
# builds — run this again to update it.
param(
    [string]$Dest = (Join-Path $env:LOCALAPPDATA "Programs\Crema"),
    [string]$Version = "",
    [switch]$Build,
    [switch]$Uninstall,
    [switch]$NoPath,
    [switch]$NoShortcuts,
    # A second shortcut that reruns this script. Only useful from a working
    # tree, so it is opt-in and lives on the Desktop rather than the Start menu.
    [switch]$RebuildShortcut
)
$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $Dest "crema.exe"
$desktop = [Environment]::GetFolderPath("Desktop")
$startMenu = Join-Path ([Environment]::GetFolderPath("ApplicationData")) "Microsoft\Windows\Start Menu\Programs"
$rebuildLink = Join-Path $desktop "Rebuild Crema.lnk"
$links = @(
    Join-Path $desktop "Crema.lnk"
    Join-Path $startMenu "Crema.lnk"
    $rebuildLink
)

# userPath edits the per-user PATH, which is the half of PATH that doesn't need
# an administrator. Already-open terminals keep the old one until restarted.
function Set-UserPath([string[]]$entries) {
    [Environment]::SetEnvironmentVariable("PATH", ($entries -join ";"), "User")
}
function Get-UserPath {
    @(([Environment]::GetEnvironmentVariable("PATH", "User") -split ";") | Where-Object { $_ })
}

if ($Uninstall) {
    foreach ($l in $links) {
        if (Test-Path $l) { Remove-Item $l -Force; "removed $l" }
    }
    $keep = Get-UserPath | Where-Object { $_.TrimEnd('\') -ne $Dest.TrimEnd('\') }
    if ($keep.Count -ne (Get-UserPath).Count) { Set-UserPath $keep; "removed $Dest from PATH" }
    if (Test-Path $Dest) { Remove-Item $Dest -Recurse -Force; "removed $Dest" }
    "crema uninstalled — your agents and settings in %APPDATA%\crema are untouched"
    return
}

if ($Build) {
    # Stamp the build with the minute it was made. Every build calls itself
    # 0.1.0-dev otherwise, and "is the fix in the one I'm running?" is not a
    # question `crema --version` should leave you guessing at.
    if (-not $Version) { $Version = "0.1.0-dev+" + (Get-Date -Format "yyyyMMdd-HHmm") }
    $ldflags = "-s -w -X main.Version=$Version"
    Push-Location $repo
    try {
        $env:CGO_ENABLED = "0"
        go build -trimpath -ldflags $ldflags -o (Join-Path $repo "crema.exe") ./cmd/crema
        if ($LASTEXITCODE -ne 0) { throw "build failed" }
    } finally { Pop-Location; Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue }
}

$source = Join-Path $repo "crema.exe"
if (-not (Test-Path $source)) { throw "$source not found — run with -Build, or build it yourself first" }

New-Item -ItemType Directory -Force $Dest | Out-Null
try {
    Copy-Item $source $exe -Force
} catch {
    throw "could not replace $exe — close any running Crema and try again ($($_.Exception.Message))"
}
"installed $(& $exe --version) to $exe"

if (-not $NoShortcuts) {
    # Start in the home folder rather than the install folder: crema restores
    # the agents you had open, and the one it makes when there are none should
    # land somewhere you keep work, not in its own program directory.
    & (Join-Path $PSScriptRoot "shortcut.ps1") -Exe $exe -WorkingDir $HOME
}

if ($RebuildShortcut) {
    # Points at the working tree it was created from, not at $Dest — the whole
    # job of this one is to pick up new source, so it has to know where source
    # is. Move or delete the checkout and this shortcut stops working; the
    # Crema shortcut, which is the one that matters, does not care.
    . (Join-Path $PSScriptRoot "consoleprops.ps1")
    $s = (New-Object -ComObject WScript.Shell).CreateShortcut($rebuildLink)
    $s.TargetPath = Join-Path $env:SystemRoot "System32\conhost.exe"
    $s.Arguments = "pwsh.exe -NoProfile -ExecutionPolicy Bypass -File " +
                   "`"$(Join-Path $PSScriptRoot 'rebuild.ps1')`""
    $s.WorkingDirectory = $repo
    $s.IconLocation = "$exe,0"
    $s.Description = "Build Crema from $repo and install it over the running copy"
    $s.Save()
    # Narrower than crema's own window, and QuickEdit left on: this one is a
    # wall of build output, so dragging to copy an error is the useful gesture.
    Set-ConsoleProps -Link $rebuildLink -Face "Cascadia Mono" `
        -Height (Get-ConsoleFontPixels 12) -Cols 100 -Lines 30 -QuickEdit
    "created $rebuildLink"
}

if (-not $NoPath) {
    $path = Get-UserPath
    if ($path -notcontains $Dest -and ($path | ForEach-Object { $_.TrimEnd('\') }) -notcontains $Dest.TrimEnd('\')) {
        Set-UserPath ($path + $Dest)
        "added $Dest to your PATH — open a new terminal and `crema` will be there"
    } else {
        "$Dest is already on your PATH"
    }
}
