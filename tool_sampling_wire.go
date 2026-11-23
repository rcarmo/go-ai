package goai

import (
	"bytes"
	"encoding/json"
	"errors"
)

// ToolConstrainedSamplingDisabled represents an explicit JSON false. It
// disables constrained sampling while preserving the declaration's presence.
const ToolConstrainedSamplingDisabled = "disabled"

func (s ToolConstrainedSampling) MarshalJSON() ([]byte, error) {
	if s.Type == ToolConstrainedSamplingDisabled {
		if s.Strict != "" || len(s.Variants) != 0 {
			return nil, errors.New("disabled constrained sampling cannot specify a schema or grammar")
		}
		return []byte("false"), nil
	}
	type object ToolConstrainedSampling
	return json.Marshal(object(s))
}

func (s *ToolConstrainedSampling) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("false")) {
		*s = ToolConstrainedSampling{Type: ToolConstrainedSamplingDisabled}
		return nil
	}
	type object ToolConstrainedSampling
	return json.Unmarshal(data, (*object)(s))
}
