package durable

import "testing"

func TestCodemode120StoreDeletionPatchValidation(t *testing.T) {
	limits := DefaultLimits()
	for _, result := range []CodemodeResult{{StoreDeletes: []string{"key", "key"}}, {StoreDeletes: []string{"key"}, StoreWrites: JSON{"key": nil}}} {
		if _, err := ownCodemodeResult(result, limits); err == nil {
			t.Fatal("invalid store deletion accepted", result)
		}
	}
	result, err := ownCodemodeResult(CodemodeResult{StoreDeletes: []string{"missing", ""}, StoreWrites: JSON{"null": nil}}, limits)
	if err != nil || len(result.StoreDeletes) != 2 {
		t.Fatal(result, err)
	}
}
