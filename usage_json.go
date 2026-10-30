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
	var long, reasoning *int
	if u.CacheWrite1hSet || u.CacheWrite1h != 0 {
		long = &u.CacheWrite1h
	}
	if u.ReasoningSet || u.Reasoning != 0 {
		reasoning = &u.Reasoning
	}
	return json.Marshal(struct {
		usageJSONAlias
		CacheWrite1h *int `json:"cacheWrite1h,omitempty"`
		Reasoning    *int `json:"reasoning,omitempty"`
	}{usageJSONAlias(u), long, reasoning})
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
	*u = next
	return nil
}

// HasUnknownJSONFields is for strict DTO adapters. It includes unknown fields
// nested inside cost, which a custom JSON unmarshaler must not hide from them.
func (u Usage) HasUnknownJSONFields() bool { return u.jsonUnknownFields }
