package openaicodex

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/responsestream"
)

func finalizeCodexResponse(raw []byte, partial *goai.Message, model *goai.Model, reasoning responsestream.ReasoningIndex) error {
	var response struct {
		ID         string `json:"id"`
		Status     string `json:"status"`
		EndTurn    *bool  `json:"end_turn"`
		Incomplete *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Output []json.RawMessage `json:"output"`
		Usage  *struct {
			Input        int `json:"input_tokens"`
			Output       int `json:"output_tokens"`
			Total        int `json:"total_tokens"`
			InputDetails *struct {
				Cached int `json:"cached_tokens"`
				Write  int `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
			OutputDetails *struct {
				Reasoning *int `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return err
	}
	if response.ID != "" {
		partial.ResponseID = response.ID
	}
	if response.EndTurn != nil {
		partial.EndTurn = response.EndTurn
	}
	reasoning.Backfill(partial.Content, response.Output)
	if usage := response.Usage; usage != nil {
		cached, write := 0, 0
		if usage.InputDetails != nil {
			cached, write = usage.InputDetails.Cached, usage.InputDetails.Write
		}
		input := usage.Input - cached - write
		if input < 0 {
			input = 0
		}
		partial.Usage = &goai.Usage{Input: input, Output: usage.Output, CacheRead: cached, CacheWrite: write, TotalTokens: usage.Total}
		if usage.OutputDetails != nil && usage.OutputDetails.Reasoning != nil {
			partial.Usage.Reasoning = *usage.OutputDetails.Reasoning
			partial.Usage.ReasoningSet = true
		}
		partial.Usage.Cost = goai.CalculateCost(model, partial.Usage)
	}
	reason := ""
	if response.Incomplete != nil {
		reason = response.Incomplete.Reason
	}
	partial.RawStopReason = response.Status
	if reason != "" {
		partial.RawStopReason += "." + reason
	}
	var err error
	partial.StopReason, partial.ErrorMessage, err = responsestream.TerminalStatus(response.Status, reason)
	if err != nil {
		return err
	}
	if partial.StopReason == goai.StopReasonStop {
		for _, block := range partial.Content {
			if block.Type == "toolCall" {
				partial.StopReason = goai.StopReasonToolUse
				break
			}
		}
	}
	return nil
}
