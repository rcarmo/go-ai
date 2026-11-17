package durable

import (
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
	"time"
)

type testEnvironment struct{ cwd string }

func (e testEnvironment) Cwd() string { return e.cwd }

// Pinned harness-tools.test.ts: a throwing environment prevents tool execution,
// yields the original error as a tool_error diagnostic, and is reported once
// per generation preparation. The abort branch is an additional native fence.
func TestEnvironmentFactoryAbortAndFailureFenceEffects(t *testing.T) {
	for _, mode := range []string{"abort", "panic", "error"} {
		t.Run(mode, func(t *testing.T) {
			registry := NewRegistry()
			entered := make(chan struct{})
			cause := errors.New("no sandbox")
			reported := make(chan error, 16)
			var effects, calls atomic.Int64
			var toolIssued, factoryEntered atomic.Bool
			if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "env", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "env.test", Version: 1, Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
				env, err := api.Environment(ctx)
				if err != nil {
					return ToolResult{}, err
				}
				effects.Add(1)
				return ToolResult{Content: env.Cwd()}, nil
			}}); err != nil {
				t.Fatal(err)
			}
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if calls.Add(1) == 1 {
					toolIssued.Store(true)
					ch <- toolAnswer("env-call", "env", JSON{})
				} else {
					ch <- terminal("failure handled")
				}
				close(ch)
				return ch
			})
			options.Registry = registry
			options.OnReport = func(err error) { reported <- err }
			options.Env = func(ctx context.Context, _ EnvTarget) (ExecutionEnvironment, error) {
				if mode == "panic" {
					panic(cause)
				}
				if mode == "error" {
					return nil, cause
				}
				// Let preparation finish, then abort while the tool's real
				// environment is awaiting cancellation, before Execute.
				if !toolIssued.Load() || !factoryEntered.CompareAndSwap(false, true) {
					return testEnvironment{"ready"}, nil
				}
				close(entered)
				<-ctx.Done()
				return testEnvironment{"late"}, nil
			}
			store, _ := newTestSQLite(t)
			h := openHarness(t, store, options)
			conversation := root(t, h, ref)
			sub, err := conversation.Submit(bg, Input{Content: "environment"})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "abort" {
				awaitTaskSignal(t, entered)
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				if err := conversation.Abort(ctx); err != nil {
					t.Fatal(err)
				}
			}
			result := waitSubmission(t, sub)
			want := "done"
			if mode == "abort" {
				want = "unanswered"
			}
			if result.Record.Status != want || mode == "abort" && result.Record.Reason != "aborted" || effects.Load() != 0 {
				t.Fatal("factory allowed effect or lost settlement", result, effects.Load())
			}
			if mode != "abort" {
				history, err := conversation.Entries(bg, EntryQuery{}, 100, nil)
				if err != nil {
					t.Fatal(err)
				}
				results := 0
				for _, entry := range history.Items {
					if entry.Kind != "pi.tool-result" {
						continue
					}
					results++
					if len(entry.Model) != 1 || !entry.Model[0].IsError || entry.Model[0].ToolCallID != "env-call" || goai.ContentText(entry.Model[0].Content) != "<harness>\n[error] no sandbox\n</harness>" {
						t.Error("environment error result", entry)
					}
					if !equalJSONValue(entry.Data, JSON{"diagnostics": []any{JSON{"severity": "error", "code": "tool_error", "message": "no sandbox"}}}) {
						t.Error("environment error diagnostic", entry.Data)
					}
				}
				if results != 1 || calls.Load() != 2 {
					t.Error("environment failure round", results, calls.Load())
				}
			}
			// Close joins report delivery; no polling establishes this count.
			if err := h.Close(bg); err != nil {
				t.Fatal(err)
			}
			if mode != "abort" {
				if len(reported) != 2 {
					t.Fatal("one report per preparation", len(reported))
				}
				for range 2 {
					if err := <-reported; err != cause {
						t.Error("environment failure identity", err)
					}
				}
			}
		})
	}
}
