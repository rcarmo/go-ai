package goai_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

// A rejected fetch is an ordinary error, without provider status and headers.
func TestMainDecisionsProviderFetchErrorDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: providerRetryTransportMain(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("fetch fixture failure")
	})}
	result, err := goai.Classify(decisionsModel110("http://example.invalid"), issue1Context(), &goai.ClassifierOptions{APIKey: "fixture", HTTPClient: client, MaxRetries: 2})
	if err != nil || result.StopReason != goai.StopReasonError || calls.Load() != 1 || !strings.Contains(result.ErrorMessage, "fetch fixture failure") {
		t.Fatalf("calls=%d, result=%+v, error=%v", calls.Load(), result, err)
	}
}

type classifierFailingBody struct{ closed *atomic.Int32 }

func (b classifierFailingBody) Read([]byte) (int, error) {
	return 0, errors.New("body fixture failure")
}
func (b classifierFailingBody) Close() error { b.closed.Add(1); return nil }

// A rejected response.text() is not an HTTP provider error in the pinned source.
func TestMainDecisionsProviderErrorBodyFailureDoesNotRetry(t *testing.T) {
	var calls, closes atomic.Int32
	client := &http.Client{Transport: providerRetryTransportMain(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return &http.Response{StatusCode: 503, Header: http.Header{"Retry-After-Ms": []string{"0"}}, Body: classifierFailingBody{closed: &closes}}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"answers":[]}`))}, nil
	})}
	result, err := goai.Classify(decisionsModel110("http://example.invalid"), issue1Context(), &goai.ClassifierOptions{APIKey: "fixture", HTTPClient: client, MaxRetries: 2})
	if err != nil || result.StopReason != goai.StopReasonError || calls.Load() != 1 || closes.Load() != 1 {
		t.Fatalf("calls=%d, closes=%d, result=%+v, error=%v", calls.Load(), closes.Load(), result, err)
	}
}

// The classifier's native 1 MiB response limit applies before retry too.
func TestMainDecisionsProviderOversizedErrorBodyDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: providerRetryTransportMain(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 503, Header: http.Header{"Retry-After-Ms": []string{"0"}}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", (1<<20)+1)))}, nil
	})}
	result, err := goai.Classify(decisionsModel110("http://example.invalid"), issue1Context(), &goai.ClassifierOptions{APIKey: "fixture", HTTPClient: client, MaxRetries: 2})
	if err != nil || result.StopReason != goai.StopReasonError || calls.Load() != 1 || !strings.Contains(result.ErrorMessage, "exceeds 1 MiB") {
		t.Fatalf("calls=%d, result=%+v, error=%v", calls.Load(), result, err)
	}
}

// Omitted timeout leaves the fetch without an added per-attempt deadline.
func TestMainDecisionsProviderDefaultHasNoAttemptDeadline(t *testing.T) {
	var sawDeadline atomic.Bool
	client := &http.Client{Transport: providerRetryTransportMain(func(r *http.Request) (*http.Response, error) {
		_, hasDeadline := r.Context().Deadline()
		sawDeadline.Store(hasDeadline)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"answers":[]}`))}, nil
	})}
	_, err := goai.Classify(decisionsModel110("http://example.invalid"), issue1Context(), &goai.ClassifierOptions{APIKey: "fixture", HTTPClient: client})
	if err != nil || sawDeadline.Load() {
		t.Fatalf("added deadline=%v, error=%v", sawDeadline.Load(), err)
	}
}

// Explicit timeoutMs: 0 is distinct from an omitted timeout in the pinned API.
func TestMainDecisionsProviderExplicitZeroTimeout(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
			_ = json.NewEncoder(w).Encode(map[string]any{"answers": decisionsAnswers110()})
		}
	}))
	defer srv.Close()
	zero := 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := goai.Classify(decisionsModel110(srv.URL), issue1Context(), &goai.ClassifierOptions{APIKey: "fixture", Context: ctx, TimeoutMsOverride: &zero})
	if err != nil || result.StopReason != goai.StopReasonError || !strings.Contains(result.ErrorMessage, "deadline exceeded") || calls.Load() > 1 || ctx.Err() != nil {
		t.Fatalf("calls=%d, caller error=%v, result=%+v, error=%v", calls.Load(), ctx.Err(), result, err)
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
