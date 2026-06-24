package oauth

import (
	"testing"
	"time"
)

func TestOAuthDeviceCodePollIntervalClampsToRFCMinimum(t *testing.T) {
	for _, secs := range []int{-1, 0, 1, 4} {
		if got := normalizeDeviceCodePollInterval(secs); got != 5*time.Second {
			t.Fatalf("normalizeDeviceCodePollInterval(%d) = %v, want 5s", secs, got)
		}
	}
	if got := normalizeDeviceCodePollInterval(7); got != 7*time.Second {
		t.Fatalf("normalizeDeviceCodePollInterval(7) = %v, want 7s", got)
	}
}

func TestOAuthDeviceCodeSlowDownAddsFiveSeconds(t *testing.T) {
	interval := 5 * time.Second
	got, ok := nextDeviceCodePollInterval(interval, "slow_down")
	if !ok {
		t.Fatal("slow_down should continue polling")
	}
	if got != 10*time.Second {
		t.Fatalf("slow_down interval = %v, want 10s", got)
	}

	got, ok = nextDeviceCodePollInterval(got, "authorization_pending")
	if !ok || got != 10*time.Second {
		t.Fatalf("authorization_pending got (%v, %v), want unchanged 10s and continue", got, ok)
	}

	if _, ok := nextDeviceCodePollInterval(got, "access_denied"); ok {
		t.Fatal("terminal OAuth errors should not continue polling")
	}
}
