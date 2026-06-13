//go:build ignore

// genicons writes three 16x16 32bpp .ico files (active/paused/error) used as
// the system-tray state icons. Run once with: go run assets/genicons.go
// The generated .ico files are embedded by assets/icons.go via go:embed.
package main

import (
	"bytes"
	"encoding/binary"
	"log"
	"os"
)

const dim = 16

// writeICO builds a single-image 32bpp BMP-backed .ico for the given RGBA fill.
func buildICO(r, g, b byte) []byte {
	// XOR bitmap: dim*dim BGRA pixels, bottom-up.
	xor := make([]byte, 0, dim*dim*4)
	for y := 0; y < dim; y++ {
		for x := 0; x < dim; x++ {
			// Simple filled circle so the icon reads as a colored dot.
			dx, dy := float64(x)-7.5, float64(y)-7.5
			inside := dx*dx+dy*dy <= 7.0*7.0
			if inside {
				xor = append(xor, b, g, r, 0xFF) // BGRA, opaque
			} else {
				xor = append(xor, 0, 0, 0, 0) // transparent
			}
		}
	}
	// AND mask: 1 bit per pixel, rows padded to 32-bit boundary. All zero
	// (alpha channel governs transparency for 32bpp icons).
	rowBytes := ((dim + 31) / 32) * 4
	and := make([]byte, rowBytes*dim)

	var img bytes.Buffer
	// BITMAPINFOHEADER (40 bytes). biHeight is doubled (XOR + AND).
	binary.Write(&img, binary.LittleEndian, uint32(40)) // biSize
	binary.Write(&img, binary.LittleEndian, int32(dim)) // biWidth
	binary.Write(&img, binary.LittleEndian, int32(dim*2))
	binary.Write(&img, binary.LittleEndian, uint16(1))  // biPlanes
	binary.Write(&img, binary.LittleEndian, uint16(32)) // biBitCount
	binary.Write(&img, binary.LittleEndian, uint32(0))  // biCompression
	binary.Write(&img, binary.LittleEndian, uint32(0))  // biSizeImage
	binary.Write(&img, binary.LittleEndian, int32(0))   // biXPelsPerMeter
	binary.Write(&img, binary.LittleEndian, int32(0))   // biYPelsPerMeter
	binary.Write(&img, binary.LittleEndian, uint32(0))  // biClrUsed
	binary.Write(&img, binary.LittleEndian, uint32(0))  // biClrImportant
	img.Write(xor)
	img.Write(and)

	imgBytes := img.Bytes()

	var out bytes.Buffer
	// ICONDIR
	binary.Write(&out, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&out, binary.LittleEndian, uint16(1)) // type = icon
	binary.Write(&out, binary.LittleEndian, uint16(1)) // count
	// ICONDIRENTRY
	out.WriteByte(dim) // width
	out.WriteByte(dim) // height
	out.WriteByte(0)   // color count
	out.WriteByte(0)   // reserved
	binary.Write(&out, binary.LittleEndian, uint16(1))  // planes
	binary.Write(&out, binary.LittleEndian, uint16(32)) // bit count
	binary.Write(&out, binary.LittleEndian, uint32(len(imgBytes)))
	binary.Write(&out, binary.LittleEndian, uint32(6+16)) // image offset
	out.Write(imgBytes)
	return out.Bytes()
}

func main() {
	icons := map[string][3]byte{
		"assets/icon_active.ico": {0x2E, 0xCC, 0x71}, // green
		"assets/icon_paused.ico": {0xF1, 0xC4, 0x0F}, // yellow
		"assets/icon_error.ico":  {0xE7, 0x4C, 0x3C}, // red
	}
	for path, c := range icons {
		if err := os.WriteFile(path, buildICO(c[0], c[1], c[2]), 0o644); err != nil {
			log.Fatalf("write %s: %v", path, err)
		}
		log.Printf("wrote %s", path)
	}
}
