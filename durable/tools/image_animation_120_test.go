package tools

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"
)

func TestImage120AnimatedGIFPassThroughAndAPNGRejection(t *testing.T) {
	palette := color.Palette{color.Black, color.White}
	first := image.NewPaletted(image.Rect(0, 0, 3, 2), palette)
	second := image.NewPaletted(image.Rect(0, 0, 3, 2), palette)
	second.SetColorIndex(1, 0, 1)
	var encoded bytes.Buffer
	if e := gif.EncodeAll(&encoded, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{5, 5}}); e != nil {
		t.Fatal(e)
	}
	prepared, e := PrepareImage(context.Background(), encoded.Bytes(), "image/gif", ImageLimits{})
	if e != nil || prepared.MimeType != "image/gif" {
		t.Fatal(prepared, e)
	}
	animation, e := gif.DecodeAll(bytes.NewReader(prepared.Data))
	if e != nil || len(animation.Image) != 2 {
		t.Fatal("pass-through lost GIF frames", animation, e)
	}
	prepared, e = PrepareImage(context.Background(), encoded.Bytes(), "image/gif", ImageLimits{MaxWidth: 1, MaxHeight: 1, MaxBase64: 1024})
	if e != nil || prepared.Width != 1 || prepared.Height != 1 || prepared.MimeType != "image/png" {
		t.Fatal("GIF resize first-frame adaptation", prepared, e)
	}
	encoded.Reset()
	if e = png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 2, 2))); e != nil {
		t.Fatal(e)
	}
	data := encoded.Bytes()
	chunk := make([]byte, 20)
	binary.BigEndian.PutUint32(chunk, 8)
	copy(chunk[4:], "acTL")
	binary.BigEndian.PutUint32(chunk[8:], 2)
	binary.BigEndian.PutUint32(chunk[16:], crc32.ChecksumIEEE(chunk[4:16]))
	apng := append([]byte{}, data[:33]...)
	apng = append(apng, chunk...)
	apng = append(apng, data[33:]...)
	if _, e = PrepareImage(context.Background(), apng, "image/png", ImageLimits{}); e == nil {
		t.Fatal("APNG accepted")
	}
}
func BenchmarkImage120Lanczos(b *testing.B) {
	src := image.NewNRGBA(image.Rect(0, 0, 512, 384))
	for y := 0; y < 384; y++ {
		for x := 0; x < 512; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: uint8(x + y), A: 255})
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, e := resizeLanczos3(context.Background(), src, 256, 192); e != nil {
			b.Fatal(e)
		}
	}
}
