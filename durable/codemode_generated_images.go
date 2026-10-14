package durable

import (
	"encoding/json"
	"fmt"
)

// Pinned output.ts warns only when no image was displayed, not for each omitted
// image. Count confirmed deliveries so reopen does not redispatch paid calls.
func codemodeUnshownImageNotice(deliveries []CodemodeDelivery, shown int) string {
	if shown > 0 {
		return ""
	}
	count := 0
	for _, delivery := range deliveries {
		if !delivery.Call.Global || delivery.Call.Name != "models.generateImages" || !delivery.HasValue {
			continue
		}
		data, err := json.Marshal(delivery.Value)
		if err != nil {
			continue
		}
		var value struct {
			Output []struct {
				Type string `json:"type"`
			} `json:"output"`
		}
		if json.Unmarshal(data, &value) != nil {
			continue
		}
		for _, block := range value.Output {
			if block.Type == "image" {
				count++
			}
		}
	}
	if count == 0 {
		return ""
	}
	plural := ""
	if count != 1 {
		plural = "s"
	}
	return fmt.Sprintf("Note: models.generateImages() returned %d image%s that the script did not show. Show each image block of result.output with image(block).", count, plural)
}
