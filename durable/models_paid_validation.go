package durable

import (
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
)

func validatePaidClassifierContext(input goai.ClassifierContext) error {
	if input.State == nil || len(input.Questions) == 0 {
		return reject("classifier state/nonempty questions required")
	}
	for key, question := range input.Questions {
		if key == "" || question.Instructions == "" {
			return reject("classifier question name/instructions required")
		}
		switch question.Type {
		case "choice":
			if choices, ok := question.Criteria.(map[string]string); !ok || len(choices) == 0 {
				return reject("classifier choice criteria required")
			}
		case "score":
			if scores, ok := question.Criteria.([]string); !ok || len(scores) < 2 {
				return reject("classifier score criteria required")
			}
		case "bool":
			if criteria, ok := question.Criteria.(goai.ClassifierBoolCriteria); !ok || criteria.True == "" || criteria.False == "" {
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
