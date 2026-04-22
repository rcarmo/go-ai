// Azure OpenAI helpers — tool call history trimming and event adaptation.
//
// Azure OpenAI Responses API has stricter limits on function-call history
// than standard OpenAI. This module provides trimming helpers and event
// normalization for Azure-specific reasoning formats.
package goai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolCallLimitConfig controls tool-call history trimming for Azure.
type ToolCallLimitConfig struct {
	// Maximum number of tool call pairs to keep in history.
	Limit int
	// Maximum characters for the summary of removed tool calls.
	SummaryMax int
	// Maximum characters per tool output in the summary.
	OutputChars int
	// Maximum estimated tokens for the entire input.
	MaxEstimatedTokens int
}

// DefaultToolCallLimitConfig returns sensible defaults for Azure.
func DefaultToolCallLimitConfig() ToolCallLimitConfig {
	return ToolCallLimitConfig{
		Limit:              128,
		SummaryMax:         8000,
		OutputChars:        200,
		MaxEstimatedTokens: 0, // 0 = no budget trimming
	}
}

// ToolCallLimitResult describes what happened during trimming.
type ToolCallLimitResult struct {
	// The trimmed message list.
	Messages []interface{}
	// Counts
	ToolCallTotal         int
	ToolCallKept          int
	ToolCallRemoved       int
	ToolCallDeduped       int
	ToolCallBudgetRemoved int
	// Summary text inserted for removed calls.
	SummaryText string
	// Token estimates
	EstimatedTokensBefore int
	EstimatedTokensAfter  int
}

// ApplyToolCallLimit removes older tool-call/tool-result pairs from
// Responses API input messages to stay within Azure limits.
// Inserts a summary assistant message for removed calls.
func ApplyToolCallLimit(messages []interface{}, config ToolCallLimitConfig) *ToolCallLimitResult {
	if config.Limit <= 0 {
		config.Limit = 128
	}
	if config.SummaryMax <= 0 {
		config.SummaryMax = 8000
	}
	if config.OutputChars <= 0 {
		config.OutputChars = 200
	}

	result := &ToolCallLimitResult{
		Messages: messages,
	}

	// Find all function_call and function_call_output pairs
	type toolCallEntry struct {
		callIndex   int
		outputIndex int
		name        string
		output      string
	}
	var entries []toolCallEntry

	for i, raw := range messages {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		typ, _ := m["type"].(string)
		if typ == "function_call" {
			name, _ := m["name"].(string)
			entries = append(entries, toolCallEntry{callIndex: i, outputIndex: -1, name: name})
		}
		if typ == "function_call_output" {
			// Match to the most recent unpaired call
			for j := len(entries) - 1; j >= 0; j-- {
				if entries[j].outputIndex == -1 {
					entries[j].outputIndex = i
					output, _ := m["output"].(string)
					entries[j].output = output
					break
				}
			}
		}
	}

	result.ToolCallTotal = len(entries)

	if len(entries) <= config.Limit {
		result.ToolCallKept = len(entries)
		return result
	}

	// Remove oldest entries beyond the limit
	removeCount := len(entries) - config.Limit
	toRemove := make(map[int]bool)
	var summaryParts []string

	for i := 0; i < removeCount && i < len(entries); i++ {
		e := entries[i]
		toRemove[e.callIndex] = true
		if e.outputIndex >= 0 {
			toRemove[e.outputIndex] = true
		}

		// Build summary snippet
		outputSnippet := truncate(e.output, config.OutputChars)
		if outputSnippet == "" {
			outputSnippet = "(no output)"
		}
		summaryParts = append(summaryParts, fmt.Sprintf("- %s → %s", e.name, outputSnippet))
	}

	// Build trimmed messages
	var trimmed []interface{}
	summaryInserted := false

	for i, msg := range messages {
		if toRemove[i] {
			if !summaryInserted {
				// Insert summary before the first kept message
				summaryText := strings.Join(summaryParts, "\n")
				if len(summaryText) > config.SummaryMax {
					summaryText = summaryText[:config.SummaryMax] + "\n..."
				}
				result.SummaryText = summaryText
				trimmed = append(trimmed, map[string]interface{}{
					"type": "message",
					"role": "assistant",
					"content": []map[string]interface{}{
						{"type": "output_text", "text": "Earlier tool calls (summarized):\n" + summaryText},
					},
					"status": "completed",
				})
				summaryInserted = true
			}
			continue
		}
		trimmed = append(trimmed, msg)
	}

	result.Messages = trimmed
	result.ToolCallKept = config.Limit
	result.ToolCallRemoved = removeCount

	return result
}

