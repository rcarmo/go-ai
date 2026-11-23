package durable

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source: pi v1.6.0 harness-tools.test.ts:1117–1129. An explicit null
// replaces progress details; the omitted case verifies the fallback separately.
func TestSourceV160ToolResultExplicitNullDetails(t *testing.T) {
	for _, tc := range []struct {
		name     string
		explicit bool
		want     string
	}{
		{"explicit-null", true, "null"},
		{"omitted-control", false, `{"old":1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				var requests, effects atomic.Int64
				registry := NewRegistry()
				if err := registry.Register(ToolRegistration{
					Definition:     goai.Tool{Name: "null", Description: "The null tool", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)},
					Implementation: "null.native", Version: 1,
					Execute: func(_ context.Context, _ any, api *ToolAPI) (ToolResult, error) {
						effects.Add(1)
						if err := api.Details(JSON{"old": 1}); err != nil {
							return ToolResult{}, err
						}
						return ToolResult{Blocks: []goai.ContentBlock{}, HasDetails: tc.explicit}, nil
					},
				}); err != nil {
					t.Fatal(err)
				}
				ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
					events := make(chan goai.Event, 1)
					if requests.Add(1) == 1 {
						events <- toolAnswer("c1", "null", JSON{})
					} else {
						events <- terminal("done")
					}
					close(events)
					return events
				})
				options.Registry = registry
				h := openHarness(t, b.store, options)
				c := root(t, h, ref)
				sub, err := c.Submit(bg, Input{Content: "go"})
				if err != nil {
					t.Fatal(err)
				}
				if settled := waitSubmission(t, sub).Submission; settled.Status != "done" || requests.Load() != 2 || effects.Load() != 1 {
					t.Fatalf("settlement=%+v requests=%d effects=%d", settled, requests.Load(), effects.Load())
				}
				result := v160ToolResult(t, bg, c, "c1")
				if result.IsError {
					t.Fatalf("tool failed: %+v", result)
				}
				// The Go receipt has a presence flag and a separate JSON-value
				// arm. Check both so a missing value cannot pass as null.
				if !result.HasDetails || result.Details != nil {
					t.Fatalf("receipt details presence = %t, object arm = %+v; want the JSON-value arm", result.HasDetails, result.Details)
				}
				data, err := json.Marshal(result.DetailsValue)
				if err != nil {
					t.Fatal(err)
				}
				if got := string(data); got != tc.want {
					t.Fatalf("receipt details = %q; want %q", got, tc.want)
				}
			})
		})
	}
}
