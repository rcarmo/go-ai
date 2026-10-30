package goai

import (
	"encoding/json"

	"github.com/rcarmo/go-ai/internal/jsonparse"
)

// MarshalJSON preserves tool-argument code units without changing text deltas.
func (e ToolCallDeltaEvent) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ContentIndex int
		Delta        jsonparse.ArgumentText
		Partial      *Message
	}{e.ContentIndex, jsonparse.ArgumentText(e.Delta), e.Partial})
}
func (e *ToolCallDeltaEvent) UnmarshalJSON(raw []byte) error {
	var wire struct {
		ContentIndex int
		Delta        jsonparse.ArgumentText
		Partial      *Message
	}
	wire.ContentIndex = e.ContentIndex
	wire.Delta = jsonparse.ArgumentText(e.Delta)
	wire.Partial = e.Partial
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	fields, err := streamingRawFields(raw)
	if err != nil {
		return err
	}
	if wire.Partial != nil {
		if encoded := streamingRawLookup(fields, "Partial"); len(encoded) > 0 {
			if err = RestoreStreamingArgumentsJSON(encoded, wire.Partial, true); err != nil {
				return err
			}
		}
	}
	e.ContentIndex = wire.ContentIndex
	e.Delta = string(wire.Delta)
	e.Partial = wire.Partial
	return nil
}
