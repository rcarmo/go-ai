package jsonparse

import (
	"sort"
	"strconv"
)

// PropertyKeys enumerates keys as JavaScript does: array-index keys first in
// numeric order, followed by other keys in insertion order. A Go map has no
// insertion order, so keys missing an order witness use lexical order.
func PropertyKeys[V any](order []string, object map[string]V) []string {
	keys := make([]string, 0, len(object))
	seen := make(map[string]bool, len(object))
	for _, key := range order {
		if _, exists := object[key]; exists && !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	start := len(keys)
	for key := range object {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys[start:])
	return OrderPropertyKeys(keys)
}

// OrderPropertyKeys puts index properties first without reordering the other
// string keys. It may reorder the supplied slice in place.
func OrderPropertyKeys(keys []string) []string {
	sort.SliceStable(keys, func(i, j int) bool {
		a, aIndex := propertyIndex(keys[i])
		b, bIndex := propertyIndex(keys[j])
		if aIndex && bIndex {
			return a < b
		}
		return aIndex && !bIndex
	})
	return keys
}

func propertyIndex(key string) (uint64, bool) {
	index, err := strconv.ParseUint(key, 10, 32)
	return index, err == nil && index < 1<<32-1 && strconv.FormatUint(index, 10) == key
}
