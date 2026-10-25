package goai

import (
	"fmt"
	"strings"
)

func systemOneImageCompat(model *ClassifierModel) (input string, optIn bool) {
	input, optIn = "assets", model.Api == ClassifierApiTypeSafeSystemOne
	if model.Api == ClassifierApiCloudflareWorkersAI {
		if strings.HasPrefix(model.ID, "@cf/cloudflare/clef") {
			input = "images"
		}
	} else if model.Provider == ClassifierProviderOpenRouter || strings.Contains(model.BaseURL, "openrouter.ai") {
		input, optIn = "state-image-urls", false
	}
	if model.Compat != nil {
		if model.Compat.ImageInput != "" {
			input = model.Compat.ImageInput
		}
		if model.Compat.SendImageOptInHeader != nil {
			optIn = *model.Compat.SendImageOptInHeader
		}
	}
	return
}

func addSystemOneImages(label string, model *ClassifierModel, input string, images []ImageContent, wire map[string]any) error {
	acceptsImages := false
	for _, modality := range model.Input {
		acceptsImages = acceptsImages || modality == "image"
	}
	if !acceptsImages {
		return fmt.Errorf("%s does not support image input for %s", label, model.ID)
	}
	switch input {
	case "images":
		if len(images) > 4 {
			return fmt.Errorf("%s accepts at most 4 images for %s, got %d", label, model.ID, len(images))
		}
		urls := make([]string, 0, len(images))
		for _, image := range images {
			if image.MimeType != "image/png" && image.MimeType != "image/jpeg" && image.MimeType != "image/webp" {
				return fmt.Errorf("%s accepts PNG, JPEG, and WebP images for %s, got %s", label, model.ID, image.MimeType)
			}
			urls = append(urls, "data:"+image.MimeType+";base64,"+image.Data)
		}
		wire["images"] = urls
	case "state-image-urls":
		parts := make([]any, 0, len(images)+1)
		for _, image := range images {
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + image.MimeType + ";base64," + image.Data}})
		}
		wire["state"] = append(parts, wire["state"])
	case "assets":
		assets := map[string]any{}
		state := make([]any, 0, len(images)+1)
		for i, image := range images {
			format := ""
			switch image.MimeType {
			case "image/png":
				format = "png"
			case "image/jpeg":
				format = "jpeg"
			}
			if format == "" {
				return fmt.Errorf("%s accepts PNG and JPEG images for %s, got %s", label, model.ID, image.MimeType)
			}
			name := fmt.Sprintf("<image-%d>", i+1)
			assets[name] = map[string]any{"type": "image", "format": format, "data_base64": image.Data}
			state = append(state, name)
		}
		wire["assets"], wire["state"] = assets, append(state, wire["state"])
	default:
		return fmt.Errorf("unsupported System One image input: %s", input)
	}
	return nil
}
