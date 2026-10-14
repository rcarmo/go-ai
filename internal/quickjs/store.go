package quickjs

import (
	"encoding/json"
	"errors"
)

func parseStoreWrites(raw string) (StoreWrites, error) {
	empty := StoreWrites{Set: map[string]json.RawMessage{}, Delete: []string{}}
	invalid := func() (StoreWrites, error) { return empty, errors.New("invalid QuickJS store patch") }
	if len(raw) > int(maxBridgeStringBytes) {
		return invalid()
	}
	var entries [][]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &entries); err != nil || entries == nil || len(entries) > maxBridgeOutputItems {
		return invalid()
	}
	patch := StoreWrites{Set: map[string]json.RawMessage{}, Delete: []string{}}
	seen := map[string]bool{}
	for _, entry := range entries {
		if len(entry) < 1 || len(entry) > 2 {
			return invalid()
		}
		var key string
		if len(entry[0]) == 0 || entry[0][0] != '"' || json.Unmarshal(entry[0], &key) != nil || seen[key] {
			return invalid()
		}
		seen[key] = true
		if len(entry) == 1 {
			patch.Delete = append(patch.Delete, key)
			continue
		}
		var value string
		if len(entry[1]) == 0 || entry[1][0] != '"' || json.Unmarshal(entry[1], &value) != nil || !json.Valid([]byte(value)) {
			return invalid()
		}
		patch.Set[key] = json.RawMessage(value)
	}
	return patch, nil
}