// AzureSessionHeaders returns Azure-specific session correlation headers.
func AzureSessionHeaders(sessionID string) map[string]string {
	if sessionID == "" {
		return nil
	}
	return map[string]string{
		"session_id":              sessionID,
		"x-client-request-id":    sessionID,
		"x-ms-client-request-id": sessionID,
	}
}

func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

// --- Azure reasoning event normalization ---

// NormalizeAzureReasoningEvent adapts Azure-specific reasoning event types
// to the standard OpenAI Responses event protocol.
//
// Azure can emit reasoning content in three forms:
//   - reasoning_summary_* events (standard, already handled)
//   - reasoning_text parts inside "reasoning" output items
//   - "message" items with phase:"commentary"
//
// This function normalizes the latter two into the summary form.
func NormalizeAzureReasoningEvent(event map[string]interface{}) map[string]interface{} {
	typ, _ := event["type"].(string)

	switch typ {
	case "response.output_item.added":
		item, _ := event["item"].(map[string]interface{})
		if item == nil {
			return event
		}
		// Commentary messages → reasoning items
		if item["type"] == "message" && item["phase"] == "commentary" {
			clone := cloneMap(event)
			clone["item"] = map[string]interface{}{
				"id":      item["id"],
				"type":    "reasoning",
				"summary": []interface{}{},
			}
			return clone
		}

	case "response.content_part.added":
		part, _ := event["part"].(map[string]interface{})
		if part == nil {
			return event
		}
		// reasoning_text parts → reasoning_summary_part
		if part["type"] == "reasoning_text" || part["type"] == "output_text" {
			clone := cloneMap(event)
			clone["type"] = "response.reasoning_summary_part.added"
			clone["part"] = map[string]interface{}{
				"type": "summary_text",
				"text": part["text"],
			}
			return clone
		}

	case "response.reasoning_text.delta":
		clone := cloneMap(event)
		clone["type"] = "response.reasoning_summary_text.delta"
		return clone

	case "response.reasoning_text.done":
		clone := cloneMap(event)
		clone["type"] = "response.reasoning_summary_part.done"
		clone["part"] = map[string]interface{}{
			"type": "summary_text",
			"text": event["text"],
		}
		return clone

	case "response.output_item.done":
		item, _ := event["item"].(map[string]interface{})
		if item == nil {
			return event
		}
		// Adapt reasoning items that use content[] instead of summary[]
		if item["type"] == "reasoning" {
			if _, hasSummary := item["summary"]; !hasSummary {
				clone := cloneMap(event)
				clone["item"] = adaptReasoningItem(item)
				return clone
			}
		}
		// Commentary messages → reasoning items
		if item["type"] == "message" && item["phase"] == "commentary" {
			clone := cloneMap(event)
			clone["item"] = adaptCommentaryItem(item)
			return clone
		}
	}

	return event
}

func adaptReasoningItem(item map[string]interface{}) map[string]interface{} {
	content, _ := item["content"].([]interface{})
	var summary []interface{}
	for _, c := range content {
		part, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if part["type"] == "reasoning_text" {
			summary = append(summary, map[string]interface{}{
				"type": "summary_text",
				"text": part["text"],
			})
		}
	}
	result := cloneMap(item)
	result["summary"] = summary
	return result
}

func adaptCommentaryItem(item map[string]interface{}) map[string]interface{} {
	content, _ := item["content"].([]interface{})
	var summary []interface{}
	for _, c := range content {
		part, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if part["type"] == "output_text" {
			summary = append(summary, map[string]interface{}{
				"type": "summary_text",
				"text": part["text"],
			})
		}
	}
	return map[string]interface{}{
		"id":      item["id"],
		"type":    "reasoning",
		"summary": summary,
	}
}

func cloneMap(m map[string]interface{}) map[string]interface{} {
	data, _ := json.Marshal(m)
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	return result
}
