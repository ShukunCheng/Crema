# Console settings for a shortcut, shared by every .lnk these scripts create.
# Dot-source it: . (Join-Path $PSScriptRoot "consoleprops.ps1")
#
# The console host keeps its own font, window and colours per shortcut, with no
# relation to whatever Windows Terminal is set to. Left alone it picks a small
# default, so anything launched through conhost.exe needs these written for it.

# Get-ConsoleFontPixels converts a point size — what Windows Terminal and every
# font dialog speak — into the cell height in pixels that the console host
# wants. conhost stores that height literally and does not scale it for the
# display, so on a 150%-scaled screen the conversion is the whole difference
# between matching the terminal and being two thirds its size.
function Get-ConsoleFontPixels {
    param([int]$Points = 12)

    # Claim DPI awareness before asking. GetDpiForSystem reports 96 to a process
    # that hasn't claimed it, however the display is really set, and PowerShell
    # hasn't — so without this the answer depends on who invoked the script
    # rather than on the monitor, and a nested call gets a different size than a
    # direct run. The call fails harmlessly if awareness is already set.
    if (-not ('Crema.Dpi' -as [type])) {
        Add-Type -Name Dpi -Namespace Crema -MemberDefinition @'
[DllImport("user32.dll")] public static extern int GetDpiForSystem();
[DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr ctx);
'@
    }
    # DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
    try { [void][Crema.Dpi]::SetProcessDpiAwarenessContext([IntPtr]::new(-4)) } catch {}
    $dpi = try { [Crema.Dpi]::GetDpiForSystem() } catch { 96 }
    return [int][Math]::Round($Points * $dpi / 72)
}

# NT_CONSOLE_PROPS: the console settings the Properties dialog of a console
# shortcut edits. A .lnk ends with a list of extra-data blocks terminated by a
# zero DWORD, so writing one means inserting 204 bytes before that terminator.
# Layout is fixed by the shell link spec; every field is little-endian.
function Set-ConsoleProps {
    param(
        [string]$Link,
        [string]$Face,
        [int]$Height,
        [int]$Cols,
        [int]$Lines,
        # Crema turns the mouse into a pointing device, so the console host must
        # not keep it for its own selection. A plain output window has no such
        # need and is friendlier with the usual drag-to-copy left on.
        [switch]$QuickEdit
    )

    $b = [byte[]]::new(0xCC)
    $w = { param($off, $val, $bytes)
        [Array]::Copy([BitConverter]::GetBytes($val), 0, $b, $off, $bytes) }

    # The signature needs the L: PowerShell reads an 8-digit hex literal as a
    # signed Int32, so a bare 0xA0000002 is a negative number that compares
    # equal to nothing read back out of the file.
    & $w 0    0xCC         4   # block size
    & $w 4    0xA0000002L  4   # NT_CONSOLE_PROPS signature
    & $w 8    0x0007      2   # fill: light grey on colour 0, which is set dark below
    & $w 10   0x00F5      2   # popup fill
    & $w 12   $Cols       2   # screen buffer X
    & $w 14   $Lines      2   # screen buffer Y — equal to the window, so a
    & $w 16   $Cols       2   # window X          full-screen TUI gets no scrollbar
    & $w 18   $Lines      2   # window Y
    # 20..24 window origin, 24..32 unused — all zero
    & $w 32   ($Height -shl 16) 4  # font size: high word is the cell height
    & $w 36   54          4   # FF_MODERN | TMPF_VECTOR | TMPF_TRUETYPE
    & $w 40   400         4   # normal weight
    [Array]::Copy([Text.Encoding]::Unicode.GetBytes($Face), 0, $b, 44,
        [Math]::Min(62, [Text.Encoding]::Unicode.GetByteCount($Face)))
    & $w 108  25          4   # cursor size (percent)
    # 112 full screen, 120 insert mode — both zero.
    & $w 116  ([int]$QuickEdit.IsPresent) 4
    & $w 124  0           4   # auto position off, so the size above is honoured
    & $w 128  50          4   # history buffer size
    & $w 132  4           4   # number of history buffers

    # Colour 0 is the window's background before crema paints anything, so set
    # it to the theme's own dark plum rather than flashing black, and 7 is the
    # text on it. Crema itself draws in 24-bit colour and ignores this table,
    # but anything a subprocess writes in the sixteen indexed colours lands
    # here, so the other entries keep their conventional hues. 0x00BBGGRR.
    $palette = @(0x001F1217, 0x00800000, 0x00008000, 0x00808000,
                 0x00000080, 0x00800080, 0x00008080, 0x00E6D9F0,
                 0x00504060, 0x00FF6B6B, 0x00A8E08F, 0x00E8E09F,
                 0x009EA9FF, 0x00D06BE0, 0x009AD8F0, 0x00F7E6EF)
    for ($i = 0; $i -lt 16; $i++) { & $w (140 + $i * 4) $palette[$i] 4 }

    $bytes = [IO.File]::ReadAllBytes($Link)
    # Drop every block a previous run left behind, so re-running is idempotent
    # and conhost is never left choosing between two of them.
    for ($again = $true; $again; ) {
        $again = $false
        for ($i = 76; $i -le $bytes.Length - 8; $i++) {
            if ([BitConverter]::ToUInt32($bytes, $i) -eq 0xCC -and
                [BitConverter]::ToUInt32($bytes, $i + 4) -eq 0xA0000002L) {
                $bytes = [byte[]]($bytes[0..($i - 1)] +
                                  $bytes[($i + 0xCC)..($bytes.Length - 1)])
                $again = $true
                break
            }
        }
    }
    # The terminator is the trailing zero DWORD; the block goes in front of it.
    $head = $bytes[0..($bytes.Length - 5)]
    [IO.File]::WriteAllBytes($Link, [byte[]]($head + $b + @(0, 0, 0, 0)))
}
