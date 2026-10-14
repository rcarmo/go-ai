package tools

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"testing"
)

func TestImage120JPEGQualityLadder(t *testing.T) {
	for _, tc := range []struct {
		first int
		want  []int
	}{{80, []int{80, 85, 70, 55, 40}}, {85, []int{85, 70, 55, 40}}, {40, []int{40, 85, 70, 55}}, {100, []int{100, 85, 70, 55, 40}}} {
		got := imageJPEGQualities(tc.first)
		if len(got) != len(tc.want) {
			t.Fatal(tc, got)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatal(tc, got)
			}
		}
	}
}
func TestImage120JPEGQualityDecodedError(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 96, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 96; x++ {
			v := uint8((x*17 + y*31 + (x*y)%71) % 256)
			source.SetNRGBA(x, y, color.NRGBA{v, v, v, 255})
		}
	}
	var input bytes.Buffer
	if err := jpeg.Encode(&input, source, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(input.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	reference := directLanczosReference(decoded, 48, 32)
	errors := map[int]float64{}
	sizes := map[int]int{}
	for _, quality := range []int{20, 95} {
		result, err := PrepareImage(context.Background(), input.Bytes(), "image/jpeg", ImageLimits{MaxWidth: 48, MaxHeight: 32, MaxBase64: 100000, JPEGQuality: quality})
		if err != nil {
			t.Fatal(err)
		}
		if result.MimeType != "image/jpeg" || result.Width != 48 || result.Height != 32 {
			t.Fatal(result)
		}
		actual, err := jpeg.Decode(bytes.NewReader(result.Data))
		if err != nil {
			t.Fatal(err)
		}
		sum := 0.0
		for y := 0; y < 32; y++ {
			for x := 0; x < 48; x++ {
				r, _, _, _ := actual.At(x, y).RGBA()
				expected, _, _, _ := reference.At(x, y).RGBA()
				difference := float64(r)/257 - float64(expected)/257
				sum += difference * difference
			}
		}
		errors[quality] = math.Sqrt(sum / (48 * 32))
		sizes[quality] = len(result.Data)
	}
	t.Logf("decoded grayscale RMS low=%.3f high=%.3f bytes low=%d high=%d", errors[20], errors[95], sizes[20], sizes[95])
	if errors[95] >= errors[20] || errors[95] > 5 || errors[20] > 45 {
		t.Fatal("JPEG decoded quality contract", errors)
	}
	for _, quality := range []int{-1, 101} {
		if _, err := PrepareImage(context.Background(), input.Bytes(), "image/jpeg", ImageLimits{JPEGQuality: quality}); err == nil {
			t.Fatal("invalid quality accepted", quality)
		}
	}
}
