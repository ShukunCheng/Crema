//go:build windows

package ui

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows offers a copied image in whichever formats the program that copied
// it felt like providing. Three cover everything crema is likely to meet: a
// browser or a screenshot tool puts a PNG on, Print Screen and the older paint
// programs put a device-independent bitmap on, and copying a file in Explorer
// puts the path on. The first and last need no conversion at all.

var errNoImage = errors.New("no image on the clipboard")

const (
	cfDIB   = 8
	cfHDROP = 15

	gmemMoveable = 0x0002
)

var (
	procOpenClipboard             = user32.NewProc("OpenClipboard")
	procCloseClipboard            = user32.NewProc("CloseClipboard")
	procGetClipboardData          = user32.NewProc("GetClipboardData")
	procIsClipboardFormatAvailabl = user32.NewProc("IsClipboardFormatAvailable")
	procRegisterClipboardFormatW  = user32.NewProc("RegisterClipboardFormatW")
	procDragQueryFileW            = shell32.NewProc("DragQueryFileW")

	kernel32b        = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalLock   = kernel32b.NewProc("GlobalLock")
	procGlobalUnlock = kernel32b.NewProc("GlobalUnlock")
	procGlobalSize   = kernel32b.NewProc("GlobalSize")
	procMoveMemory   = kernel32b.NewProc("RtlMoveMemory")
)

// imageExts are the ones the agents can read back out of a file.
var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true,
}

// clipboardImage puts whatever image is on the clipboard into dir and returns
// the file. A copied *file* is left where it is and its own path comes back —
// there is no reason to duplicate a picture that already exists on disk.
func clipboardImage(dir string) (string, error) {
	if err := openClipboard(); err != nil {
		return "", err
	}
	defer procCloseClipboard.Call()

	if path, ok := droppedImage(); ok {
		return path, nil
	}
	if data, ok := clipboardBytes(pngFormat()); ok {
		return writeImage(dir, "png", data)
	}
	if data, ok := clipboardBytes(cfDIB); ok {
		img, err := decodeDIB(data)
		if err != nil {
			return "", err
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return "", err
		}
		return writeImage(dir, "png", buf.Bytes())
	}
	return "", errNoImage
}

// openClipboard retries: the clipboard is a single global thing, and whichever
// program was last asked about it may not have let go yet.
func openClipboard() error {
	for i := 0; i < 10; i++ {
		if ok, _, _ := procOpenClipboard.Call(0); ok != 0 {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("the clipboard is busy")
}

func pngFormat() uintptr {
	name, err := windows.UTF16PtrFromString("PNG")
	if err != nil {
		return 0
	}
	f, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name)))
	return f
}

// clipboardBytes copies out one format's bytes while the clipboard is open.
func clipboardBytes(format uintptr) ([]byte, bool) {
	if format == 0 {
		return nil, false
	}
	if ok, _, _ := procIsClipboardFormatAvailabl.Call(format); ok == 0 {
		return nil, false
	}
	h, _, _ := procGetClipboardData.Call(format)
	if h == 0 {
		return nil, false
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return nil, false
	}
	defer procGlobalUnlock.Call(h)
	n, _, _ := procGlobalSize.Call(h)
	if n == 0 {
		return nil, false
	}
	// Copied rather than aliased: the clipboard's memory belongs to whoever
	// put it there and is only ours until the clipboard closes. Asking the OS
	// to do the copy also keeps the pointer on the syscall side of the fence,
	// where a Go pointer never has to be made out of an address.
	out := make([]byte, n)
	procMoveMemory.Call(uintptr(unsafe.Pointer(&out[0])), p, n)
	return out, true
}

// droppedImage reports a file copied in Explorer, when it is an image.
func droppedImage() (string, bool) {
	if ok, _, _ := procIsClipboardFormatAvailabl.Call(cfHDROP); ok == 0 {
		return "", false
	}
	h, _, _ := procGetClipboardData.Call(cfHDROP)
	if h == 0 {
		return "", false
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, _, _ := procDragQueryFileW.Call(h, 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return "", false
	}
	path := windows.UTF16ToString(buf[:n])
	if !imageExts[strings.ToLower(filepath.Ext(path))] {
		return "", false
	}
	return path, true
}

func writeImage(dir, ext string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("paste-%s.%s", time.Now().Format("20060102-150405"), ext))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// decodeDIB turns a clipboard bitmap into an image. Only the true-colour
// depths are handled — a screenshot is one of those, and a palette-indexed
// clipboard image is a museum piece.
func decodeDIB(data []byte) (image.Image, error) {
	const headerSize = 40
	if len(data) < headerSize {
		return nil, errors.New("clipboard bitmap is too short to have a header")
	}
	var h struct {
		Size          uint32
		Width, Height int32
		Planes, Bits  uint16
		Compression   uint32
		SizeImage     uint32
		XPPM, YPPM    int32
		Used, Needed  uint32
	}
	if err := binary.Read(bytes.NewReader(data[:headerSize]), binary.LittleEndian, &h); err != nil {
		return nil, err
	}
	if h.Bits != 24 && h.Bits != 32 {
		return nil, fmt.Errorf("clipboard bitmap is %d bits per pixel, which crema can't read", h.Bits)
	}

	// The pixels start after the header, and after the three colour masks when
	// the header says the channels are laid out by mask rather than in order.
	off := int(h.Size)
	if off < headerSize {
		off = headerSize
	}
	if h.Compression == 3 { // BI_BITFIELDS
		off += 12
	}
	if off > len(data) {
		return nil, errors.New("clipboard bitmap has no pixels")
	}
	pix := data[off:]

	w, flip := int(h.Width), h.Height > 0
	ht := int(h.Height)
	if !flip {
		ht = -ht
	}
	if w <= 0 || ht <= 0 {
		return nil, errors.New("clipboard bitmap has no size")
	}
	stride := ((w*int(h.Bits) + 31) / 32) * 4
	if len(pix) < stride*ht {
		return nil, errors.New("clipboard bitmap is shorter than its own size")
	}

	step := int(h.Bits) / 8
	img := image.NewNRGBA(image.Rect(0, 0, w, ht))
	for y := 0; y < ht; y++ {
		row := y
		if flip { // a positive height means the rows are stored bottom-up
			row = ht - 1 - y
		}
		line := pix[row*stride:]
		for x := 0; x < w; x++ {
			p := line[x*step:]
			// Alpha is forced opaque: a 32-bit clipboard bitmap usually leaves
			// that byte at zero, and honouring it would paste a blank image.
			img.SetNRGBA(x, y, color.NRGBA{R: p[2], G: p[1], B: p[0], A: 255})
		}
	}
	return img, nil
}
