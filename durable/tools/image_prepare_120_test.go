package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestImage120PrepareBoundsOrientationAndColour(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 12, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 12; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 120, G: 80, B: 40, A: 255})
		}
	}
	var encoded bytes.Buffer
	if e := png.Encode(&encoded, src); e != nil {
		t.Fatal(e)
	}
	prepared, e := PrepareImage(context.Background(), encoded.Bytes(), "image/png", ImageLimits{MaxWidth: 6, MaxHeight: 4, MaxBase64: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if prepared.Width != 6 || prepared.Height != 4 || base64.StdEncoding.EncodedLen(len(prepared.Data)) > 1000 || prepared.OriginalWidth != 12 {
		t.Fatal(prepared)
	}
	decoded, _, e := image.Decode(bytes.NewReader(prepared.Data))
	if e != nil {
		t.Fatal(e)
	}
	r, g, b, a := decoded.At(2, 2).RGBA()
	for _, pair := range [][2]int{{int(r >> 8), 120}, {int(g >> 8), 80}, {int(b >> 8), 40}, {int(a >> 8), 255}} {
		if delta := pair[0] - pair[1]; delta < -1 || delta > 1 {
			t.Fatal("colour tolerance exceeded", pair)
		}
	}
	encoded.Reset()
	if e = jpeg.Encode(&encoded, src, nil); e != nil {
		t.Fatal(e)
	}
	original := encoded.Bytes()
	exif := make([]byte, 32)
	copy(exif, []byte("Exif\x00\x00II"))
	binary.LittleEndian.PutUint16(exif[8:10], 42)
	binary.LittleEndian.PutUint32(exif[10:14], 8)
	binary.LittleEndian.PutUint16(exif[14:16], 1)
	binary.LittleEndian.PutUint16(exif[16:18], 0x112)
	binary.LittleEndian.PutUint16(exif[18:20], 3)
	binary.LittleEndian.PutUint32(exif[20:24], 1)
	binary.LittleEndian.PutUint16(exif[24:26], 6)
	oriented := append([]byte{}, original[:2]...)
	oriented = append(oriented, 0xff, 0xe1, 0, byte(len(exif)+2))
	oriented = append(oriented, exif...)
	oriented = append(oriented, original[2:]...)
	if jpegOrientation(oriented) != 6 {
		t.Fatal("orientation metadata")
	}
	prepared, e = PrepareImage(context.Background(), oriented, "image/jpeg", ImageLimits{})
	if e != nil || prepared.Width != 8 || prepared.Height != 12 {
		t.Fatal(prepared, e)
	}
}
func TestImage120PrepareDecodeAndLimitGuards(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := PrepareImage(ctx, []byte{1}, "image/png", ImageLimits{}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e := PrepareImage(context.Background(), []byte("not image"), "image/png", ImageLimits{}); e == nil {
		t.Fatal("invalid accepted")
	}
	var b bytes.Buffer
	png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	if _, e := PrepareImage(context.Background(), b.Bytes(), "image/jpeg", ImageLimits{}); e == nil {
		t.Fatal("MIME mismatch")
	}
	if _, e := PrepareImage(context.Background(), b.Bytes(), "image/png", ImageLimits{MaxBase64: 1}); e == nil {
		t.Fatal("impossible budget accepted")
	}
	for orientation := 1; orientation <= 8; orientation++ {
		src := image.NewNRGBA(image.Rect(0, 0, 2, 3))
		src.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
		out := orientImage(src, orientation)
		w, h := 2, 3
		if orientation >= 5 {
			w, h = 3, 2
		}
		if out.Bounds().Dx() != w || out.Bounds().Dy() != h {
			t.Fatal(orientation, out.Bounds())
		}
	}
}
