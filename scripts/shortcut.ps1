# Creates a shortcut that opens crema in a window of its own, so it gets its
# own taskbar button with the product icon instead of joining whatever terminal
# started it.
#
#   pwsh -File scripts/shortcut.ps1                    # Desktop and Start menu
#   pwsh -File scripts/shortcut.ps1 -Dir D:\my-project # opened on a folder
#   pwsh -File scripts/shortcut.ps1 -NoStartMenu       # Desktop only
#
# Two things make the button separate. The target is conhost.exe, the classic
# console host: asking it to run a program gives that program a window nobody
# else is sharing, where launching crema.exe directly would hand it to whatever
# "Terminal application" Windows Settings names — on Windows 11 that is Windows
# Terminal, whose window, and whose taskbar button, belong to the terminal. And
# the shortcut carries the same AppUserModelID crema puts on its window, so the
# taskbar treats a pinned Crema and a running Crema as one application.
#
# The console host also has its own font and window settings, which have
# nothing to do with Windows Terminal's — left alone it picks a small default
# that looks nothing like the terminal crema was developed in. Those settings
# live inside the .lnk, in a block the scripting interface cannot write, so
# this script appends it by hand after the shortcut is saved.
param(
    [string]$Exe = (Join-Path (Split-Path -Parent $PSScriptRoot) "crema.exe"),
    [string]$Dir = "",
    # WorkingDir is where crema starts when nothing tells it otherwise, without
    # the --dir that -Dir adds: --dir always opens an agent on that folder, even
    # when there are saved ones to restore, which would stack up a new agent on
    # every launch. Defaults to wherever the binary lives.
    [string]$WorkingDir = "",
    [string]$Name = "Crema",
    [string]$AppId = "Gomocha.Crema",
    [string]$Font = "Cascadia Mono",  # Windows Terminal's own default face
    [int]$FontPoints = 12,            # ...at Windows Terminal's own default size
    [int]$FontSize = 0,               # exact cell height in px; overrides -FontPoints
    [int]$Columns = 140,
    [int]$Rows = 38,
    [switch]$NoStartMenu
)
$ErrorActionPreference = "Stop"

. (Join-Path $PSScriptRoot "consoleprops.ps1")
if ($FontSize -le 0) { $FontSize = Get-ConsoleFontPixels $FontPoints }

$Exe = (Resolve-Path $Exe).Path
$conhost = Join-Path $env:SystemRoot "System32\conhost.exe"
if (-not (Test-Path $conhost)) { throw "conhost.exe not found — this script is for Windows" }

$arguments = "`"$Exe`""
$workdir = Split-Path -Parent $Exe
if ($WorkingDir) { $workdir = (Resolve-Path $WorkingDir).Path }
if ($Dir) {
    $Dir = (Resolve-Path $Dir).Path
    $arguments += " --dir `"$Dir`""
    $workdir = $Dir
}

# WScript.Shell writes every part of a shortcut except the AppUserModelID,
# which has no scripting interface — that one is set through the shell's
# property store below.
Add-Type -ErrorAction Stop @"
using System;
using System.Runtime.InteropServices;

[StructLayout(LayoutKind.Sequential)]
public struct PropertyKey { public Guid fmtid; public uint pid; }

[ComImport, Guid("886D8EEB-8CF2-4446-8D02-CDBA1DBDCF99"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IPropertyStore {
    void GetCount(out uint count);
    void GetAt(uint index, out PropertyKey key);
    void GetValue(ref PropertyKey key, IntPtr value);
    void SetValue(ref PropertyKey key, IntPtr value);
    void Commit();
}

[ComImport, Guid("0000010B-0000-0000-C000-000000000046"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IPersistFile {
    void GetClassID(out Guid id);
    [PreserveSig] int IsDirty();
    void Load([MarshalAs(UnmanagedType.LPWStr)] string file, uint mode);
    void Save([MarshalAs(UnmanagedType.LPWStr)] string file, [MarshalAs(UnmanagedType.Bool)] bool remember);
    void SaveCompleted([MarshalAs(UnmanagedType.LPWStr)] string file);
    void GetCurFile([MarshalAs(UnmanagedType.LPWStr)] out string file);
}

public static class ShortcutAppId {
    [DllImport("ole32.dll")] static extern int PropVariantClear(IntPtr pv);

    // A PROPVARIANT holding a string: the type tag, then the pointer where the
    // union starts. The string has to come from the COM allocator, because
    // PropVariantClear is what frees it.
    const ushort VT_LPWSTR = 31;
    static void SetString(IntPtr pv, string s) {
        for (int i = 0; i < 32; i++) Marshal.WriteByte(pv, i, 0);
        Marshal.WriteInt16(pv, 0, (short)VT_LPWSTR);
        Marshal.WriteIntPtr(pv, 8, Marshal.StringToCoTaskMemUni(s));
    }

    // Stamps an existing .lnk with the application it belongs to.
    public static void Set(string link, string appId) {
        var shellLink = Type.GetTypeFromCLSID(new Guid("00021401-0000-0000-C000-000000000046"));
        object o = Activator.CreateInstance(shellLink);
        try {
            ((IPersistFile)o).Load(link, 2 /* STGM_READWRITE */);
            var key = new PropertyKey {
                fmtid = new Guid("9F4C2855-9F79-4B39-A8D0-E1D42DE1D5F3"), pid = 5 };
            IntPtr pv = Marshal.AllocCoTaskMem(32);
            try {
                SetString(pv, appId);
                var store = (IPropertyStore)o;
                store.SetValue(ref key, pv);
                store.Commit();
            } finally {
                PropVariantClear(pv);
                Marshal.FreeCoTaskMem(pv);
            }
            ((IPersistFile)o).Save(link, true);
        } finally {
            Marshal.ReleaseComObject(o);
        }
    }
}
"@


$targets = @([Environment]::GetFolderPath("Desktop"))
if (-not $NoStartMenu) {
    $targets += Join-Path ([Environment]::GetFolderPath("ApplicationData")) "Microsoft\Windows\Start Menu\Programs"
}

$shell = New-Object -ComObject WScript.Shell
foreach ($folder in $targets) {
    $link = Join-Path $folder "$Name.lnk"
    $s = $shell.CreateShortcut($link)
    $s.TargetPath = $conhost
    $s.Arguments = $arguments
    $s.WorkingDirectory = $workdir
    $s.IconLocation = "$Exe,0"   # the icon built into the binary
    $s.Description = "Crema — a terminal UI for Claude Code and Codex"
    $s.Save()
    [ShortcutAppId]::Set($link, $AppId)
    # Last, because the two steps above rewrite the whole file.
    Set-ConsoleProps -Link $link -Face $Font -Height $FontSize -Cols $Columns -Lines $Rows
    "created $link  ($Font ${FontSize}px, ${Columns}x${Rows})"
}
"AppUserModelID: $AppId — right-click the running window's taskbar button to pin it"
