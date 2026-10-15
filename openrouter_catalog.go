package goai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PersonalizeOpenRouterCatalog filters only the supplied chat catalog for this
// credential. The caller owns persistence in its credential-scoped store; never
// write per-key results into the global provider registry.
func PersonalizeOpenRouterCatalog(ctx context.Context, client *http.Client, baseURL, key string, catalog []*Model) ([]*Model, error) {
	if ctx == nil {
		return nil, fmt.Errorf("nil catalog context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if key == "" {
		return cloneModels(catalog), nil
	}
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	if client == nil {
		client = http.DefaultClient
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(bounded, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404 {
		return cloneModels(catalog), nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OpenRouter model list request failed: %d", resp.StatusCode)
	}
	bytes, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(bytes) > 8<<20 {
		return nil, fmt.Errorf("OpenRouter model list response exceeds limit")
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(bytes, &envelope) != nil {
		return nil, fmt.Errorf("invalid OpenRouter model list response")
	}
	data, ok := envelope["data"]
	if !ok || len(data) == 0 || data[0] != '[' {
		return nil, fmt.Errorf("invalid OpenRouter model list response")
	}
	var entries []json.RawMessage
	if err = json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("invalid OpenRouter model list response")
	}
	allowed := map[string]openRouterCatalogEntry{}
	for _, raw := range entries {
		var e openRouterCatalogEntry
		if json.Unmarshal(raw, &e) == nil && e.ID != "" {
			allowed[e.ID] = e
		}
	}
	out := make([]*Model, 0, len(catalog))
	for _, model := range catalog {
		if model == nil {
			continue
		}
		e, ok := allowed[model.ID]
		if !ok {
			continue
		}
		owned := cloneModel(model)
		if e.Top.Context > 0 {
			owned.ContextWindow = e.Top.Context
		} else if e.Context > 0 {
			owned.ContextWindow = e.Context
		}
		if e.Top.MaxTokens > 0 {
			owned.MaxTokens = e.Top.MaxTokens
		}
		if e.Pricing != nil {
			owned.Cost = openRouterCatalogCost(e.Pricing)
		}
		out = append(out, owned)
	}
	return out, nil
}

type openRouterCatalogEntry struct {
	ID      string `json:"id"`
	Context int    `json:"context_length"`
	Top     struct {
		Context   int `json:"context_length"`
		MaxTokens int `json:"max_completion_tokens"`
	} `json:"top_provider"`
	Pricing *openRouterPricing `json:"pricing"`
}
type openRouterRates struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
	CacheRead  string `json:"input_cache_read"`
	CacheWrite string `json:"input_cache_write"`
}
type openRouterPricing struct {
	openRouterRates
	Overrides []struct {
		Min   *int            `json:"min_prompt_tokens"`
		Start json.RawMessage `json:"utc_start"`
		End   json.RawMessage `json:"utc_end"`
		Days  json.RawMessage `json:"utc_days"`
		openRouterRates
	} `json:"pricing_overrides"`
}

func openRouterRate(value string, fallback float64) float64 {
	if value == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return fallback
	}
	return math.Round(n*1e12) / 1e6
}
func openRouterCatalogCost(p *openRouterPricing) ModelCost {
	cost := ModelCost{Input: openRouterRate(p.Prompt, 0), Output: openRouterRate(p.Completion, 0), CacheRead: openRouterRate(p.CacheRead, 0), CacheWrite: openRouterRate(p.CacheWrite, 0)}
	for _, o := range p.Overrides {
		if o.Min == nil || *o.Min <= 0 || o.Start != nil || o.End != nil || o.Days != nil {
			continue
		}
		cost.Tiers = append(cost.Tiers, ModelCostTier{InputTokensAbove: *o.Min, Input: openRouterRate(o.Prompt, cost.Input), Output: openRouterRate(o.Completion, cost.Output), CacheRead: openRouterRate(o.CacheRead, cost.CacheRead), CacheWrite: openRouterRate(o.CacheWrite, cost.CacheWrite)})
	}
	return cost
}
