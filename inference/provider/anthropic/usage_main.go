package anthropic

import (
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/openroutercost"
)

// Pointers distinguish explicit zero from omitted/null proxy usage fields.
type anthropicUsage struct {
	openroutercost.Accounting
	Input         *int `json:"input_tokens"`
	Output        *int `json:"output_tokens"`
	CacheRead     *int `json:"cache_read_input_tokens"`
	CacheWrite    *int `json:"cache_creation_input_tokens"`
	CacheCreation *struct {
		Ephemeral1h *int `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
	OutputDetails *struct {
		Thinking *int `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

func applyAnthropicUsage(usage *goai.Usage, raw *anthropicUsage, model *goai.Model, initial bool) {
	if initial {
		usage.Input = 0
		usage.Output = 0
		usage.CacheRead = 0
		usage.CacheWrite = 0
		usage.CacheWrite1h = 0
		// Upstream initialises this optional property even without a TTL breakdown.
		usage.CacheWrite1hSet = true
	}
	if raw != nil {
		if raw.Input != nil {
			usage.Input = *raw.Input
		}
		if raw.Output != nil {
			usage.Output = *raw.Output
		}
		if raw.CacheRead != nil {
			usage.CacheRead = *raw.CacheRead
		}
		if raw.CacheWrite != nil {
			usage.CacheWrite = *raw.CacheWrite
		}
		if raw.CacheCreation != nil && raw.CacheCreation.Ephemeral1h != nil {
			usage.CacheWrite1h = *raw.CacheCreation.Ephemeral1h
			usage.CacheWrite1hSet = true
		}
		if !initial && raw.OutputDetails != nil && raw.OutputDetails.Thinking != nil {
			usage.Reasoning = *raw.OutputDetails.Thinking
			usage.ReasoningSet = true
		}
	}
	usage.TotalTokens = usage.Input + usage.Output + usage.CacheRead + usage.CacheWrite
	computeCosts(usage, model)
	// Upstream applies billed accounting on deltas, not message_start.
	if !initial && raw != nil && openroutercost.IsModel(model) {
		openroutercost.Apply(usage, raw.Accounting)
	}
}
