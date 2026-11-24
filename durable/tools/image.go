package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"strings"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
)

const MaxImageBytes = 8 << 20
const MaxImagePixels = 40_000_000

// LoadImage validates raster dimensions before exposing a bounded inline image.
// PNG/JPEG/GIF use standard-library decoders. Unsupported formats reject.
func (e *Env) LoadImage(ctx context.Context, path string) (goai.ContentBlock, durable.JSON, error) {
	return e.loadImage(ctx, path, nil)
}
func (e *Env) LoadPreparedImage(ctx context.Context, path string, limits ImageLimits) (goai.ContentBlock, durable.JSON, error) {
	return e.loadImage(ctx, path, &limits)
}
func (e *Env) loadImage(ctx context.Context, path string, limits *ImageLimits) (goai.ContentBlock, durable.JSON, error) {
	if err := ctx.Err(); err != nil {
		return goai.ContentBlock{}, nil, err
	}
	absolute, err := e.resolve(path)
	if err != nil {
		return goai.ContentBlock{}, nil, err
	}
	file, err := os.Open(absolute)
	if err != nil {
		return goai.ContentBlock{}, nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxImageBytes+1))
	if err != nil {
		return goai.ContentBlock{}, nil, err
	}
	if len(data) > MaxImageBytes {
		return goai.ContentBlock{}, nil, errors.New("image exceeds byte limit")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return goai.ContentBlock{}, nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > MaxImagePixels {
		return goai.ContentBlock{}, nil, errors.New("image exceeds pixel limit")
	}
	if limits == nil {
		if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
			return goai.ContentBlock{}, nil, err
		}
	}
	mime := "image/" + format
	if format == "jpg" {
		mime = "image/jpeg"
	}
	if limits != nil {
		prepared, err := PrepareImage(ctx, data, mime, *limits)
		if err != nil {
			return goai.ContentBlock{}, nil, &imagePreparationError{mime: mime, cause: err}
		}
		details := durable.JSON{"path": path, "width": prepared.Width, "height": prepared.Height, "mimeType": prepared.MimeType}
		if prepared.ConvertedFrom != "" {
			details["convertedFrom"] = prepared.ConvertedFrom
		}
		if prepared.OriginalWidth != 0 {
			details["originalWidth"] = prepared.OriginalWidth
			details["originalHeight"] = prepared.OriginalHeight
		}
		return goai.ContentBlock{Type: "image", Data: base64.StdEncoding.EncodeToString(prepared.Data), MimeType: prepared.MimeType}, details, nil
	}
	return goai.ContentBlock{Type: "image", MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)}, durable.JSON{"path": path, "width": config.Width, "height": config.Height, "mimeType": mime}, nil
}
func isRasterImage(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	prefix := make([]byte, 16)
	n, _ := file.Read(prefix)
	prefix = prefix[:n]
	return (len(prefix) >= 12 && string(prefix[:4]) == "RIFF" && string(prefix[8:12]) == "WEBP") || bytes.HasPrefix(prefix, []byte("BM")) || bytes.HasPrefix(prefix, []byte{137, 80, 78, 71, 13, 10, 26, 10}) || bytes.HasPrefix(prefix, []byte{255, 216, 255}) || bytes.HasPrefix(prefix, []byte("GIF87a")) || bytes.HasPrefix(prefix, []byte("GIF89a"))
}

// LoadInlineImage reads the reference no-processor image path. It validates the
// model's inline limits and preserves supported bytes without recompression.
func (e *Env) LoadInlineImage(ctx context.Context, path string, limits ImageLimits) (goai.ContentBlock, durable.JSON, error) {
	absolute, err := e.resolve(path)
	if err != nil {
		return goai.ContentBlock{}, nil, err
	}
	file, err := os.Open(absolute)
	if err != nil {
		return goai.ContentBlock{}, nil, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return goai.ContentBlock{}, nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxImageBytes+1))
	if err != nil {
		return goai.ContentBlock{}, nil, err
	}
	if err = ctx.Err(); err != nil {
		return goai.ContentBlock{}, nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return goai.ContentBlock{}, nil, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return goai.ContentBlock{}, nil, errors.New("image changed during read")
	}
	return inlineImageBytesAt(ctx, path, data, limits)
}

func inlineImageBytes(ctx context.Context, data []byte, limits ImageLimits) (goai.ContentBlock, durable.JSON, error) {
	return inlineImageBytesAt(ctx, "image", data, limits)
}

