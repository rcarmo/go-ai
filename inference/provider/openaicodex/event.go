package openaicodex

import (
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
)

// Codex's JS adapter accesses null.type before filtering event types. This is
// an ordinary adapter exception upstream, eligible for pre-start fallback.
type codexNullEventError struct{ partial *goai.Message }

func (e *codexNullEventError) Error() string {
	return "Cannot read properties of null (reading 'type')"
}

// A callback can change type after the adapter's initial string-type filter.
// The shared event processor ignores such a type, after starting the output.
type codexEventType string

func (t *codexEventType) UnmarshalJSON(raw []byte) error {
	var value string
	_ = json.Unmarshal(raw, &value)
	*t = codexEventType(value)
	return nil
}

func decodeCodexEvent(data []byte, transport string, opts *goai.StreamOptions, model *goai.Model, partial *goai.Message) ([]byte, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		if json.Valid(data) {
			return nil, false, nil
		}
		return nil, false, &codexProtocolError{message: fmt.Sprintf("Invalid Codex %s JSON: %s", transport, err), cause: err, payload: string(data), partial: partial}
	}
	if fields == nil {
		return nil, false, &codexNullEventError{partial: partial}
	}
	var kind *string
	if json.Unmarshal(fields["type"], &kind) != nil || kind == nil {
		return nil, false, nil
	}
	observed, err := observeCodexEvent(data, opts, model)
	if err != nil {
		return nil, false, &codexObserverError{cause: err, partial: partial}
	}
	return observed, true, nil
}
