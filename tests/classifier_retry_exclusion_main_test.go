package goai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Port of the pinned Decisions 503 recovery with omitted retry options.
func TestMainDecisionsProviderDefaultRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After-Ms", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": decisionsAnswers110()})
	}))
	defer srv.Close()
	result, err := goai.Classify(decisionsModel110(srv.URL), issue1Context(), &goai.ClassifierOptions{APIKey: "fixture"})
	if err != nil || result.StopReason != goai.StopReasonStop || calls.Load() != 2 {
		t.Fatalf("calls=%d, result=%+v, error=%v", calls.Load(), result, err)
	}
}

// Source-shaped explicit zero disables the default retries.
func TestMainDecisionsProviderExplicitZeroRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After-Ms", "0")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	zero := 0
	result, err := goai.Classify(decisionsModel110(srv.URL), issue1Context(), &goai.ClassifierOptions{APIKey: "fixture", MaxRetriesOverride: &zero})
	if err != nil || result.StopReason != goai.StopReasonError || calls.Load() != 1 {
		t.Fatalf("calls=%d, result=%+v, error=%v", calls.Load(), result, err)
	}
}

// Source-derived: an attempt timeout is a TimeoutError, not a provider error.
func TestMainDecisionsProviderAttemptTimeoutDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := goai.Classify(decisionsModel110(srv.URL), issue1Context(), &goai.ClassifierOptions{APIKey: "fixture", Context: ctx, TimeoutMs: 20, MaxRetries: 2})
	if err != nil || result.StopReason != goai.StopReasonError || calls.Load() != 1 {
		t.Fatalf("calls=%d, result=%+v, error=%v", calls.Load(), result, err)
	}
}

// Source-derived from the generic retry predicate and OpenAI Decisions' 504 exclusion.
func TestMainDecisionsProviderRetriesServerStatusesAndDirective(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		directive string
	}{
		{"status-600", 600, ""},
		{"forced-400", 400, "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					w.Header().Set("Retry-After-Ms", "0")
					if tc.directive != "" {
						w.Header().Set("X-Should-Retry", tc.directive)
					}
					w.WriteHeader(tc.status)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"answers": decisionsAnswers110()})
			}))
			defer srv.Close()
			result, err := goai.Classify(decisionsModel110(srv.URL), issue1Context(), &goai.ClassifierOptions{APIKey: "fixture", MaxRetries: 1})
			if err != nil || result.StopReason != goai.StopReasonStop || calls.Load() != 2 {
				t.Fatalf("calls=%d, result=%+v, error=%v", calls.Load(), result, err)
			}
		})
	}
}

// Source-shaped OpenAI Decisions gateway refusal, with an additional server
// force-retry directive to exercise noRetryStatuses precedence through HTTP.
func TestMainDecisionsProviderExcludesGatewayTimeoutDespiteRetryHeader(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("X-Should-Retry", "true")
		w.Header().Set("Retry-After-Ms", "0")
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte("<html>gateway timeout</html>"))
	}))
	defer srv.Close()
	result, err := goai.Classify(decisionsModel110(srv.URL), issue1Context(), &goai.ClassifierOptions{APIKey: "fixture", MaxRetries: 2})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || result.StopReason != goai.StopReasonError || !strings.Contains(result.ErrorMessage, "600K") || strings.Contains(result.ErrorMessage, "<html>") {
		t.Fatalf("requests=%d, stop=%v, error=%q", requests.Load(), result.StopReason, result.ErrorMessage)
	}
}
