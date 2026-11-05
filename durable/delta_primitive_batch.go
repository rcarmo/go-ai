package durable

import (
	"encoding/json"
	"reflect"
)

func normalizedDeltaPrimitive(value any) bool {
	switch value.(type) {
	case nil, bool, string, json.Number:
		return true
	default:
		return false
	}
}

// A large set-only batch on one object can account for every intermediate
// revision exactly without re-encoding the entire growing tree for each key.
// This never bypasses tuple, scalar, member, node, depth or encoded-byte limits.
// Unsupported shapes fall back BEFORE mutation to complete generic replay.
func applyPrimitiveObjectSets(root any, operations []Operation, limits Limits, encodedBytes int) (bool, error) {
	if len(operations) < 64 {
		return false, nil
	}
	first := operations[0]
	if first[0] != "s" {
		return false, nil
	}
	path := first[1].([]any)
	if len(path) == 0 {
		return false, nil
	}
	prefix := path[:len(path)-1]
	var parent any = root
	for _, segment := range prefix {
		switch container := parent.(type) {
		case map[string]any:
			var exists bool
			parent, exists = container[segmentKey(segment)]
			if !exists {
				return false, nil
			}
		case []any:
			index, err := deltaIndex(segment)
			if err != nil || index >= uint64(len(container)) {
				return false, nil
			}
			parent = container[index]
		default:
			return false, nil
		}
	}
	object, ok := parent.(map[string]any)
	if !ok {
		return false, nil
	}
	for _, operation := range operations {
		if operation[0] != "s" || !normalizedDeltaPrimitive(operation[2]) {
			return false, nil
		}
		current := operation[1].([]any)
		if len(current) != len(path) {
			return false, nil
		}
		for i, segment := range prefix {
			if current[i] != segment {
				return false, nil
			}
		}
		if old, exists := object[segmentKey(current[len(current)-1])]; exists && !normalizedDeltaPrimitive(old) {
			return false, nil
		}
	}
	budget := jsonBudget{l: limits, active: make(map[uintptr]bool)}
	if err := budget.walk(reflect.ValueOf(root), 1, true); err != nil {
		return true, err
	}
	nodes := budget.nodes
	sizes := make(map[any]int)
	sizeOf := func(value any) (int, error) {
		if size, ok := sizes[value]; ok {
			return size, nil
		}
		data, err := encodeBounded(value, limits, limits.MaxRecordBytes)
		if err != nil {
			return 0, err
		}
		sizes[value] = len(data)
		return len(data), nil
	}
	for _, operation := range operations {
		current := operation[1].([]any)
		key := segmentKey(current[len(current)-1])
		old, exists := object[key]
		size, err := sizeOf(operation[2])
		if err != nil {
			return true, err
		}
		delta := size
		if exists {
			previous, err := sizeOf(old)
			if err != nil {
				return true, err
			}
			delta -= previous
		} else {
			if len(object) >= limits.MaxMembers {
				return true, reject("object growth limit")
			}
			if nodes > limits.MaxNodes-2 {
				return true, reject("JSON node limit")
			}
			nodes += 2 // key and primitive
			keyBytes, err := sizeOf(key)
			if err != nil {
				return true, err
			}
			delta += keyBytes + 1 // key and colon
			if len(object) > 0 {
				delta++
			} // comma
		}
		if delta > limits.MaxDocumentBytes-encodedBytes {
			return true, reject("encoded JSON byte limit")
		}
		encodedBytes += delta
		object[key] = operation[2] // owned primitives have no mutable placement authority
	}
	return true, nil
}
