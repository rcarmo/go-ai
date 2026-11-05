package oauth

import (
	"testing"
	"time"
)

func TestOAuthDeviceCodePollIntervalDefaultAndMinimum(t *testing.T) {
	for _, tc := range []struct {
		secs int
		want time.Duration
	}{{-1, time.Second}, {0, 5 * time.Second}, {1, time.Second}, {4, 4 * time.Second}, {7, 7 * time.Second}} {
		if got := normalizeDeviceCodePollInterval(tc.secs); got != tc.want {
			t.Fatalf("interval %d got %s want %s", tc.secs, got, tc.want)
		}
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

func TestOAuthDeviceCodeSlowDownHonorsServerProvidedInterval(t *testing.T) {
	got, ok := nextDeviceCodePollIntervalWithServerInterval(7*time.Second, "slow_down", 12)
	if !ok || got != 12*time.Second {
		t.Fatalf("slow_down with server interval got (%v, %v), want 12s and continue", got, ok)
	}
	got, ok = nextDeviceCodePollIntervalWithServerInterval(7*time.Second, "slow_down", 1)
	if !ok || got != time.Second {
		t.Fatalf("slow_down with low server interval got (%v, %v), want clamped 1s and continue", got, ok)
	}
}

// v1.5.0 oauth-device-code.test.ts uses a two-second interval; the shared
// source helper clamps explicit intervals to one second, not five seconds.
func TestV150DeviceCodeExplicitIntervals(t *testing.T) {
	for _, seconds := range []int{1, 2, 4, 7} {
		if got := normalizeDeviceCodePollInterval(seconds); got != time.Duration(seconds)*time.Second {
			t.Errorf("interval %d: got %s", seconds, got)
		}
	}
	if got, ok := nextDeviceCodePollIntervalWithServerInterval(2*time.Second, "slow_down", 1); !ok || got != time.Second {
		t.Errorf("server interval 1: %s %v", got, ok)
	}
}
