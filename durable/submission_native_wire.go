package durable

import (
	"bytes"
	"encoding/json"

	goai "github.com/rcarmo/go-ai"
)

func (s Submission) hasRequestID() bool { return s.RequestID != "" || s.RequestIDPresent }

// MarshalJSON retains the private native envelope. Presence uses the existing
// optional requestId string: old absent and nonempty encodings are unchanged.
// The bounded encoder recognises this exact library type and reads its fields;
// it never invokes this method or caller-owned marshalers during persistence.
func (s Submission) MarshalJSON() ([]byte, error) {
	l := maximumDurableLimits()
	return encodeBoundedOrder(s, l, l.MaxRecordBytes, true)
}

func (s *Submission) UnmarshalJSON(data []byte) error {
	var wire struct {
		ID           ID                     `json:"id"`
		Conversation ID                     `json:"conversation"`
		RequestID    json.RawMessage        `json:"requestId"`
		Type         string                 `json:"type"`
		Status       string                 `json:"status"`
		Value        JSON                   `json:"value"`
		Trace        *goai.TelemetryCarrier `json:"trace,omitempty"`
	}
	if err := decodePublicJSON(data, &wire); err != nil {
		return err
	}
	result := Submission{ID: wire.ID, Conversation: wire.Conversation, Type: wire.Type, Status: wire.Status, Value: wire.Value, Trace: wire.Trace}
	if len(wire.RequestID) != 0 {
		if bytes.Equal(bytes.TrimSpace(wire.RequestID), []byte("null")) {
			return reject("submission requestId must be a string")
		}
		if err := json.Unmarshal(wire.RequestID, &result.RequestID); err != nil {
			return err
		}
		result.RequestIDPresent = true
	}
	*s = result
	return nil
}
