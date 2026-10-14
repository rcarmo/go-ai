package durable

import (
	"strconv"
	"strings"
)

func validSequentialKey(key string) bool {
	value, err := strconv.ParseUint(key, 10, 64)
	return err == nil && value > 0 && value <= MaxID && strconv.FormatUint(value, 10) == key
}

func validNestedKey(key string) bool {
	if key == "" || key == "__proto__" || strings.Contains(key, "/") {
		return false
	}
	// Positive integer path segments are reserved for upstream sequential keys.
	positive := key[0] >= '1' && key[0] <= '9'
	for i := 1; i < len(key) && positive; i++ {
		positive = key[i] >= '0' && key[i] <= '9'
	}
	return !positive
}
