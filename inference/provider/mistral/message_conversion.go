package mistral

import (
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/errortext"
	"strings"
)

func mistralTextPart(text string) mistralContentPart {
	return mistralContentPart{Type: "text", Text: &text}
}
func mistralImagePart(block goai.ContentBlock) mistralContentPart {
	return mistralContentPart{Type: "image_url", ImageURL: "data:" + block.MimeType + ";base64," + block.Data}
}

func mistralUserMessage(message goai.Message, supportsImages bool) (mistralMsg, bool) {
	var parts []mistralContentPart
	hadImages := false
	for _, block := range message.Content {
		switch block.Type {
		case "text":
			parts = append(parts, mistralTextPart(goai.SanitizeSurrogates(block.Text)))
		case "image":
			hadImages = true
			if supportsImages {
				parts = append(parts, mistralImagePart(block))
			}
		}
	}
	if len(parts) > 0 {
		return mistralMsg{Role: "user", Content: parts}, true
	}
	if hadImages && !supportsImages {
		return mistralMsg{Role: "user", Content: "(image omitted: model does not support images)"}, true
	}
	return mistralMsg{}, false
}

func mistralToolResultContent(message goai.Message, supportsImages bool) []mistralContentPart {
	var texts []string
	hasImages := false
	for _, block := range message.Content {
		if block.Type == "text" {
			texts = append(texts, goai.SanitizeSurrogates(block.Text))
		}
		hasImages = hasImages || block.Type == "image"
	}
	text := errortext.Trim(strings.Join(texts, "\n"))
	prefix := ""
	if message.IsError {
		prefix = "[tool error] "
	}
	switch {
	case text != "":
		if hasImages && !supportsImages {
			text += "\n[tool image omitted: model does not support images]"
		}
	case hasImages && supportsImages:
		text = "(see attached image)"
	case hasImages:
		text = "(image omitted: model does not support images)"
	default:
		text = "(no tool output)"
	}
	parts := []mistralContentPart{mistralTextPart(prefix + text)}
	if supportsImages {
		for _, block := range message.Content {
			if block.Type == "image" {
				parts = append(parts, mistralImagePart(block))
			}
		}
	}
	return parts
}
