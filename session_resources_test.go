package goai

import (
	"errors"
	"reflect"
	"testing"
)

// The pinned cleanup registry contains each individual failure so that a
// provider cannot keep the other resources alive by throwing during cleanup.
func TestSourceV160SessionResourceCleanupContinuesAfterPanic(t *testing.T) {
	first, second := errors.New("cleanup panic"), errors.New("cleanup returned error")
	var calls []string
	defer RegisterSessionResourceCleanup(func(id string) error {
		calls = append(calls, id+"/first")
		panic(first)
	})()
	defer RegisterSessionResourceCleanup(func(id string) error {
		calls = append(calls, id+"/second")
		return second
	})()
	defer func() {
		if value := recover(); value != nil {
			t.Fatalf("cleanup panic escaped before remaining callbacks: %v, calls %v", value, calls)
		}
	}()
	err := CleanupSessionResources("pinned-test")
	if !errors.Is(err, first) || !errors.Is(err, second) || !reflect.DeepEqual(calls, []string{"pinned-test/first", "pinned-test/second"}) {
		t.Fatalf("cleanup = %v, calls %v", err, calls)
	}
}
