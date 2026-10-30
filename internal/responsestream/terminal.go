package responsestream

import (
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
)

func TerminalStatus(status, reason string) (goai.StopReason, string, error) {
	switch status {
	case "", "completed", "in_progress", "queued":
		return goai.StopReasonStop, "", nil
	case "incomplete":
		if reason == "max_output_tokens" {
			return goai.StopReasonLength, "", nil
		}
		message := "Response incomplete without a provider reason"
		if reason != "" {
			message = "Response incomplete: " + reason
		}
		return goai.StopReasonError, message, nil
	case "failed", "cancelled":
		return goai.StopReasonError, "", nil
	default:
		return goai.StopReasonError, "", fmt.Errorf("Unhandled stop reason: %s", status)
	}
}

// BackfillReasoning keeps the completed item intact and fills only missing or
// empty encryption fields from terminal output. Summary text is not replaced.
func BackfillReasoning(blocks []goai.ContentBlock, output []json.RawMessage) {
	type stored struct {
		index int
		item  map[string]json.RawMessage
	}
	byID := map[string]stored{}
	for i := range blocks {
		block := &blocks[i]
		if block.Type != "thinking" || block.ThinkingSignature == "" {
			continue
		}
		var item map[string]json.RawMessage
		if json.Unmarshal([]byte(block.ThinkingSignature), &item) != nil {
			continue
		}
		var id string
		if json.Unmarshal(item["id"], &id) == nil && id != "" {
			byID[id] = stored{i, item}
		}
	}
	for _, raw := range output {
		var item struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			Encrypted string `json:"encrypted_content"`
		}
		if json.Unmarshal(raw, &item) != nil || item.Type != "reasoning" || item.Encrypted == "" {
			continue
		}
		record, ok := byID[item.ID]
		if !ok {
			continue
		}
		if raw, exists := record.item["encrypted_content"]; exists {
			var existing *string
			if json.Unmarshal(raw, &existing) != nil || (existing != nil && *existing != "") {
				continue
			}
		}
		encoded, _ := json.Marshal(item.Encrypted)
		record.item["encrypted_content"] = encoded
		signature, err := json.Marshal(record.item)
		if err == nil {
			blocks[record.index].ThinkingSignature = string(signature)
		}
	}
}
