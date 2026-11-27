// Package openroutercost applies OpenRouter's billed totals after catalogue
// estimates. Raw accounting fields preserve missing/null and invalid-type
// distinctions from the upstream wire contract without rejecting SSE messages.
package openroutercost

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
)

type Accounting struct {
	Cost        json.RawMessage `json:"cost,omitempty"`
	IsBYOK      json.RawMessage `json:"is_byok,omitempty"`
	CostDetails json.RawMessage `json:"cost_details,omitempty"`
}

func IsModel(provider, baseURL string) bool {
	return provider == "openrouter" || strings.Contains(baseURL, "openrouter.ai")
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

// BilledTotal returns the authoritative total when the wire accounting is
// sufficient. server_tool_cost is already included in cost, so is not added.
func BilledTotal(reported Accounting) (float64, bool) {
	cost, ok := validCost(reported.Cost)
	if !ok {
		return 0, false
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
			return 0, false
		}
		upstream, valid := validCost(details.Total)
		if bytes.Equal(bytes.TrimSpace(details.Total), []byte("null")) {
			prompt, p := validCost(details.Prompt)
			completion, c := validCost(details.Completion)
			if !p || !c {
				return 0, false
			}
			upstream = prompt + completion
			valid = !math.IsInf(upstream, 0)
		}
		if !valid {
			return 0, false
		}
		cost += upstream
	default:
		return 0, false
	}
	return cost, true
}
