package durable

import "testing"

func TestCodemode120ResultUnionAndExplicitNull(t *testing.T) {
	for _, result := range []CodemodeResult{{Value: "hidden"}, {HasValue: true, Value: "success", Error: "failure"}, {HasValue: true, Error: "failure"}} {
		if _, err := ownCodemodeResult(result, DefaultLimits()); err == nil {
			t.Fatal("invalid result union accepted", result)
		}
	}
	owned, err := ownCodemodeResult(CodemodeResult{HasValue: true, Value: nil}, DefaultLimits())
	if err != nil || !owned.HasValue || owned.Value != nil {
		t.Fatal("explicit null lost", owned, err)
	}
}
