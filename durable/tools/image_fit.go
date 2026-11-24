package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
)

// ImageProcessor returns nil when an image cannot be decoded or fitted. An
// error identifies a processor failure, which the omission note includes.
type ImageProcessor func(context.Context, []byte, string, ImageLimits) (*PreparedImage, error)

// FittedImage contains the prepared image or its omission note, never both.
type FittedImage struct {
	Image       *goai.ContentBlock
	Notes       []string
	OmittedNote string
}

// FittedImages is zero when the content would be stored unchanged.
type FittedImages struct {
	Changed bool
	Content []goai.ContentBlock
	Notes   []string
}

// NativeImageProcessor uses the pure-Go image decoder and resizer.
func NativeImageProcessor(ctx context.Context, bytes []byte, mime string, limits ImageLimits) (*PreparedImage, error) {
	if limits.MaxWidth <= 0 || limits.MaxHeight <= 0 || limits.MaxBase64 <= 0 {
		return nil, nil
	}
	image, err := PrepareImage(ctx, bytes, mime, limits)
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	if image.OriginalWidth != 0 {
		if orientation := imageOrientation(bytes); orientation >= 5 && orientation <= 8 {
			image.OriginalWidth, image.OriginalHeight = image.OriginalHeight, image.OriginalWidth
		}
		if image.Width == image.OriginalWidth && image.Height == image.OriginalHeight {
			image.OriginalWidth, image.OriginalHeight = 0, 0
		}
	}
	return &image, nil
}

// FitImage uses the MIME type identified from the bytes when available.
func FitImage(ctx context.Context, bytes []byte, mime string, limits ImageLimits, processor ImageProcessor) FittedImage {
	detected := detectImageMime(bytes)
	if detected == "" {
		detected = mime
	}
	if processor == nil {
		return omitFittedImage(mime, nil)
	}
	prepared, err := processor(ctx, bytes, detected, limits)
	if err != nil || prepared == nil {
		return omitFittedImage(mime, err)
	}
	image := goai.ContentBlock{Type: "image", Data: base64.StdEncoding.EncodeToString(prepared.Data), MimeType: prepared.MimeType}
	return FittedImage{Image: &image, Notes: preparedImageNotes(*prepared)}
}

func preparedImageNotes(prepared PreparedImage) []string {
	var notes []string
	if prepared.ConvertedFrom != "" {
		notes = append(notes, fmt.Sprintf("Converted from %s to %s.", prepared.ConvertedFrom, prepared.MimeType))
	}
	if prepared.OriginalWidth != 0 && prepared.Width > 0 {
		notes = append(notes, fmt.Sprintf("Resized from %dx%d to %dx%d. Multiply coordinates by %.2f to map them to the original.", prepared.OriginalWidth, prepared.OriginalHeight, prepared.Width, prepared.Height, float64(prepared.OriginalWidth)/float64(prepared.Width)))
	}
	return notes
}

func omitFittedImage(mime string, err error) FittedImage {
	note := fmt.Sprintf("An image (%s) cannot be prepared for the model and was left out", mime)
	if err != nil {
		note += ": " + err.Error()
	}
	return FittedImage{OmittedNote: note + "."}
}

// FitImages preserves text order and reports only changed or omitted images.
func FitImages(ctx context.Context, content []goai.ContentBlock, limits ImageLimits, processor ImageProcessor) FittedImages {
	result := FittedImages{Content: make([]goai.ContentBlock, 0, len(content))}
	for _, block := range content {
		if block.Type != "image" {
			result.Content = append(result.Content, block)
			continue
		}
		// atob ignores ASCII whitespace and accepts an unpadded final group.
		encoded := strings.Map(func(ch rune) rune {
			switch ch {
			case ' ', '\r', '\n', '\t', '\f':
				return -1
			}
			return ch
		}, block.Data)
		encoding := base64.StdEncoding
		if !strings.HasSuffix(encoded, "=") {
			encoding = base64.RawStdEncoding
		}
		bytes, err := encoding.DecodeString(encoded)
		fitted := FittedImage{}
		if err != nil {
			fitted = omitFittedImage(block.MimeType, err)
		} else {
			fitted = FitImage(ctx, bytes, block.MimeType, limits, processor)
		}
		switch {
		case fitted.Image == nil:
			result.Changed = true
			result.Notes = append(result.Notes, fitted.OmittedNote)
		case fitted.Image.Data == block.Data && fitted.Image.MimeType == block.MimeType:
			result.Content = append(result.Content, block)
		default:
			result.Changed = true
			result.Content = append(result.Content, *fitted.Image)
			result.Notes = append(result.Notes, fitted.Notes...)
		}
	}
	if !result.Changed {
		return FittedImages{}
	}
	return result
}

// CreateFitImagesExtension fits model-facing tool results before settlement.
func CreateFitImagesExtension(processor ImageProcessor) *durable.Extension {
	return &durable.Extension{Name: "fit-images", ToolHooks: durable.ToolHooks{
		AfterTool: func(ctx context.Context, _ goai.ToolCall, result durable.ToolResult, api *durable.HookAPI) (*durable.ToolResult, error) {
			var images bool
			for _, block := range result.Blocks {
				images = images || block.Type == "image"
			}
			if !images {
				return nil, nil
			}
			call, found, err := api.Task(ctx, api.TaskID())
			if err != nil || !found {
				return nil, err
			}
			if call.Input != nil {
				switch input := call.Input.Value.(type) {
				case durable.JSON:
					if input["kind"] == "nested" {
						return nil, nil
					}
				case map[string]any:
					if input["kind"] == "nested" {
						return nil, nil
					}
				}
			}
			model, err := fitConversationModel(ctx, api)
			if err != nil {
				return nil, err
			}
			fitted := FitImages(ctx, result.Blocks, fitModelLimits(model), processor)
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !fitted.Changed {
				return nil, nil
			}
			result.Blocks = fitted.Content
			if len(fitted.Notes) > 0 {
				result.Diagnostics = append(result.Diagnostics, durable.ToolDiagnostic{Severity: "info", Code: "image", Message: strings.Join(fitted.Notes, " ")})
			}
			return &result, nil
		},
	}}
}

func fitConversationModel(ctx context.Context, api *durable.HookAPI) (*goai.Model, error) {
	value, found, err := api.SnapshotDefinition(ctx, durable.AgentDoc(), api.ConversationID(), nil)
	if err != nil || !found {
		return nil, err
	}
	model, ok := value.Get("model")
	if !ok {
		return nil, nil
	}
	provider, providerOK := model.Get("provider")
	id, idOK := model.Get("modelId")
	if !providerOK || !idOK {
		return nil, nil
	}
	access, err := api.Models()
	if err != nil {
		return nil, err
	}
	return access.GetModel(goai.Provider(provider.String()), id.String())
}

func fitModelLimits(model *goai.Model) ImageLimits {
	limits := ImageLimits{MaxWidth: 2000, MaxHeight: 2000, MaxBase64: 9 << 19}
	if model != nil && model.InputLimits != nil && model.InputLimits.Images != nil {
		if resize := model.InputLimits.Images.Resize; resize != nil {
			if resize.MaxWidthSet || resize.MaxWidth != 0 {
				limits.MaxWidth = resize.MaxWidth
			}
			if resize.MaxHeightSet || resize.MaxHeight != 0 {
				limits.MaxHeight = resize.MaxHeight
			}
			if resize.MaxBytesSet || resize.MaxBytes != 0 {
				limits.MaxBase64 = resize.MaxBytes
			}
		}
	}
	return limits
}
