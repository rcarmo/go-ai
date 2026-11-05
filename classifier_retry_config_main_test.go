package goai

import (
	"testing"
	"time"
)

func TestMainClassifierRetryDefaultsAndExplicitZero(t *testing.T) {
	zero, one := 0, 1
	for _, tc := range []struct {
		name           string
		opts           *ClassifierOptions
		retries, delay int
	}{
		{"nil", nil, 2, 60000},
		{"empty", &ClassifierOptions{}, 2, 60000},
		{"legacy-positive", &ClassifierOptions{MaxRetries: 1, MaxRetryDelayMs: 1200}, 1, 1200},
		{"zero-retries", &ClassifierOptions{MaxRetriesOverride: &zero}, 0, 60000},
		{"zero-cap", &ClassifierOptions{MaxRetryDelayMsOverride: &zero}, 2, 0},
		{"override-precedence", &ClassifierOptions{MaxRetries: 3, MaxRetriesOverride: &one}, 1, 60000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := classifierRetryConfig(tc.opts)
			if cfg.MaxRetries != tc.retries || cfg.MaxRetryDelayMs != tc.delay {
				t.Fatalf("got retries=%d cap=%d, want %d/%d", cfg.MaxRetries, cfg.MaxRetryDelayMs, tc.retries, tc.delay)
			}
			if cfg.MaxRetryDelayMs != 0 && time.Duration(cfg.MaxRetryDelayMs)*time.Millisecond <= 0 {
				t.Fatal("expected positive cap")
			}
		})
	}
}
