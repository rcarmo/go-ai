package durable

import (
	"context"
	"encoding/json"
	"fmt"

	goai "github.com/rcarmo/go-ai"
)

// SignalState contains a delivered value (which may be null) or an expired key.
// A nil *SignalState denotes a key with neither a value nor an expiration.
type SignalState struct {
	Value   any
	Expired bool
}

func (s SignalState) MarshalJSON() ([]byte, error) {
	if s.Expired {
		return []byte(`{"expired":true}`), nil
	}
	return json.Marshal(struct {
		Value any `json:"value"`
	}{s.Value})
}

// ReadSignal reads this invocation's retained signal value, even after a wake
// consumed the key. A migration changes only the returned view of that value.
func (r *TaskRuntime) ReadSignal(ctx context.Context, token *SignalToken, key string) (*SignalState, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	if token == nil || token.definition.Schema == nil {
		return nil, reject("invalid signal token")
	}
	value, found, err := r.Memo(ctx, signalMemoName(token.Name(), key))
	if err != nil || !found {
		return nil, err
	}
	stored := generationObject(value)
	if _, expired := stored["expired"]; expired {
		return &SignalState{Expired: true}, nil
	}
	payload, present := stored["value"]
	number, ok := exactNumber(stored["version"])
	if !present || !ok || number == nil || !number.IsInt() || !number.Num().IsUint64() || number.Sign() <= 0 {
		return nil, reject("invalid signal memo")
	}
	version := number.Num().Uint64()
	if version == token.Version() {
		return &SignalState{Value: payload}, nil
	}
	if version > token.Version() {
		return nil, fmt.Errorf("Signal %s value has version %d, newer than %d", token.Name(), version, token.Version())
	}
	if token.definition.Migrate == nil {
		return nil, fmt.Errorf("Signal %s has no migration from version %d", token.Name(), version)
	}
	migrated, err := token.definition.Migrate(payload, version)
	if err != nil {
		return nil, err
	}
	owned, err := ownJSONValue(migrated, r.harness.session.limits)
	if err != nil || !goai.CheckJSONSchemaValue(map[string]any(token.definition.Schema), owned) {
		return nil, fmt.Errorf("Signal %s migration from version %d returned an invalid value", token.Name(), version)
	}
	return &SignalState{Value: owned}, nil
}
