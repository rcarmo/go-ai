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

// Source checkImagesContext checks required field types on the original
// object; typed zero values alone cannot distinguish missing from empty.
func validatePaidImageInput(context map[string]any) error {
	input, ok := context["input"].([]any)
	if !ok || len(input) == 0 {
		return reject("image input must be a nonempty array")
	}
	for _, value := range input {
		block, ok := value.(map[string]any)
		if !ok {
			return reject("invalid image input block")
		}
		switch block["type"] {
		case "text":
			if _, ok := block["text"].(string); !ok {
				return reject("image text must be a string")
			}
		case "image":
			if _, ok := block["data"].(string); !ok {
				return reject("image data must be a string")
			}
			if _, ok := block["mimeType"].(string); !ok {
				return reject("image mimeType must be a string")
			}
		default:
			return reject("invalid image input type")
		}
	}
	return nil
}

func validatePaidImageContext(input images.ImagesContext) error {
	if len(input.Input) == 0 {
		return reject("nonempty image input required")
	}
	for _, item := range input.Input {
		if item.Type != "text" && item.Type != "image" {
			return reject("invalid image input type")
		}
	}
	return nil
}
