package tools

import (
	"context"
	"errors"
	"image"
	"image/color"
	"math"
)

const maxResizeScratchBytes = 64 << 20

type imageTap struct {
	index  int
	weight float64
}

func lanczos3(x float64) float64 {
	x = math.Abs(x)
	if x == 0 {
		return 1
	}
	if x >= 3 {
		return 0
	}
	p := math.Pi * x
	return math.Sin(p) / p * math.Sin(p/3) / (p / 3)
}
func imageWeights(ctx context.Context, source, target int) ([][]imageTap, error) {
	out := make([][]imageTap, target)
	scale := math.Max(1, float64(source)/float64(target))
	for i := range out {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		center := (float64(i)+.5)*float64(source)/float64(target) - .5
		left := max(0, int(math.Ceil(center-3*scale)))
		right := min(source-1, int(math.Floor(center+3*scale)))
		sum := 0.0
		for j := left; j <= right; j++ {
			weight := lanczos3((float64(j) - center) / scale)
			if weight != 0 {
				out[i] = append(out[i], imageTap{j, weight})
				sum += weight
			}
		}
		if sum == 0 {
			return nil, errors.New("empty image resampling kernel")
		}
		for j := range out[i] {
			out[i][j].weight /= sum
		}
	}
	return out, nil
}

// Separable, antialiased Lanczos3 in premultiplied alpha. Float32 intermediate
// storage limits scratch memory; final channel conversion clamps ringing and
// rounds once. Reject unreasonable scratch before allocating any coefficient
// tables. Decoder/output allocations remain governed by MaxImagePixels.
func resizeLanczos3(ctx context.Context, src image.Image, w, h int) (*image.NRGBA, error) {
	if ctx == nil {
		return nil, errors.New("nil resize context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if w < 1 || h < 1 || sw < 1 || sh < 1 || w > MaxImagePixels || h > MaxImagePixels || sw > MaxImagePixels || sh > MaxImagePixels || int64(sw)*int64(sh) > MaxImagePixels || int64(w)*int64(h) > MaxImagePixels {
		return nil, errors.New("invalid resize dimensions")
	}
	// Horizontal scratch plus worst-case source-sized tap sets for each axis.
	scratch := int64(w)*int64(sh)*16 + int64(sw+sh)*256 + int64(w+h)*96
	if scratch > maxResizeScratchBytes {
		return nil, errors.New("image resize exceeds scratch budget")
	}
	xs, err := imageWeights(ctx, sw, w)
	if err != nil {
		return nil, err
	}
	ys, err := imageWeights(ctx, sh, h)
	if err != nil {
		return nil, err
	}
	intermediate := make([]float32, w*sh*4)
	// Concrete colour access avoids boxing one color.Color per kernel tap.
	nrgba, _ := src.(*image.NRGBA)
	rgba, _ := src.(*image.RGBA)
	for y := 0; y < sh; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x, taps := range xs {
			var channels [4]float64
			for _, tap := range taps {
				var c color.NRGBA
				if nrgba != nil {
					c = nrgba.NRGBAAt(b.Min.X+tap.index, b.Min.Y+y)
				} else if rgba != nil {
					pixel := rgba.RGBAAt(b.Min.X+tap.index, b.Min.Y+y)
					c.A = pixel.A
					if pixel.A > 0 {
						c.R = uint8(uint32(pixel.R) * 255 / uint32(pixel.A))
						c.G = uint8(uint32(pixel.G) * 255 / uint32(pixel.A))
						c.B = uint8(uint32(pixel.B) * 255 / uint32(pixel.A))
					}
				} else {
					c = color.NRGBAModel.Convert(src.At(b.Min.X+tap.index, b.Min.Y+y)).(color.NRGBA)
				}
				a := float64(c.A) / 255
				channels[0] += float64(c.R) * a * tap.weight
				channels[1] += float64(c.G) * a * tap.weight
				channels[2] += float64(c.B) * a * tap.weight
				channels[3] += float64(c.A) * tap.weight
			}
			at := (y*w + x) * 4
			for k, v := range channels {
				intermediate[at+k] = float32(v)
			}
		}
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	clamp := func(v float64) uint8 { return uint8(math.Round(math.Max(0, math.Min(255, v)))) }
	for y, taps := range ys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := 0; x < w; x++ {
			var channels [4]float64
			for _, tap := range taps {
				at := (tap.index*w + x) * 4
				for k := range channels {
					channels[k] += float64(intermediate[at+k]) * tap.weight
				}
			}
			a := math.Max(0, math.Min(255, channels[3]))
			pixel := color.NRGBA{A: clamp(a)}
			if a > 0 {
				pixel.R = clamp(channels[0] * 255 / a)
				pixel.G = clamp(channels[1] * 255 / a)
				pixel.B = clamp(channels[2] * 255 / a)
			}
			out.SetNRGBA(x, y, pixel)
		}
	}
	return out, nil
}
