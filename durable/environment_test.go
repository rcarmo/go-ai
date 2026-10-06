package durable

import (
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testEnvironment struct{ cwd string }

func (e testEnvironment) Cwd() string { return e.cwd }
func TestEnvironmentFactoryAbortAndFailureFenceEffects(t *testing.T) {
	for _, mode := range []string{"abort", "panic", "error"} {
		t.Run(mode, func(t *testing.T) {
			registry := NewRegistry()
			entered := make(chan struct{})
			var effects atomic.Int64
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
			var calls atomic.Int64
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if calls.Add(1) == 1 {
					ch <- toolAnswer("env-call", "env", JSON{})
				} else {
					ch <- terminal("failure handled")
				}
				close(ch)
				return ch
			})
			options.Registry = registry
			options.Env = func(ctx context.Context, target EnvTarget) (ExecutionEnvironment, error) {
				close(entered)
				if mode == "panic" {
					panic("SECRET ENV")
				}
				if mode == "error" {
					return nil, errors.New("SECRET ENV")
				}
				<-ctx.Done()
				return testEnvironment{"late"}, nil
			}
			store, _ := NewMemory()
			h := openHarness(t, store, options)
			conversation := root(t, h, ref)
			sub, err := conversation.Submit(bg, Input{Content: "environment"})
			if err != nil {
				t.Fatal(err)
			}
			awaitTaskSignal(t, entered)
			if mode == "abort" {
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				if err := conversation.Abort(ctx); err != nil {
					t.Fatal(err)
				}
			}
			result := waitSubmission(t, sub)
			want := "done"
			if mode == "abort" {
				want = "aborted"
			}
			if result.Submission.Status != want || effects.Load() != 0 {
				t.Fatal("factory allowed effect", result, effects.Load())
			}
			state, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			for _, task := range state.Tasks {
				encoded, err := encodeBounded(task, DefaultLimits(), DefaultLimits().MaxRecordBytes)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(encoded), "SECRET ENV") {
					t.Fatal("factory private error persisted")
				}
			}
			encoded, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "SECRET ENV") {
				t.Fatal("factory private error persisted outside task")
			}
		})
	}
}
