package goai

import (
	"encoding/json"
	"testing"
)

func TestImage120ResizeLimitPresenceRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		present bool
		value   int
	}{{`{}`, false, 0}, {`{"maxWidth":null}`, false, 0}, {`{"maxWidth":0}`, true, 0}, {`{"maxWidth":-1}`, true, -1}, {`{"maxWidth":4}`, true, 4}} {
		var value ModelImageResizeOptions
		if err := json.Unmarshal([]byte(tc.raw), &value); err != nil {
			t.Fatal(err)
		}
		if value.MaxWidthSet != tc.present || value.MaxWidth != tc.value {
			t.Fatal(tc, value)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var again ModelImageResizeOptions
		if err = json.Unmarshal(encoded, &again); err != nil {
			t.Fatal(err)
		}
		if again != value {
			t.Fatal(tc, string(encoded), again)
		}
	}
	original := Model{InputLimits: &ModelInputLimits{Images: &ModelImageInputLimits{Resize: &ModelImageResizeOptions{MaxBytesSet: true}}}}
	copy := cloneModel(&original)
	if !copy.InputLimits.Images.Resize.MaxBytesSet {
		t.Fatal("clone lost explicit zero")
	}
}
