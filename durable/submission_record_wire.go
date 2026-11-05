package durable

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
)

// MarshalJSON exports the original submission lifecycle. Compatibility Value
// is private native data and is never part of a public SubmissionRecord.
func (r SubmissionRecord) MarshalJSON() ([]byte, error) {
	if err := r.validateWire(); err != nil {
		return nil, err
	}
	var out orderedJSON
	out.field("id", r.ID)
	out.field("conversationId", r.Conversation)
	out.field("type", r.Type)
	out.field("status", r.Status)
	if r.RequestID != "" || r.wireRequestIDPresent {
		out.field("requestId", r.RequestID)
	}
	if r.Entry != 0 {
		out.field("entry", r.Entry)
	}
	if r.Answer != 0 {
		out.field("answer", r.Answer)
	}
	if r.Status == "unanswered" {
		out.field("reason", r.Reason)
	}
	if r.Detail != nil {
		out.field("detail", r.Detail.Value)
	}
	if r.Trace != nil {
		out.field("trace", *r.Trace)
	}
	return out.bytes()
}

func (r SubmissionRecord) validateWire() error {
	if r.ID == 0 || r.Conversation == 0 {
		return reject("submission identity")
	}
	if r.Type != "input" && r.Type != "write" && r.Type != "continue" {
		return reject("submission type")
	}
	if r.Detail != nil && (!r.Detail.Present || r.Status != "unanswered") {
		return reject("submission detail")
	}
	switch r.Status {
	case "queued":
		if r.Type == "continue" || r.Entry != 0 || r.Answer != 0 || r.Reason != "" {
			return reject("queued submission fields")
		}
	case "placed":
		if r.Type == "write" || (r.Type == "input") != (r.Entry != 0) || r.Answer != 0 || r.Reason != "" {
			return reject("placed submission fields")
		}
	case "done":
		if (r.Type != "continue") != (r.Entry != 0) || (r.Type != "write") != (r.Answer != 0) || r.Reason != "" {
			return reject("done submission fields")
		}
	case "unanswered":
		if r.Answer != 0 || r.Type != "input" && r.Entry != 0 {
			return reject("unanswered submission fields")
		}
	default:
		return reject("submission status")
	}
	return nil
}

func (r *SubmissionRecord) UnmarshalJSON(data []byte) error {
	var wire struct {
		ID           ID                     `json:"id"`
		Conversation ID                     `json:"conversationId"`
		Type         string                 `json:"type"`
		Status       string                 `json:"status"`
		RequestID    *string                `json:"requestId"`
		Entry        ID                     `json:"entry"`
		Answer       ID                     `json:"answer"`
		Reason       *string                `json:"reason"`
		Detail       json.RawMessage        `json:"detail"`
		Trace        *goai.TelemetryCarrier `json:"trace"`
	}
	if err := decodePublicJSON(data, &wire); err != nil {
		return err
	}
	result := SubmissionRecord{ID: wire.ID, Conversation: wire.Conversation, Type: wire.Type, Status: wire.Status, Entry: wire.Entry, Answer: wire.Answer, Trace: wire.Trace}
	if (wire.Status == "unanswered") != (wire.Reason != nil) {
		return reject("submission reason presence")
	}
	if wire.Reason != nil {
		result.Reason = *wire.Reason
	}
	if wire.RequestID != nil {
		result.RequestID, result.wireRequestIDPresent = *wire.RequestID, true
	}
	if len(wire.Detail) != 0 {
		var detail any
		if err := decodePublicJSON(wire.Detail, &detail); err != nil {
			return err
		}
		result.Detail = &TaskValue{Present: true, Value: detail}
	}
	if _, err := result.MarshalJSON(); err != nil {
		return err
	}
	*r = result
	return nil
}
