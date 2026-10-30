// Package openroutercost applies OpenRouter's billed totals after catalogue
// estimates. Raw accounting fields preserve missing/null and invalid-type
// distinctions from the upstream wire contract without rejecting SSE messages.
package openroutercost

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"

	goai "github.com/rcarmo/go-ai"
)

type Accounting struct {
	Cost        json.RawMessage `json:"cost,omitempty"`
	IsBYOK      json.RawMessage `json:"is_byok,omitempty"`
	CostDetails json.RawMessage `json:"cost_details,omitempty"`
}

func IsModel(model *goai.Model) bool {
	return model != nil && (model.Provider == "openrouter" || strings.Contains(model.BaseURL, "openrouter.ai"))
}

func validCost(raw json.RawMessage) (float64, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, false
	}
	var value float64
	if json.Unmarshal(raw, &value) != nil {
		return 0, false
	}
	return value, !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

// Apply changes only the total and its provenance, never the catalogue's cost
// components. server_tool_cost is already included in cost, so is not added.
func Apply(usage *goai.Usage, reported Accounting) {
	if usage == nil {
		return
	}
	cost, ok := validCost(reported.Cost)
	if !ok {
		return
	}
	switch string(bytes.TrimSpace(reported.IsBYOK)) {
	case "false":
	case "true":
		var details struct {
			Total      json.RawMessage `json:"upstream_inference_cost"`
			Prompt     json.RawMessage `json:"upstream_inference_prompt_cost"`
			Completion json.RawMessage `json:"upstream_inference_completions_cost"`
		}
		if json.Unmarshal(reported.CostDetails, &details) != nil {
			return
		}
		upstream, valid := validCost(details.Total)
		if bytes.Equal(bytes.TrimSpace(details.Total), []byte("null")) {
			prompt, p := validCost(details.Prompt)
			completion, c := validCost(details.Completion)
			if !p || !c {
				return
			}
			upstream = prompt + completion
			valid = !math.IsInf(upstream, 0)
		}
		if !valid {
			return
		}
		cost += upstream
	default:
		return
	}
	usage.Cost.Total = cost
	fromProvider := true
	usage.Cost.TotalFromProvider = &fromProvider
}
