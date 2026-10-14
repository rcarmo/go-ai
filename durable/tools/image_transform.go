package tools

import (
	"context"
	"encoding/binary"
	"image"
)

func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for pos := 2; pos+4 <= len(data); {
		if data[pos] != 0xff {
			return 1
		}
		marker := data[pos+1]
		pos += 2
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		size := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if size < 2 || size > len(data)-pos {
			return 1
		}
		segment := data[pos+2 : pos+size]
		pos += size
		if marker != 0xe1 || len(segment) < 14 || string(segment[:6]) != "Exif\x00\x00" {
			continue
		}
		return tiffOrientation(segment[6:])
	}
	return 1
}
func orientImage(src image.Image, orientation int) image.Image {
	return orientImageContext(context.Background(), src, orientation)
}
func orientImageContext(ctx context.Context, src image.Image, orientation int) image.Image {
	if orientation == 1 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	ow, oh := w, h
	if orientation >= 5 {
		ow, oh = h, w
	}
	out := image.NewNRGBA(image.Rect(0, 0, ow, oh))
	for y := 0; y < oh; y++ {
		if ctx.Err() != nil {
			return out
		}
		for x := 0; x < ow; x++ {
			sx, sy := x, y
			switch orientation {
			case 2:
				sx = w - 1 - x
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sy = h - 1 - y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			out.Set(x, y, src.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	return out
}
