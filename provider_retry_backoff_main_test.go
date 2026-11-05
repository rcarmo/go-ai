package goai

import (
	"testing"
	"time"
)

// Source-derived from provider-retry.ts at upstream v1.5.0: the fallback
// is min(0.5*2^retryIndex, 8) seconds, reduced by up to 25% jitter.
func TestMainProviderRetryFallbackBackoffBounds(t *testing.T) {
	for _, tc := range []struct {
		index int
		base  time.Duration
	}{
		{0, 500 * time.Millisecond},
		{1, time.Second},
		{4, 8 * time.Second},
		{5, 8 * time.Second},
		{64, 8 * time.Second},
		{1000, 8 * time.Second},
	} {
		for i := 0; i < 32; i++ {
			delay, err := providerRetryDelay(&ProviderRequestError{Status: 503}, tc.index, ProviderRetryOptions{})
			if err != nil || delay < tc.base*3/4 || delay > tc.base {
				t.Fatalf("index %d: delay=%v err=%v, want [%v,%v]", tc.index, delay, err, tc.base*3/4, tc.base)
			}
		}
	}
}

func TestMainProviderRetryFallbackHasDownwardJitter(t *testing.T) {
	// The probability of 64 independent samples all being exactly the base
	// with a working continuous jitter source is negligible.
	for i := 0; i < 64; i++ {
		delay, err := providerRetryDelay(&ProviderRequestError{Status: 503}, 0, ProviderRetryOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if delay < 500*time.Millisecond {
			return
		}
	}
	t.Fatal("fallback never applied downward jitter")
}
