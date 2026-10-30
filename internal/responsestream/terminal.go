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

// ReasoningIndex follows provider item identity even when its latest block
// has not completed. It is stream-owned and never attached to snapshots.
type ReasoningIndex map[string]int

func (r *ReasoningIndex) Track(id string, index int) {
	if *r == nil {
		*r = ReasoningIndex{}
	}
	(*r)[id] = index
}

// Backfill keeps the completed item intact and fills only missing or empty
// encryption fields on the latest block for an ID. Summary text is preserved.
func (r ReasoningIndex) Backfill(blocks []goai.ContentBlock, output []json.RawMessage) {
	for _, raw := range output {
		var item struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			Encrypted string `json:"encrypted_content"`
		}
		if json.Unmarshal(raw, &item) != nil || item.Type != "reasoning" || item.Encrypted == "" {
			continue
		}
		index, ok := r[item.ID]
		if !ok || index < 0 || index >= len(blocks) {
			continue
		}
		block := &blocks[index]
		if block.Type != "thinking" || block.ThinkingSignature == "" {
			continue
		}
		var stored map[string]json.RawMessage
		if json.Unmarshal([]byte(block.ThinkingSignature), &stored) != nil || stored == nil {
			continue
		}
		if value, exists := stored["encrypted_content"]; exists {
			var existing *string
			if json.Unmarshal(value, &existing) != nil || (existing != nil && *existing != "") {
				continue
			}
		}
		encrypted, _ := json.Marshal(item.Encrypted)
		stored["encrypted_content"] = encrypted
		signature, err := json.Marshal(stored)
		if err == nil {
			block.ThinkingSignature = string(signature)
		}
	}
}
