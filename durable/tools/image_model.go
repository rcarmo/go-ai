package tools

import (
	"context"
	"fmt"
	"github.com/rcarmo/go-ai/durable"
)

// Model limits override defaults field by field; an unresolved model defaults
// to accepting images, matching pinned read behaviour. Explicit host limits
// in ReadWithPreparedImages take precedence over model-derived dimensions.
func invocationImageLimits(ctx context.Context, api *durable.ToolAPI) (ImageLimits, bool, error) {
	limits := ImageLimits{MaxWidth: 2000, MaxHeight: 2000, MaxBase64: 9 << 19}
	if api == nil {
		return limits, true, nil
	}
	agent, err := api.Agent(ctx)
	if err != nil {
		return limits, false, err
	}
	access, err := api.Models()
	if err != nil {
		return limits, false, err
	}
	ref := agent.Configuration.Model
	model, err := access.GetModel(ref.Provider, ref.ID)
	if err != nil {
		return limits, false, err
	}
	if model == nil {
		return limits, true, nil
	}
	vision := false
	for _, kind := range model.Input {
		if kind == "image" {
			vision = true
		}
	}
	if model.InputLimits != nil && model.InputLimits.Images != nil && model.InputLimits.Images.Resize != nil {
		resize := model.InputLimits.Images.Resize
		limits.JPEGQuality = resize.JPEGQuality
		if limits.JPEGQuality < 0 || limits.JPEGQuality > 100 {
			return limits, vision, fmt.Errorf("invalid model image JPEG quality")
		}
		if resize.MaxWidthSet || resize.MaxWidth != 0 {
			limits.MaxWidth = resize.MaxWidth
		}
		if resize.MaxHeightSet || resize.MaxHeight != 0 {
			limits.MaxHeight = resize.MaxHeight
		}
		if resize.MaxBytesSet || resize.MaxBytes != 0 {
			limits.MaxBase64 = resize.MaxBytes
		}
		if limits.MaxWidth <= 0 || limits.MaxHeight <= 0 || limits.MaxBase64 <= 0 {
			return limits, vision, fmt.Errorf("invalid model image limits: dimensions and base64 budget must be positive")
		}
	}
	return limits, vision, nil
}
