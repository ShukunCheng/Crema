//go:build windows

package ui

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	procSetConsoleTitleW = kernel32.NewProc("SetConsoleTitleW")

	procSendMessageW = user32.NewProc("SendMessageW")

	shell32                         = windows.NewLazySystemDLL("shell32.dll")
	procExtractIconExW              = shell32.NewProc("ExtractIconExW")
	procSHGetPropertyStoreForWindow = shell32.NewProc("SHGetPropertyStoreForWindow")
	procSetProcessAppID             = shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
)

const (
	wmSetIcon = 0x0080
	iconSmall = 0
	iconBig   = 1

	// AppID is what crema calls itself to the shell. The taskbar groups
	// windows by this, so naming it is what keeps crema's button its own
	// rather than folded in with whatever else the console host is running.
	AppID = "Gomocha.Crema"
)

// setConsoleTitle names the window through the console API. A classic console
// host puts this in the title bar, which is what its taskbar button and its
// alt-tab entry read; Windows Terminal ignores it in favour of the OSC
// sequence, which says the same thing.
func setConsoleTitle(title string) {
	if p, err := windows.UTF16PtrFromString(title); err == nil {
		procSetConsoleTitleW.Call(uintptr(unsafe.Pointer(p)))
	}
}

// adoptConsoleWindow puts crema's own icon on the console window, so a window
// crema has to itself carries the product icon rather than the host's.
//
// This only bites when crema owns a real window — a classic console host, the
// shape scripts/shortcut.ps1 arranges. Inside Windows Terminal the visible
// window belongs to the terminal and this handle is the hidden pseudo-console
// one, so the message lands somewhere harmless and nothing changes: a guest
// program cannot repaint its host's window, and the taskbar groups by that
// window's owner.
func adoptConsoleWindow() {
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	path, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return
	}
	// The icons come out of crema's own resources, by way of the file on disk,
	// so there is one source for them: the .syso the build embeds.
	var large, small windows.Handle
	procExtractIconExW.Call(uintptr(unsafe.Pointer(path)), 0,
		uintptr(unsafe.Pointer(&large)), uintptr(unsafe.Pointer(&small)), 1)
	if large != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconBig, uintptr(large))
	}
	if small != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, uintptr(small))
	}
	claimTaskbarButton(hwnd)
}

// The shell's window property store, reached by hand because this is the only
// COM crema needs. IPropertyStore's methods sit after IUnknown's three.
const (
	psSetValue = 6
	psCommit   = 7
	psRelease  = 2
)

// comObject is any COM interface pointer: the first word is its vtable.
type comObject struct{ vtbl *[16]uintptr }

type propertyKey struct {
	fmtid windows.GUID
	pid   uint32
}

// PKEY_AppUserModel_ID: the property a window sets to tell the taskbar which
// application it belongs to.
var pkeyAppUserModelID = propertyKey{
	fmtid: windows.GUID{Data1: 0x9F4C2855, Data2: 0x9F79, Data3: 0x4B39,
		Data4: [8]byte{0xA8, 0xD0, 0xE1, 0xD4, 0x2D, 0xE1, 0xD5, 0xF3}},
	pid: 5,
}

var iidPropertyStore = windows.GUID{Data1: 0x886D8EEB, Data2: 0x8CF2, Data3: 0x4446,
	Data4: [8]byte{0x8D, 0x02, 0xCD, 0xBA, 0x1D, 0xBD, 0xCF, 0x99}}

// propVariant is only ever a string here, so the union is one pointer.
type propVariant struct {
	vt       uint16
	reserved [3]uint16
	val      uintptr
	_        uintptr
}

const vtLPWSTR = 31

// claimTaskbarButton tells the shell that this window is Crema and not another
// instance of whatever console host is drawing it. Without it the taskbar
// falls back to guessing from the process behind the window, which is how a
// console window ends up sharing a button with every other terminal.
//
// The process-wide call covers windows made later; the property store covers
// the console window, which already existed before crema started running.
func claimTaskbarButton(hwnd uintptr) {
	id, err := windows.UTF16PtrFromString(AppID)
	if err != nil {
		return
	}
	procSetProcessAppID.Call(uintptr(unsafe.Pointer(id)))

	// COM has to be initialised on the thread that uses it, so this stays put
	// for the handful of calls below.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// A thread already initialised in the other mode answers RPC_E_CHANGED_MODE
	// and the store still works — but then the reference is somebody else's,
	// and giving it back is not ours to do.
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err == nil {
		defer windows.CoUninitialize()
	}

	var store *comObject
	hr, _, _ := procSHGetPropertyStoreForWindow.Call(hwnd,
		uintptr(unsafe.Pointer(&iidPropertyStore)), uintptr(unsafe.Pointer(&store)))
	if hr != 0 || store == nil {
		return
	}
	this := uintptr(unsafe.Pointer(store))
	pv := propVariant{vt: vtLPWSTR, val: uintptr(unsafe.Pointer(id))}
	syscall.SyscallN(store.vtbl[psSetValue], this,
		uintptr(unsafe.Pointer(&pkeyAppUserModelID)), uintptr(unsafe.Pointer(&pv)))
	syscall.SyscallN(store.vtbl[psCommit], this)
	syscall.SyscallN(store.vtbl[psRelease], this)
	runtime.KeepAlive(id)
}
