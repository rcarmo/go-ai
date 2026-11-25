package durable

import (
	"encoding/json"
	"reflect"
)

// A store patch is a sequence of individually bounded values, not a document.
// This transient budget is the existing worker frame budget, independent of
// the limits on a future SQLite transaction containing the patch.
func decodeCodemodeWorkerStorePatch(wire string, l Limits) (JSON, []string, error) {
	if len(wire) > l.MaxFramePayloadBytes {
		return nil, nil, reject("worker store patch exceeds frame limit")
	}
	var tuples [][]json.RawMessage
	if err := decodeStrictRoot([]byte(wire), l, l.MaxFramePayloadBytes, &tuples, '['); err != nil {
		return nil, nil, err
	}
	writes, seen := JSON{}, map[string]bool{}
	var deletes []string
	for _, tuple := range tuples {
		if len(tuple) < 1 || len(tuple) > 2 {
			return nil, nil, reject("invalid worker store patch")
		}
		var key *string
		if err := json.Unmarshal(tuple[0], &key); err != nil || key == nil || seen[*key] {
			return nil, nil, reject("invalid worker store key")
		}
		seen[*key] = true
		if len(tuple) == 1 {
			if len(deletes) >= l.MaxPage {
				return nil, nil, reject("codemode store deletion budget")
			}
			deletes = append(deletes, *key)
			continue
		}
		var encoded *string
		if err := json.Unmarshal(tuple[1], &encoded); err != nil || encoded == nil {
			return nil, nil, reject("invalid worker store value")
		}
		var value any
		if err := decodeWorkerJSON([]byte(*encoded), l, l.MaxRecordBytes, &value); err != nil {
			return nil, nil, err
		}
		writes[*key] = value
	}
	return writes, deletes, nil
}

// Own each value without making the map one Durable document. Use the same
// per-value JSON checks and keep the aggregate under the worker's frame cap.
func ownCodemodeStoreValues(values JSON, l Limits) (JSON, error) {
	if values == nil {
		return nil, nil
	}
	budget := jsonBudget{l: l, active: map[uintptr]bool{}}
	if err := budget.walk(reflect.ValueOf(values), 1, true); err != nil {
		return nil, err
	}
	out := make(JSON, len(values))
	left := DefaultLimits().MaxFramePayloadBytes - 2
	for key, value := range values {
		if len(key) > l.MaxStringBytes {
			return nil, reject("invalid codemode store key")
		}
		p, err := encodeBounded(value, l, l.MaxDocumentBytes)
		if err != nil {
			return nil, err
		}
		name, err := encodeBounded(key, l, l.MaxDocumentBytes)
		if err != nil {
			return nil, err
		}
		left -= len(name) + len(p) + 2
		if left < 0 {
			return nil, reject("codemode store exceeds frame limit")
		}
		var valueCopy any
		if err := decodeWorkerJSON(p, l, l.MaxDocumentBytes, &valueCopy); err != nil {
			return nil, err
		}
		out[key] = valueCopy
	}
	return out, nil
}
