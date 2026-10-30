package responsestream

import (
	goai "github.com/rcarmo/go-ai"
	"strings"
)

// ToolResultOutput follows Responses' text/image union after message transforms.
func ToolResultOutput(model *goai.Model, content []goai.ContentBlock) any {
	var texts []string
	images := 0
	for _, block := range content {
		switch block.Type {
		case "text":
			texts = append(texts, block.Text)
		case "image":
			images++
		}
	}
	text := strings.Join(texts, "\n")
	// ECMAScript trim includes BOM but excludes NEL (unlike Go TrimSpace).
	hasText := strings.TrimFunc(text, func(r rune) bool {
		return r >= 0x9 && r <= 0xd || r >= 0x2000 && r <= 0x200a || strings.ContainsRune(" \u00a0\u1680\u2028\u2029\u202f\u205f\u3000\ufeff", r)
	}) != ""
	supportsImages := false
	if model != nil {
		for _, kind := range model.Input {
			if kind == "image" {
				supportsImages = true
				break
			}
		}
	}
	if images == 0 || !supportsImages {
		if hasText {
			return goai.SanitizeSurrogates(text)
		}
		if images > 0 {
			return "(see attached image)"
		}
		return "(no tool output)"
	}
	output := make([]any, 0, images+1)
	if hasText {
		output = append(output, map[string]any{"type": "input_text", "text": goai.SanitizeSurrogates(text)})
	}
	for _, block := range content {
		if block.Type == "image" {
			output = append(output, map[string]any{"type": "input_image", "detail": "auto", "image_url": "data:" + block.MimeType + ";base64," + block.Data})
		}
	}
	return output
}
