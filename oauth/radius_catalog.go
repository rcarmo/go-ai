package oauth

import (
	"encoding/json"
	"slices"

	goai "github.com/rcarmo/go-ai"
)

// RadiusGatewayImage and RadiusGatewayDecision are the typed /v1/config rows.
type RadiusGatewayImage struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Input  []string       `json:"input"`
	Output []string       `json:"output"`
	Cost   goai.ModelCost `json:"cost"`
}

type RadiusGatewayDecision struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Input         []string       `json:"input"`
	Cost          goai.ModelCost `json:"cost"`
	ContextWindow int            `json:"contextWindow"`
}

// Decode optional catalogs per row, so unsupported modalities and malformed
// typed rows do not turn a valid chat refresh into a failure.
func (cfg *RadiusGatewayConfig) UnmarshalJSON(data []byte) error {
	type config RadiusGatewayConfig
	var wire struct {
		config
		Images   json.RawMessage `json:"images"`
		Decision json.RawMessage `json:"decision"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*cfg = RadiusGatewayConfig(wire.config)
	for _, row := range radiusTypedRows(wire.Images) {
		if !slices.Contains(row.Output, any("image")) {
			continue
		}
		cfg.Images = append(cfg.Images, RadiusGatewayImage{
			ID: row.ID, Name: row.Name, Input: radiusModalities(row.Input), Output: radiusModalities(row.Output), Cost: *row.Cost,
		})
	}
	for _, row := range radiusTypedRows(wire.Decision) {
		if row.ContextWindow == nil {
			continue
		}
		var input []string
		for _, value := range row.Input {
			if modality, ok := value.(string); ok {
				input = append(input, modality)
			}
		}
		cfg.Decision = append(cfg.Decision, RadiusGatewayDecision{
			ID: row.ID, Name: row.Name, Input: input, Cost: *row.Cost, ContextWindow: *row.ContextWindow,
		})
	}
	return nil
}

type radiusTypedRow struct {
	ID            string
	Name          string
	Input         []any
	Output        []any
	Cost          *goai.ModelCost
	ContextWindow *int
}

func radiusTypedRows(data json.RawMessage) []radiusTypedRow {
	var encoded []json.RawMessage
	if json.Unmarshal(data, &encoded) != nil {
		return nil
	}
	out := make([]radiusTypedRow, 0, len(encoded))
	for _, raw := range encoded {
		var row radiusTypedRow
		if json.Unmarshal(raw, &row) == nil && row.ID != "" && row.Name != "" && row.Input != nil && row.Cost != nil {
			out = append(out, row)
		}
	}
	return out
}

func radiusModalities(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if s, ok := value.(string); ok && (s == "text" || s == "image") {
			out = append(out, s)
		}
	}
	return out
}

func cloneRadiusCost(cost goai.ModelCost) goai.ModelCost {
	if cost.ImageOutput != nil {
		cost.ImageOutput = new(*cost.ImageOutput)
	}
	cost.Tiers = slices.Clone(cost.Tiers)
	return cost
}

func cloneRadiusGatewayConfig(cfg *RadiusGatewayConfig) *RadiusGatewayConfig {
	out := *cfg
	out.Models = slices.Clone(cfg.Models)
	for i := range out.Models {
		m := &out.Models[i]
		m.Input = slices.Clone(m.Input)
		m.Cost = cloneRadiusCost(m.Cost)
		if m.ThinkingLevelMap != nil {
			levels := make(map[goai.ModelThinkingLevel]*string, len(m.ThinkingLevelMap))
			for level, value := range m.ThinkingLevelMap {
				if value != nil {
					text := *value
					levels[level] = &text
				} else {
					levels[level] = nil
				}
			}
			m.ThinkingLevelMap = levels
		}
	}
	out.Images = slices.Clone(cfg.Images)
	for i := range out.Images {
		m := &out.Images[i]
		m.Input, m.Output, m.Cost = slices.Clone(m.Input), slices.Clone(m.Output), cloneRadiusCost(m.Cost)
	}
	out.Decision = slices.Clone(cfg.Decision)
	for i := range out.Decision {
		m := &out.Decision[i]
		m.Input, m.Cost = slices.Clone(m.Input), cloneRadiusCost(m.Cost)
	}
	return &out
}

func (p *RadiusProvider) catalogFromGatewayConfig(cfg *RadiusGatewayConfig) *goai.ModelsStoreEntry {
	entry := &goai.ModelsStoreEntry{Models: p.modelsFromGatewayConfig(cfg), ImageModels: []*goai.ImageModel{}, ClassifierModels: []*goai.ClassifierModel{}}
	if cfg == nil {
		return entry
	}
	for _, m := range cfg.Images {
		if m.ID != "" && m.Name != "" && slices.Contains(m.Output, "image") {
			entry.ImageModels = append(entry.ImageModels, &goai.ImageModel{
				ID: m.ID, Type: "image", Name: m.Name, Api: goai.ImageApiPiImage, Provider: goai.ImageProvider(p.id),
				BaseURL: cfg.BaseURL, Input: slices.Clone(m.Input), Output: slices.Clone(m.Output), Cost: cloneRadiusCost(m.Cost),
			})
		}
	}
	for _, m := range cfg.Decision {
		if m.ID != "" && m.Name != "" {
			entry.ClassifierModels = append(entry.ClassifierModels, &goai.ClassifierModel{
				ID: m.ID, Type: "classifier", Name: m.Name, Api: goai.ClassifierApiPiDecision, Provider: goai.ClassifierProvider(p.id),
				BaseURL: cfg.BaseURL, Input: slices.Clone(m.Input), Cost: cloneRadiusCost(m.Cost), ContextWindow: m.ContextWindow,
			})
		}
	}
	return entry
}
