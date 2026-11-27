package goai

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

type usageJSONAlias Usage

// MarshalJSON preserves optional zero counters without changing the public
// integer fields. Nonzero counters are reported even without presence flags.
func (u Usage) MarshalJSON() ([]byte, error) {
	var long, reasoning, imageOutput *int
	if u.CacheWrite1hSet || u.CacheWrite1h != 0 {
		long = &u.CacheWrite1h
	}
	if u.ReasoningSet || u.Reasoning != 0 {
		reasoning = &u.Reasoning
	}
	if u.ImageOutputSet || u.ImageOutput != 0 {
		imageOutput = &u.ImageOutput
	}
	return json.Marshal(struct {
		usageJSONAlias
		CacheWrite1h *int `json:"cacheWrite1h,omitempty"`
		Reasoning    *int `json:"reasoning,omitempty"`
		ImageOutput  *int `json:"imageOutput,omitempty"`
	}{usageJSONAlias(u), long, reasoning, imageOutput})
}

// UnmarshalJSON retains optional-counter presence. Public JSON decoding keeps
// its usual unknown-field compatibility; strict native DTO decoding can query
// HasUnknownJSONFields to reject that input instead of bypassing its schema.
func (u *Usage) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil
	}
	next := *u
	var wire struct {
		*usageJSONAlias
		CacheWrite1h *int `json:"cacheWrite1h"`
		Reasoning    *int `json:"reasoning"`
		ImageOutput  *int `json:"imageOutput"`
	}
	wire.usageJSONAlias = (*usageJSONAlias)(&next)
	if next.CacheWrite1hSet || next.CacheWrite1h != 0 {
		value := next.CacheWrite1h
		wire.CacheWrite1h = &value
	}
	if next.ReasoningSet || next.Reasoning != 0 {
		value := next.Reasoning
		wire.Reasoning = &value
	}
	if next.ImageOutputSet || next.ImageOutput != 0 {
		value := next.ImageOutput
		wire.ImageOutput = &value
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&wire)
	if err == nil {
		var extra any
		if trailing := decoder.Decode(&extra); trailing != io.EOF {
			if trailing != nil {
				return trailing
			}
			return errors.New("trailing usage JSON")
		}
	}
	if err != nil {
		// Re-decode only the compatibility path. A successful permissive decode
		// records that strict schema validation must reject this input.
		if fallback := json.Unmarshal(data, &wire); fallback != nil {
			return fallback
		}
	}
	next.jsonUnknownFields = err != nil
	next.CacheWrite1hSet = wire.CacheWrite1h != nil
	next.ReasoningSet = wire.Reasoning != nil
	if wire.CacheWrite1h != nil {
		next.CacheWrite1h = *wire.CacheWrite1h
	}
	if wire.Reasoning != nil {
		next.Reasoning = *wire.Reasoning
	}
	next.ImageOutputSet = wire.ImageOutput != nil
	next.ImageOutput = 0
	if wire.ImageOutput != nil {
		next.ImageOutput = *wire.ImageOutput
	}
	*u = next
	return nil
}

// HasUnknownJSONFields is for strict DTO adapters. It includes unknown fields
// nested inside cost, which a custom JSON unmarshaler must not hide from them.
func (u Usage) HasUnknownJSONFields() bool { return u.jsonUnknownFields }
