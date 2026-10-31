package stricttools

import "testing"

func TestLimitMessageMain(t *testing.T) {
	for _, tc := range []struct {
		message string
		want    bool
	}{
		{"The compiled grammar is too large", true}, {"prefix: Too many strict tools (21), the limit is 20", true}, {"Schemas contains too many parameters with union types (17)", true},
		{"the compiled grammar is too large", false}, {"Too many strict tools", false}, {"Schemas contains too many parameters", false}, {"unrelated validation error", false}, {"", false},
	} {
		if got := LimitMessage(tc.message); got != tc.want {
			t.Errorf("%q: %v, want %v", tc.message, got, tc.want)
		}
	}
}
