package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
	"image"
	"image/jpeg"
	"image/png"
	"math"
)

// ImageLimits bounds dimensions and base64 characters (the upstream maxBase64
// contract), not raw encoded bytes. Zero chooses the default budget.
type ImageLimits struct {
	MaxWidth, MaxHeight, MaxBase64 int
	// JPEGQuality selects the first lossy encoding attempt (1..100).
	// Zero selects 80. Later attempts use the pinned 85/70/55/40 ladder.
	JPEGQuality int
}
type PreparedImage struct {
	Data                          []byte
	MimeType                      string
	Width, Height                 int
	ConvertedFrom                 string
	OriginalWidth, OriginalHeight int
}

func PrepareImage(ctx context.Context, data []byte, mimeType string, limits ImageLimits) (PreparedImage, error) {
	if ctx == nil {
		return PreparedImage{}, errors.New("nil image context")
	}
	if e := ctx.Err(); e != nil {
		return PreparedImage{}, e
	}
	if len(data) == 0 || len(data) > MaxImageBytes {
		return PreparedImage{}, errors.New("image input outside byte limit")
	}
	if limits.MaxWidth < 0 || limits.MaxHeight < 0 || limits.MaxBase64 < 0 {
		return PreparedImage{}, errors.New("negative image limit")
	}
	if limits.MaxWidth == 0 {
		limits.MaxWidth = 2000
	}
	if limits.MaxHeight == 0 {
		limits.MaxHeight = 2000
	}
	if limits.MaxBase64 == 0 {
		limits.MaxBase64 = 9 << 19
	}
	if limits.JPEGQuality < 0 || limits.JPEGQuality > 100 {
		return PreparedImage{}, errors.New("image JPEG quality outside 1..100")
	}
	if limits.JPEGQuality == 0 {
		limits.JPEGQuality = 80
	}
	if animatedPNG(data) {
		return PreparedImage{}, errors.New("animated PNG is not supported")
	}
	config, format, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > MaxImagePixels {
		return PreparedImage{}, errors.New("image cannot be decoded within pixel budget")
	}
	decoded, _, e := image.Decode(bytes.NewReader(data))
	if e != nil {
		return PreparedImage{}, errors.New("image cannot be decoded")
	}
	if e = ctx.Err(); e != nil {
		return PreparedImage{}, e
	}
	actual := "image/" + format
	if actual != mimeType {
		return PreparedImage{}, errors.New("image MIME mismatch")
	}
	orientation := imageOrientation(data)
	upright := orientImageContext(ctx, decoded, orientation)
	if e = ctx.Err(); e != nil {
		return PreparedImage{}, e
	}
	width, height := upright.Bounds().Dx(), upright.Bounds().Dy()
	result := PreparedImage{Width: width, Height: height, MimeType: actual}
	if orientation == 1 && width <= limits.MaxWidth && height <= limits.MaxHeight && base64.StdEncoding.EncodedLen(len(data)) <= limits.MaxBase64 && (format == "png" || format == "jpeg" || format == "gif" || format == "webp") {
		result.Data = append([]byte(nil), data...)
		return result, nil
	}
	scale := math.Min(1, math.Min(float64(limits.MaxWidth)/float64(width), float64(limits.MaxHeight)/float64(height)))
	w, h := max(1, int(math.Round(float64(width)*scale))), max(1, int(math.Round(float64(height)*scale)))
	for {
		if e := ctx.Err(); e != nil {
			return PreparedImage{}, e
		}
		raster, err := resizeLanczos3(ctx, upright, w, h)
		if err != nil {
			return PreparedImage{}, err
		}
		var encoded bytes.Buffer
		tryPNG := func() bool {
			encoded.Reset()
			if e := png.Encode(&encoded, raster); e != nil {
				return false
			}
			if base64.StdEncoding.EncodedLen(encoded.Len()) > limits.MaxBase64 {
				return false
			}
			result.MimeType = "image/png"
			return true
		}
		tryJPEG := func() bool {
			for _, quality := range imageJPEGQualities(limits.JPEGQuality) {
				encoded.Reset()
				if e := jpeg.Encode(&encoded, raster, &jpeg.Options{Quality: quality}); e != nil {
					return false
				}
				if base64.StdEncoding.EncodedLen(encoded.Len()) <= limits.MaxBase64 {
					result.MimeType = "image/jpeg"
					return true
				}
			}
			return false
		}
		accepted := false
		if format == "jpeg" {
			accepted = tryJPEG() || tryPNG()
		} else {
			accepted = tryPNG() || tryJPEG()
		}
		if accepted {
			result.Data = append([]byte(nil), encoded.Bytes()...)
			result.Width = w
			result.Height = h
			if result.MimeType != actual {
				result.ConvertedFrom = actual
			}
			if w != config.Width || h != config.Height || orientation != 1 {
				result.OriginalWidth = config.Width
				result.OriginalHeight = config.Height
			}
			return result, nil
		}
		if w == 1 && h == 1 {
			return PreparedImage{}, errors.New("image cannot fit base64 limit")
		}
		w = max(1, int(math.Floor(float64(w)*.75)))
		h = max(1, int(math.Floor(float64(h)*.75)))
	}
}
