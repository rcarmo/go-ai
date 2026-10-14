package tools

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"os"
	"testing"
)

// WebP fixture: golang.org/x/image v0.39.0 testdata, BSD license alongside.
func TestImage120WebPDecodePassThroughResizeAndBMPConversion(t *testing.T) {
	data, e := os.ReadFile("testdata/gopher-doc-lossless.webp")
	if e != nil {
		t.Fatal(e)
	}
	config, _, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil {
		t.Fatal(e)
	}
	prepared, e := PrepareImage(context.Background(), data, "image/webp", ImageLimits{})
	if e != nil || prepared.MimeType != "image/webp" || prepared.Width != config.Width {
		t.Fatal(prepared, e)
	}
	prepared, e = PrepareImage(context.Background(), data, "image/webp", ImageLimits{MaxWidth: 20, MaxHeight: 20, MaxBase64: 4096})
	if e != nil || prepared.Width > 20 || prepared.Height > 20 || prepared.ConvertedFrom != "image/webp" {
		t.Fatal(prepared, e)
	}
	bmp := make([]byte, 58)
	copy(bmp, "BM")
	binary.LittleEndian.PutUint32(bmp[2:], 58)
	binary.LittleEndian.PutUint32(bmp[10:], 54)
	binary.LittleEndian.PutUint32(bmp[14:], 40)
	binary.LittleEndian.PutUint32(bmp[18:], 1)
	binary.LittleEndian.PutUint32(bmp[22:], 1)
	binary.LittleEndian.PutUint16(bmp[26:], 1)
	binary.LittleEndian.PutUint16(bmp[28:], 24)
	bmp[54], bmp[55], bmp[56] = 30, 60, 90
	prepared, e = PrepareImage(context.Background(), bmp, "image/bmp", ImageLimits{})
	if e != nil || prepared.MimeType != "image/png" || prepared.ConvertedFrom != "image/bmp" || prepared.Width != 1 {
		t.Fatal(prepared, e)
	}
	decoded, _, e := image.Decode(bytes.NewReader(prepared.Data))
	if e != nil {
		t.Fatal(e)
	}
	pixel := color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA)
	if pixel.R != 90 || pixel.G != 60 || pixel.B != 30 {
		t.Fatal(pixel)
	}
}
func TestImage120OrientationsPixelMappingAndMalformedEXIF(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 2; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(1 + y*2 + x), A: 255})
		}
	}
	expected := [][]uint8{{1, 2, 3, 4, 5, 6}, {2, 1, 4, 3, 6, 5}, {6, 5, 4, 3, 2, 1}, {5, 6, 3, 4, 1, 2}, {1, 3, 5, 2, 4, 6}, {5, 3, 1, 6, 4, 2}, {6, 4, 2, 5, 3, 1}, {2, 4, 6, 1, 3, 5}}
	for orientation := 1; orientation <= 8; orientation++ {
		out := orientImage(src, orientation)
		index := 0
		for y := 0; y < out.Bounds().Dy(); y++ {
			for x := 0; x < out.Bounds().Dx(); x++ {
				r, _, _, _ := out.At(x, y).RGBA()
				if uint8(r>>8) != expected[orientation-1][index] {
					t.Fatal("orientation pixel", orientation, index, r>>8)
				}
				index++
			}
		}
	}
	for _, data := range [][]byte{nil, []byte("Exif"), {0xff, 0xd8, 0xff, 0xe1, 0xff, 0xff}, {0xff, 0xd8, 0xff, 0xe1, 0, 2}} {
		if imageOrientation(data) != 1 {
			t.Fatal("malformed orientation")
		}
	}
	tiff := make([]byte, 8)
	copy(tiff, "II")
	binary.LittleEndian.PutUint16(tiff[2:], 42)
	binary.LittleEndian.PutUint32(tiff[4:], ^uint32(0))
	if tiffOrientation(tiff) != 1 {
		t.Fatal("EXIF offset overflow")
	}
	good := make([]byte, 26)
	copy(good, "II")
	binary.LittleEndian.PutUint16(good[2:], 42)
	binary.LittleEndian.PutUint32(good[4:], 8)
	binary.LittleEndian.PutUint16(good[8:], 1)
	binary.LittleEndian.PutUint16(good[10:], 0x112)
	binary.LittleEndian.PutUint16(good[12:], 3)
	binary.LittleEndian.PutUint32(good[14:], 1)
	binary.LittleEndian.PutUint16(good[18:], 8)
	webp := make([]byte, 12)
	copy(webp, "RIFF")
	copy(webp[8:], "WEBP")
	webp = append(webp, []byte("EXIF")...)
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(len(good)))
	webp = append(webp, size...)
	webp = append(webp, good...)
	binary.LittleEndian.PutUint32(webp[4:], uint32(len(webp)-8))
	if imageOrientation(webp) != 8 {
		t.Fatal("WebP EXIF orientation")
	}
}
