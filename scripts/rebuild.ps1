# The click target behind the "Rebuild Crema" shortcut: build the working tree
# and install it over the copy the Crema shortcut launches.
#
# Everything here is what install.ps1 already does. What this adds is being
# usable as a button — it opens its own window, says what changed, refuses to
# fail silently, and stays on screen long enough to read when something breaks.
#
#   pwsh -File scripts/rebuild.ps1            # build, install, report
#   pwsh -File scripts/rebuild.ps1 -KeepOpen  # never auto-close
param(
    [switch]$KeepOpen
)
$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
$installed = Join-Path $env:LOCALAPPDATA "Programs\Crema\crema.exe"

function Say([string]$text, [string]$color = "Gray") { Write-Host $text -ForegroundColor $color }

# Keys are only readable from a real console. Run this from a pipeline or a
# scheduled task and every RawUI call throws, so check once and fall back to
# not waiting at all — a script that hangs unattended is worse than one that
# scrolls past.
$script:interactive = $true
try { [void]$Host.UI.RawUI.KeyAvailable } catch { $script:interactive = $false }

function Read-AnyKey {
    if (-not $script:interactive) { return [char]0 }
    ($Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")).Character
}

# Wait for a key, or for a countdown to run out. A key during the countdown
# cancels it, so a glance that turns into actually reading the output doesn't
# lose the window.
function Wait-Then-Close([int]$seconds) {
    if (-not $script:interactive) { return }
    if ($KeepOpen -or $seconds -le 0) {
        Say ""; Say "press any key to close" "DarkGray"
        [void](Read-AnyKey); return
    }
    for ($i = $seconds; $i -gt 0; $i--) {
        Write-Host ("`r  closing in $i s — press any key to stay ") -NoNewline -ForegroundColor DarkGray
        for ($t = 0; $t -lt 10; $t++) {
            if ($Host.UI.RawUI.KeyAvailable) {
                [void](Read-AnyKey)
                Write-Host ("`r" + (" " * 46)) -NoNewline
                Say "`r  press any key to close" "DarkGray"
                [void](Read-AnyKey); return
            }
            Start-Sleep -Milliseconds 100
        }
    }
}

# Hash separately from the text: the install always rewrites the file, so the
# timestamp moves on every run and only the hash says whether the build that
# came out is actually a different one.
function Describe([string]$path) {
    if (-not (Test-Path $path)) { return [pscustomobject]@{ Hash = ""; Text = "not installed" } }
    $f = Get-Item $path
    $h = (Get-FileHash $path -Algorithm SHA256).Hash
    [pscustomobject]@{
        Hash = $h
        Text = "{0}  {1:N0} bytes  {2}" -f $h.Substring(0, 12), $f.Length,
                                           $f.LastWriteTime.ToString("HH:mm:ss")
    }
}

Say ""
Say "  Rebuild Crema" "Magenta"
Say "  $repo" "DarkGray"
Say ""

# The build always succeeds whether or not crema is running; only the copy over
# the installed binary fails, because Windows locks a running image. Catch that
# before spending a build on it, and let the user fix it in place rather than
# closing the window on them — closing crema from here would take its open
# agents with it, and only crema's own quit saves those.
while ($true) {
    $running = @(Get-Process -Name crema -ErrorAction SilentlyContinue)
    if ($running.Count -eq 0) { break }
    Say "  Crema is running (pid $($running.Id -join ', '))." "Yellow"
    Say "  Quit it first — ctrl+q, so it saves your open agents." "Yellow"
    Say ""
    if (-not $script:interactive) { Say "  nothing done" "DarkGray"; exit 1 }
    Say "  press any key once it's closed, or q to give up" "DarkGray"
    if ((Read-AnyKey) -eq 'q') { Say ""; Say "  nothing done" "DarkGray"; Wait-Then-Close 3; exit 1 }
    Say ""
}

$before = Describe $installed
Say "  before : $($before.Text)" "DarkGray"
Say ""

try {
    & (Join-Path $PSScriptRoot "install.ps1") -Build
    if ($LASTEXITCODE -ne 0 -and $null -ne $LASTEXITCODE) { throw "install failed (exit $LASTEXITCODE)" }
} catch {
    Say ""
    Say "  FAILED" "Red"
    Say "  $($_.Exception.Message)" "Red"
    Say ""
    Wait-Then-Close 0   # an error is the one thing worth keeping on screen
    exit 1
}

$after = Describe $installed
Say ""
Say "  before : $($before.Text)" "DarkGray"
Say "  after  : $($after.Text)" "Green"
Say ""
if ($before.Hash -eq $after.Hash) {
    Say "  same binary — nothing that affects the build changed" "DarkGray"
} else {
    Say "  new binary installed" "Green"
}
Say "  the Crema shortcut now runs this build" "Green"
Wait-Then-Close 6
