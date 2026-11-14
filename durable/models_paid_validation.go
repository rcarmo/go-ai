package durable

import (
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
)

// Validate required image fields before typed decoding collapses missing/null
// string fields to their zero values. Empty strings remain source-valid.
func validatePaidClassifierImages(context map[string]any) error {
	value, present := context["images"]
	if !present {
		return nil
	}
	images, ok := value.([]any)
	if !ok {
		return reject("classifier images must be an array")
	}
	for _, value := range images {
		image, ok := value.(map[string]any)
		if !ok || image["type"] != "image" {
			return reject("invalid classifier image block")
		}
		if _, ok := image["data"].(string); !ok {
			return reject("classifier image data must be a string")
		}
		if _, ok := image["mimeType"].(string); !ok {
			return reject("classifier image mimeType must be a string")
		}
	}
	return nil
}

func validatePaidClassifierContext(input goai.ClassifierContext) error {
	if input.State == nil || len(input.Questions) == 0 {
		return reject("classifier state/nonempty questions required")
	}
	for _, question := range input.Questions {
		// IDs, instructions and criterion meanings are strings in source;
		// empty strings are valid. Only the criteria collection is nonempty.
		switch question.Type {
		case "choice":
			if choices, ok := question.Criteria.(map[string]string); !ok || len(choices) == 0 {
				return reject("classifier choice criteria required")
			}
		case "score":
			if scores, ok := question.Criteria.([]string); !ok || len(scores) == 0 {
				return reject("classifier score criteria required")
			}
		case "bool":
			if _, ok := question.Criteria.(goai.ClassifierBoolCriteria); !ok {
				return reject("classifier bool criteria required")
			}
		default:
			return reject("invalid classifier question type")
		}
	}
	return nil
}
func validatePaidImageContext(input images.ImagesContext) error {
	if len(input.Input) == 0 {
		return reject("nonempty image input required")
	}
	for _, item := range input.Input {
		switch item.Type {
		case "text":
			if item.Text == "" {
				return reject("image text prompt required")
			}
		case "image":
			if item.Data == "" || item.MimeType == "" {
				return reject("image data/mimeType required")
			}
		default:
			return reject("invalid image input type")
		}
	}
	return nil
}