const noImageProcessor = "and no image processor is configured to shrink it"

func inlineImageRejection(path, mime string, size int64, limits ImageLimits) error {
	if mime == "image/bmp" {
		return fmt.Errorf("%s is an image (%s) that needs converting, and no image processor is configured", path, mime)
	}
	// Avoid overflowing EncodedLen or allocating the image to find its size.
	if size > int64(limits.MaxBase64/4)*3 {
		label := fmt.Sprintf("%.1fMB", float64(size)/(1<<20))
		if size < 1<<20 {
			label = formatSize(int(size))
		}
		return fmt.Errorf("%s is an image (%s) of %s, too large to send, %s", path, mime, label, noImageProcessor)
	}
	if size > MaxImageBytes {
		return errors.New("image exceeds input byte limit")
	}
	return nil
}

func inlineImageBytesAt(ctx context.Context, path string, data []byte, limits ImageLimits) (goai.ContentBlock, durable.JSON, error) {
	if ctx == nil {
		return goai.ContentBlock{}, nil, errors.New("nil image context")
	}
	if err := ctx.Err(); err != nil {
		return goai.ContentBlock{}, nil, err
	}
	mime := detectImageMime(data)
	if mime == "" {
		return goai.ContentBlock{}, nil, errors.New("image requires an image processor")
	}
	if err := inlineImageRejection(path, mime, int64(len(data)), limits); err != nil {
		return goai.ContentBlock{}, nil, err
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return goai.ContentBlock{}, nil, err
	}
	if config.Width > limits.MaxWidth || config.Height > limits.MaxHeight {
		return goai.ContentBlock{}, nil, fmt.Errorf("%s is an image (%s) of %dx%d, larger than %dx%d, %s", path, mime, config.Width, config.Height, limits.MaxWidth, limits.MaxHeight, noImageProcessor)
	}
	if config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > MaxImagePixels {
		return goai.ContentBlock{}, nil, errors.New("image exceeds inline dimensions; supply an image processor")
	}
	return goai.ContentBlock{Type: "image", MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)}, durable.JSON{"width": config.Width, "height": config.Height, "mimeType": mime}, nil
}

func inlineReadResult(ctx context.Context, path string, data []byte, api *durable.ToolAPI) (durable.ToolResult, error) {
	limits, vision, err := invocationImageLimits(ctx, api)
	if err != nil {
		return durable.ToolResult{}, err
	}
	return inlineReadImageResult(ctx, path, data, limits, vision)
}

func imageReadFailure(err error) durable.ToolResult {
	return durable.ToolResult{Blocks: []goai.ContentBlock{}, IsError: true, Diagnostics: []durable.ToolDiagnostic{{Severity: "error", Code: "unsupported_image", Message: err.Error()}}}
}

func inlineReadImageResult(ctx context.Context, path string, data []byte, limits ImageLimits, vision bool) (durable.ToolResult, error) {
	block, details, err := inlineImageBytesAt(ctx, path, data, limits)
	if err != nil {
		if ctx.Err() != nil {
			return durable.ToolResult{}, ctx.Err()
		}
		return imageReadFailure(err), nil
	}
	return imageReadResult(block, details, vision), nil
}

type imagePreparationError struct {
	mime  string
	cause error
}

func (e *imagePreparationError) Error() string { return e.cause.Error() }
func (e *imagePreparationError) Unwrap() error { return e.cause }

// Retain images in the transcript even for text-only models. Provider message
// transformation supplies placeholders without discarding durable image data.
func imageReadResult(block goai.ContentBlock, details durable.JSON, vision bool) durable.ToolResult {
	message := fmt.Sprintf("Read image file [%s]", block.MimeType)
	prepared := PreparedImage{MimeType: block.MimeType}
	prepared.ConvertedFrom, _ = details["convertedFrom"].(string)
	prepared.OriginalWidth, _ = details["originalWidth"].(int)
	prepared.OriginalHeight, _ = details["originalHeight"].(int)
	prepared.Width, _ = details["width"].(int)
	prepared.Height, _ = details["height"].(int)
	notes := preparedImageNotes(prepared)
	if !vision {
		notes = append(notes, "Image output will be replaced with a text placeholder because the current model does not support image inputs.")
	}
	if len(notes) > 0 {
		message += ". " + strings.Join(notes, " ")
	}
	return durable.ToolResult{Blocks: []goai.ContentBlock{block}, Details: details, Diagnostics: []durable.ToolDiagnostic{{Severity: "info", Message: message}}}
}
