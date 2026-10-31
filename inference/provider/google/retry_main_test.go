package google

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func googleRetryEventsMain(ctx context.Context, api goai.Api, url string, opts *goai.StreamOptions) (done bool, terminal *goai.ErrorEvent) {
	opts.APIKey = "test-key"
	opts.Project = "test-project"
	opts.Location = "test-location"
	for event := range streamGoogle(ctx, &goai.Model{ID: "gemini-3-pro", Api: api, Provider: goai.ProviderGoogle, BaseURL: url}, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, opts) {
		switch e := event.(type) {
		case *goai.DoneEvent:
			done = true
		case *goai.ErrorEvent:
			terminal = e
		}
	}
	return
}

const googleRetrySuccessMain = "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}\n\n"

func TestGooglePublicRetryMatrixMain(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		for _, status := range []int{408, 409, 429, 500, 501, 599, 400} {
			for _, count := range []int{-2, -1, 0, 1} {
				t.Run(fmt.Sprint(api, "/", status, "/", count), func(t *testing.T) {
					var calls atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if calls.Add(1) == 1 {
							w.Header().Set("Retry-After", "0")
							w.WriteHeader(status)
							fmt.Fprint(w, "failure")
							return
						}
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, googleRetrySuccessMain)
					}))
					defer server.Close()
					delay := 1
					opts := &goai.StreamOptions{MaxRetryDelayMs: &delay}
					if count != -2 {
						opts.MaxRetries = &count
					}
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					done, terminal := googleRetryEventsMain(ctx, api, server.URL, opts)
					expected := int32(1)
					if count == 1 && status != 400 {
						expected = 2
					}
					if calls.Load() != expected || done != (expected == 2) || (terminal == nil) != (expected == 2) {
						t.Fatal("retry policy mismatch", calls.Load(), expected, done, terminal)
					}
				})
			}
		}
	}
}
func TestGoogleRetryConfigNativePrecedenceMain(t *testing.T) {
	if cfg := googleRetryConfig(nil); cfg.MaxRetries != 0 || cfg.MaxRetryDelayMs != 60000 {
		t.Fatal("wrong source defaults", cfg)
	}
	zero := 0
	if cfg := googleRetryConfig(&goai.StreamOptions{MaxRetries: &zero, MaxRetryDelayMs: &zero}); cfg.MaxRetries != 0 || cfg.MaxRetryDelayMs != 0 {
		t.Fatal("explicit zeros lost", cfg)
	}
	native := goai.RetryConfig{MaxRetries: 2, InitialDelay: time.Millisecond, ConnectTimeout: 7 * time.Second, RequestTimeout: 9 * time.Second, RetryableStatuses: []int{418}}
	cfg := googleRetryConfig(&goai.StreamOptions{RetryConfig: &native, MaxRetries: &zero})
	if cfg.MaxRetries != 2 || cfg.ConnectTimeout != native.ConnectTimeout || cfg.RequestTimeout != native.RequestTimeout || cfg.RetryableStatuses[0] != 418 {
		t.Fatal("native config changed", cfg)
	}
	if native.MaxRetries != 2 {
		t.Fatal("caller config mutated")
	}
}
func TestGoogleRetryDelayCapAndCancellationMain(t *testing.T) {
	for _, abort := range []bool{false, true} {
		t.Run(fmt.Sprint(abort), func(t *testing.T) {
			var calls atomic.Int32
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(429)
				fmt.Fprint(w, "limited")
				if abort {
					time.AfterFunc(20*time.Millisecond, cancel)
				}
			}))
			defer server.Close()
			count, delay := 1, 1
			if abort {
				delay = 2000
			}
			done, terminal := googleRetryEventsMain(ctx, goai.ApiGoogleGenerativeAI, server.URL, &goai.StreamOptions{MaxRetries: &count, MaxRetryDelayMs: &delay})
			if done || terminal == nil || calls.Load() != 1 {
				t.Fatal("delay handling", done, terminal, calls.Load())
			}
			if abort {
				if terminal.Reason != goai.StopReasonAborted {
					t.Fatal("cancel not aborted", terminal)
				}
			} else if !strings.Contains(terminal.Err.Error(), "retry delay") {
				t.Fatal("missing delay cap error", terminal.Err)
			}
		})
	}
}
func TestGoogleNeverReplaysOpenedStreamMain(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "4096")
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"started\"}]}}]}\n\n")
	}))
	defer server.Close()
	count := 2
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, terminal := googleRetryEventsMain(ctx, goai.ApiGoogleGenerativeAI, server.URL, &goai.StreamOptions{MaxRetries: &count})
	if calls.Load() != 1 || terminal == nil {
		t.Fatal("stream replayed or truncation lost", calls.Load(), terminal)
	}
}
