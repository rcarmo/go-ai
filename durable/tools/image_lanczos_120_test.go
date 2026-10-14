package tools

import (
	"context"
	"errors"
	"image"
	"image/color"
	"math"
	"testing"
)

// Independent direct 2D float64 convolution avoids production's separable
// float32 scratch. Acceptance is channel error, never generated-byte equality.
func directLanczosReference(src image.Image, w, h int) *image.NRGBA {
	b := src.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	sx, sy := math.Max(1, float64(b.Dx())/float64(w)), math.Max(1, float64(b.Dy())/float64(h))
	kernel := func(v float64) float64 {
		if v == 0 {
			return 1
		}
		if math.Abs(v) >= 3 {
			return 0
		}
		return 3 * math.Sin(math.Pi*v) * math.Sin(math.Pi*v/3) / (math.Pi * math.Pi * v * v)
	}
	for y := 0; y < h; y++ {
		cy := (float64(y)+.5)*float64(b.Dy())/float64(h) - .5
		for x := 0; x < w; x++ {
			cx := (float64(x)+.5)*float64(b.Dx())/float64(w) - .5
			var channels [4]float64
			normal := 0.0
			for yy := 0; yy < b.Dy(); yy++ {
				wy := kernel((float64(yy) - cy) / sy)
				for xx := 0; xx < b.Dx(); xx++ {
					weight := wy * kernel((float64(xx)-cx)/sx)
					if weight == 0 {
						continue
					}
					c := color.NRGBAModel.Convert(src.At(b.Min.X+xx, b.Min.Y+yy)).(color.NRGBA)
					channels[0] += float64(c.R) * float64(c.A) / 255 * weight
					channels[1] += float64(c.G) * float64(c.A) / 255 * weight
					channels[2] += float64(c.B) * float64(c.A) / 255 * weight
					channels[3] += float64(c.A) * weight
					normal += weight
				}
			}
			for i := range channels {
				channels[i] /= normal
			}
			clamp := func(v float64) uint8 { return uint8(math.Round(math.Max(0, math.Min(255, v)))) }
			a := math.Max(0, math.Min(255, channels[3]))
			c := color.NRGBA{A: clamp(a)}
			if a > 0 {
				c.R = clamp(channels[0] * 255 / a)
				c.G = clamp(channels[1] * 255 / a)
				c.B = clamp(channels[2] * 255 / a)
			}
			out.SetNRGBA(x, y, c)
		}
	}
	return out
}
func TestImage120LanczosNumericalDistributionAndAlphaEdges(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 19, 13))
	for y := 0; y < 13; y++ {
		for x := 0; x < 19; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 13), G: uint8(y * 19), B: uint8((x + y) * 8), A: uint8(40 + (x*7+y*11)%216)})
		}
	}
	for _, size := range [][2]int{{7, 5}, {12, 9}, {19, 13}} {
		got, e := resizeLanczos3(context.Background(), src, size[0], size[1])
		if e != nil {
			t.Fatal(e)
		}
		want := directLanczosReference(src, size[0], size[1])
		maxError, total, samples := 0, 0, 0
		for y := 0; y < size[1]; y++ {
			for x := 0; x < size[0]; x++ {
				a, b := got.NRGBAAt(x, y), want.NRGBAAt(x, y)
				for _, pair := range [][2]uint8{{a.R, b.R}, {a.G, b.G}, {a.B, b.B}, {a.A, b.A}} {
					delta := int(pair[0]) - int(pair[1])
					if delta < 0 {
						delta = -delta
					}
					maxError = max(maxError, delta)
					total += delta
					samples++
				}
			}
		}
		if maxError > 1 || float64(total)/float64(samples) > .05 {
			t.Fatal("float32 reduction error", size, maxError, float64(total)/float64(samples))
		}
		t.Logf("%v channel max=%d mean=%.6f", size, maxError, float64(total)/float64(samples))
	}
	edge := image.NewNRGBA(image.Rect(0, 0, 16, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 16; x++ {
			a := uint8(0)
			if x >= 8 {
				a = 255
			}
			edge.SetNRGBA(x, y, color.NRGBA{R: 255, A: a})
		}
	}
	got, e := resizeLanczos3(context.Background(), edge, 6, 2)
	if e != nil {
		t.Fatal(e)
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 6; x++ {
			c := got.NRGBAAt(x, y)
			if c.A > 0 && c.R < 254 {
				t.Fatal("dark alpha fringe", c)
			}
		}
	}
}
func TestImage120LanczosCancellationAndScratchBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := resizeLanczos3(ctx, image.NewNRGBA(image.Rect(0, 0, 2, 2)), 1, 1); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	huge := image.NewUniform(color.White)
	if _, e := resizeLanczos3(context.Background(), huge, 2000, 2000); e == nil {
		t.Fatal("unbounded scratch accepted")
	}
}
