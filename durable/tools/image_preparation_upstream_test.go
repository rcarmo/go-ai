package tools

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rcarmo/go-ai/durable"
	"golang.org/x/image/bmp"
)

// Source: pi v1.6.0 packages/durable/test/tools-images.test.ts:229-251,268-330.
func TestSourceV160ImagePreparation(t *testing.T) {
	pngAt := func(w, h int) []byte {
		t.Helper()
		var out bytes.Buffer
		if err := png.Encode(&out, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	const side = 16
	grid := []string{"RGB", "WKY"}
	palette := map[byte]color.NRGBA{
		'R': {R: 255, A: 255}, 'G': {G: 255, A: 255}, 'B': {B: 255, A: 255},
		'W': {R: 255, G: 255, B: 255, A: 255}, 'K': {A: 255}, 'Y': {R: 255, G: 255, A: 255},
	}
	pixels := image.NewNRGBA(image.Rect(0, 0, 3*side, 2*side))
	for y, row := range grid {
		for x := range len(row) {
			rect := image.Rect(x*side, y*side, (x+1)*side, (y+1)*side)
			draw.Draw(pixels, rect, &image.Uniform{C: palette[row[x]]}, image.Point{}, draw.Src)
		}
	}
	jpegAt := func(src image.Image) []byte {
		t.Helper()
		var out bytes.Buffer
		if err := jpeg.Encode(&out, src, &jpeg.Options{Quality: 95}); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	stored := jpegAt(pixels)
	for i, expected := range []string{"RGB/WKY", "BGR/YKW", "YKW/BGR", "WKY/RGB", "RW/GK/BY", "WR/KG/YB", "YB/KG/WR", "BY/GK/RW"} {
		orientation := i + 1
		t.Run(fmt.Sprintf("orientation-%d", orientation), func(t *testing.T) {
			input := sourceJPEGOrientation(stored, orientation, binary.BigEndian, false)
			prepared, err := PrepareImage(t.Context(), input, "image/jpeg", ImageLimits{})
			if err != nil {
				t.Fatal(err)
			}
			decoded, _, err := image.Decode(bytes.NewReader(prepared.Data))
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Split(expected, "/")
			if decoded.Bounds().Dx() != len(want[0])*side || decoded.Bounds().Dy() != len(want)*side {
				t.Fatalf("decoded bounds = %v; want %dx%d", decoded.Bounds(), len(want[0])*side, len(want)*side)
			}
			for y, row := range want {
				for x := range len(row) {
					r, g, b, _ := decoded.At(x*side+side/2, y*side+side/2).RGBA()
					p := palette[row[x]]
					if (r>>8 > 127) != (p.R == 255) || (g>>8 > 127) != (p.G == 255) || (b>>8 > 127) != (p.B == 255) {
						t.Errorf("centre (%d,%d) = [%d %d %d], want rounded colour %c", x, y, r>>8, g>>8, b>>8, row[x])
					}
				}
			}
		})
	}
	t.Run("jpeg-exif-parser", func(t *testing.T) { // 317
		jpeg := jpegAt(image.NewRGBA(image.Rect(0, 0, 2, 2)))
		for _, tc := range []struct {
			name  string
			data  []byte
			value int
		}{
			{"big-endian", sourceJPEGOrientation(jpeg, 6, binary.BigEndian, false), 6},
			{"little-endian", sourceJPEGOrientation(jpeg, 8, binary.LittleEndian, false), 8},
			{"app0-first", sourceJPEGOrientation(jpeg, 5, binary.BigEndian, true), 5},
			{"out-of-range", sourceJPEGOrientation(jpeg, 9, binary.BigEndian, false), 1},
			{"absent", jpeg, 1},
			{"png", pngAt(2, 2), 1},
		} {
			if got := imageOrientation(tc.data); got != tc.value {
				t.Errorf("%s orientation=%d; want %d", tc.name, got, tc.value)
			}
		}
	})
	t.Run("webp-exif-parser", func(t *testing.T) { // 327
		for _, tc := range []struct {
			value  int
			prefix bool
		}{{6, false}, {3, true}} {
			data := sourceWebPOrientation(tc.value, tc.prefix)
			if got := imageOrientation(data); got != tc.value {
				t.Errorf("orientation=%d with Exif prefix=%t; want %d", got, tc.prefix, tc.value)
			}
		}
	})
	t.Run("extreme-aspect", func(t *testing.T) { // 229
		out, err := PrepareImage(t.Context(), pngAt(6000, 2), "image/png", ImageLimits{})
		if err != nil || out.OriginalWidth != 6000 || out.OriginalHeight != 2 || out.Width != 2000 || out.Height != 1 {
			t.Errorf("resize %dx%d -> %dx%d, error=%v", out.OriginalWidth, out.OriginalHeight, out.Width, out.Height, err)
		}
	})
	t.Run("one-pixel-over-budget", func(t *testing.T) { // 234
		out, err := PrepareImage(t.Context(), pngAt(4, 4), "image/png", ImageLimits{MaxWidth: 2000, MaxHeight: 2000, MaxBase64: 8})
		// The Go API returns an error instead of the JS undefined result.
		if err == nil || len(out.Data) != 0 {
			t.Errorf("encoded bytes=%d, error=%v; want refusal with no image", len(out.Data), err)
		}
	})
	t.Run("jpeg-stays-jpeg", func(t *testing.T) { // 239
		data := sourceJPEGOrientation(jpegAt(image.NewRGBA(image.Rect(0, 0, 40, 30))), 3, binary.BigEndian, false)
		out, err := PrepareImage(t.Context(), data, "image/jpeg", ImageLimits{})
		if err != nil || out.MimeType != "image/jpeg" {
			t.Errorf("prepared MIME=%q; error=%v", out.MimeType, err)
		}
	})
	t.Run("prepared-bmp-read", func(t *testing.T) { // 248
		root := t.TempDir()
		var img bytes.Buffer
		if err := bmp.Encode(&img, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "a.bmp"), img.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := ReadWithImages(LocalEnv(root)).Execute(t.Context(), durable.JSON{"path": "a.bmp"}, nil)
		if err != nil || got.IsError || len(got.Blocks) != 1 || got.Blocks[0].Type != "image" || got.Blocks[0].MimeType != "image/png" {
			t.Fatalf("prepared read=%+v; error=%v", got, err)
		}
		const want = "Read image file [image/png]. Converted from image/bmp to image/png."
		if len(got.Diagnostics) != 1 || got.Diagnostics[0].Message != want {
			t.Errorf("diagnostics=%+v; want only %q", got.Diagnostics, want)
		}
	})
}

func sourceImageTIFF(value int, order binary.ByteOrder) []byte {
	tiff := make([]byte, 26)
	copy(tiff, "MM")
	if order == binary.LittleEndian {
		copy(tiff, "II")
	}
	order.PutUint16(tiff[2:], 42)
	order.PutUint32(tiff[4:], 8)
	order.PutUint16(tiff[8:], 1)
	order.PutUint16(tiff[10:], 0x112)
	order.PutUint16(tiff[12:], 3)
	order.PutUint32(tiff[14:], 1)
	order.PutUint16(tiff[18:], uint16(value))
	return tiff
}

func sourceJPEGOrientation(data []byte, value int, order binary.ByteOrder, app0 bool) []byte {
	out := append([]byte{}, data[:2]...)
	if app0 {
		out = append(out, 0xff, 0xe0, 0, 16, 'J', 'F', 'I', 'F', 0, 1, 1, 0, 0, 1, 0, 1, 0, 0)
	}
	exif := append([]byte("Exif\x00\x00"), sourceImageTIFF(value, order)...)
	out = append(out, 0xff, 0xe1)
	out = binary.BigEndian.AppendUint16(out, uint16(len(exif)+2))
	out = append(out, exif...)
	return append(out, data[2:]...)
}

func sourceWebPOrientation(value int, prefix bool) []byte {
	data := sourceImageTIFF(value, binary.LittleEndian)
	if prefix {
		data = append([]byte("Exif\x00\x00"), data...)
	}
	// The unchanged fixture has a preceding odd-length VP8 chunk plus pad.
	out := []byte("RIFF\x00\x00\x00\x00WEBPVP8 \x03\x00\x00\x00\x01\x02\x03\x00EXIF")
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)+4+len(data)-8))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(data)))
	return append(out, data...)
}
