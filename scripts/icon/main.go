// Command icon turns the product artwork into the multi-size .ico that
// Windows wants, so the one PNG in the repository stays the single source of
// the icon.
//
//	go run ./scripts/icon                      # Crema.png -> assets/crema.ico
//	go run github.com/akavel/rsrc@v0.10.2 -ico assets/crema.ico \
//	    -arch amd64 -o cmd/crema/rsrc_windows_amd64.syso
//
// The second step writes the COFF resource object that the Go linker picks up
// from the main package's directory; both outputs are committed, so an
// ordinary `go build` produces an icon-bearing binary with no tools installed.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

// sizes are the square icon sizes Windows picks between: the small ones for
// list views and the taskbar, 256 for the big Explorer tiles. Anything below
// 128 is stored as a plain bitmap, which every version of Windows reads; the
// two large ones are stored as PNG, which is what keeps the file small.
var sizes = []int{16, 24, 32, 48, 64, 128, 256}

const pngFrom = 128

func main() {
	in := flag.String("in", "Crema.png", "source artwork")
	out := flag.String("out", filepath.Join("assets", "crema.ico"), "icon to write")
	flag.Parse()

	if err := run(*in, *out); err != nil {
		fmt.Fprintln(os.Stderr, "icon:", err)
		os.Exit(1)
	}
}

func run(in, out string) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		return fmt.Errorf("%s: %w", in, err)
	}
	b := src.Bounds()
	if b.Dx() < sizes[len(sizes)-1] {
		return fmt.Errorf("%s is %dx%d, too small for a %d icon", in, b.Dx(), b.Dy(), sizes[len(sizes)-1])
	}

	var buf bytes.Buffer
	if err := writeICO(&buf, src); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s (%dx%d) -> %s, %d sizes, %.0f KB\n",
		in, b.Dx(), b.Dy(), out, len(sizes), float64(buf.Len())/1024)
	return nil
}

// ICO layout: a directory of fixed-size entries, then each image's bytes.
type iconDirEntry struct {
	Width, Height, Colors, Reserved uint8
	Planes, BitCount                uint16
	BytesInRes, ImageOffset         uint32
}

func writeICO(w *bytes.Buffer, src image.Image) error {
	images := make([][]byte, len(sizes))
	for i, n := range sizes {
		img := resize(src, n)
		if n >= pngFrom {
			var b bytes.Buffer
			if err := png.Encode(&b, img); err != nil {
				return err
			}
			images[i] = b.Bytes()
			continue
		}
		images[i] = dib(img)
	}

	// header: reserved, type 1 (icon), count
	binary.Write(w, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	offset := uint32(6 + 16*len(sizes))
	for i, n := range sizes {
		binary.Write(w, binary.LittleEndian, iconDirEntry{
			Width:       uint8(n), // 256 wraps to 0, which is what the format means by it
			Height:      uint8(n),
			Planes:      1,
			BitCount:    32,
			BytesInRes:  uint32(len(images[i])),
			ImageOffset: offset,
		})
		offset += uint32(len(images[i]))
	}
	for _, img := range images {
		w.Write(img)
	}
	return nil
}

// dib encodes one image the way an .ico stores a bitmap: a header whose height
// counts both the colour rows and the mask rows, then bottom-up BGRA, then an
// all-zero mask. The mask is only there because the format demands it — with
// 32 bits per pixel Windows uses the alpha channel instead.
func dib(img *image.NRGBA) []byte {
	n := img.Bounds().Dx()
	maskRow := ((n + 31) / 32) * 4 // 1 bit per pixel, rows padded to 4 bytes

	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, struct {
		Size                         uint32
		Width, Height                int32
		Planes, BitCount             uint16
		Compression, SizeImage       uint32
		XPelsPerMeter, YPelsPerMeter int32
		ClrUsed, ClrImportant        uint32
	}{
		Size: 40, Width: int32(n), Height: int32(2 * n),
		Planes: 1, BitCount: 32,
		SizeImage: uint32(n*n*4 + n*maskRow),
	})
	for y := n - 1; y >= 0; y-- { // bottom-up
		for x := 0; x < n; x++ {
			c := img.NRGBAAt(x, y)
			b.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	b.Write(make([]byte, n*maskRow))
	return b.Bytes()
}

// resize scales src down to n×n by averaging every source pixel a destination
// pixel covers. The averaging is done on premultiplied values — Go's RGBA is
// already premultiplied — because averaging straight colour across a
// transparent edge drags whatever is behind the transparency into the result,
// which shows up as a dark halo around the artwork.
func resize(src image.Image, n int) *image.NRGBA {
	b := src.Bounds()
	pre := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(pre, pre.Bounds(), src, b.Min, draw.Src)

	dst := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		y0, y1 := y*pre.Bounds().Dy()/n, (y+1)*pre.Bounds().Dy()/n
		for x := 0; x < n; x++ {
			x0, x1 := x*pre.Bounds().Dx()/n, (x+1)*pre.Bounds().Dx()/n
			var r, g, bl, a, count uint64
			for sy := y0; sy < max(y1, y0+1); sy++ {
				for sx := x0; sx < max(x1, x0+1); sx++ {
					p := pre.RGBAAt(sx, sy)
					r, g, bl, a = r+uint64(p.R), g+uint64(p.G), bl+uint64(p.B), a+uint64(p.A)
					count++
				}
			}
			dst.SetNRGBA(x, y, unpremultiply(
				uint8(r/count), uint8(g/count), uint8(bl/count), uint8(a/count)))
		}
	}
	return dst
}

// unpremultiply converts back to straight colour, which is what both PNG and a
// 32-bit icon bitmap store.
func unpremultiply(r, g, b, a uint8) color.NRGBA {
	if a == 0 {
		return color.NRGBA{}
	}
	return color.NRGBA{
		R: uint8(min(255, int(r)*255/int(a))),
		G: uint8(min(255, int(g)*255/int(a))),
		B: uint8(min(255, int(b)*255/int(a))),
		A: a,
	}
}
