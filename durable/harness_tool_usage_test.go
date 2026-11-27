package durable

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Native-only: pi v1.7.0 tool.ts records usage and the tool receipt in one
// transaction, but ToolExecutionResult has no counterpart to Go's Commit callback.
func TestToolUsageValidationFencesApplicationCommit(t *testing.T) {
	charged := &goai.Usage{Input: 2, Output: 1, ImageOutputSet: true, TotalTokens: 3}
	image := &goai.Usage{Output: 4, ImageOutput: 2, ImageOutputSet: true, TotalTokens: 4}
	tokenLimit := &goai.Usage{Input: int(MaxID), TotalTokens: int(MaxID)}
	costLimit := &goai.Usage{Cost: goai.CostBreakdown{Total: math.MaxFloat64}}
	const invalid = "durable: rejected: invalid contribution usage"
	const nonFinite = "durable: rejected: Value contains a non-finite number and is not strict JSON"
	for _, tc := range []struct {
		name                    string
		prior, usage, total     *goai.Usage
		fault                   string
		appCalls                int64
		noCallback, pairedError bool
	}{
		{name: "negative-input", usage: &goai.Usage{Input: -1}, fault: invalid},
		{name: "negative-image-output", usage: &goai.Usage{ImageOutput: -1, ImageOutputSet: true}, fault: invalid},
		{name: "negative-cost", usage: &goai.Usage{Cost: goai.CostBreakdown{Input: -1}}, fault: invalid},
		{name: "over-budget-counter", usage: &goai.Usage{Input: int(MaxID) + 1}, fault: invalid},
		{name: "nan-cost", usage: &goai.Usage{Cost: goai.CostBreakdown{Total: math.NaN()}}, fault: nonFinite},
		{name: "infinite-cost", usage: &goai.Usage{Cost: goai.CostBreakdown{Output: math.Inf(1)}}, fault: nonFinite},
		{name: "aggregate-counter-overflow", prior: tokenLimit, usage: charged, total: tokenLimit, fault: "durable: rejected: usage overflow"},
		{name: "aggregate-cost-overflow", prior: costLimit, usage: costLimit, total: costLimit, fault: nonFinite},
		{name: "valid-aggregate", prior: charged, usage: image, total: &goai.Usage{Input: 2, Output: 5, ImageOutput: 2, ImageOutputSet: true, TotalTokens: 7}, appCalls: 1},
		{name: "callback-error-rolls-back", prior: charged, usage: image, total: charged, fault: "durable: rejected: test app refusal", appCalls: 1},
		{name: "without-callback", usage: charged, total: charged, noCallback: true},
		{name: "paired-go-error-keeps-current-contract", usage: charged, total: charged, appCalls: 1, pairedError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			var effects, apps, requests atomic.Int64
			toolIDs := make(chan ID, 2)
			registry := NewRegistry()
			tool := wrapRegistration("usage")
			tool.Execute = func(_ context.Context, _ any, api *ToolAPI) (ToolResult, error) {
				n := effects.Add(1)
				if n <= 2 {
					toolIDs <- api.TaskID()
				}
				if tc.prior != nil && n == 1 {
					return ToolResult{Content: "seed", Usage: tc.prior}, nil
				}
				result := ToolResult{Content: "result", Usage: tc.usage}
				if !tc.noCallback {
					result.Commit = func(tx *Tx) error {
						apps.Add(1)
						if _, err := tx.AppendEntry(1, EntryDraft{"kind": "app.usage-settled"}); err != nil {
							return err
						}
						if tc.name == "callback-error-rolls-back" {
							return reject("test app refusal")
						}
						return nil
					}
				}
				if tc.pairedError {
					return result, fmt.Errorf("test executor error")
				}
				return result, nil
			}
			if err := registry.Register(tool); err != nil {
				t.Fatal(err)
			}
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				n := requests.Add(1)
				if n%2 == 1 {
					ch <- toolAnswer(fmt.Sprintf("call-%d", n), "usage", JSON{})
				} else {
					ch <- terminal("answer")
				}
				close(ch)
				return ch
			})
			options.Registry = registry
			store, dir := newSQLiteTestStore(t)
			h := openHarness(t, store, options)
			c := root(t, h, ref)
			send := func(c *ConversationHandle, input Input) Settlement {
				t.Helper()
				s, err := c.Submit(ctx, input)
				if err != nil {
					t.Fatal(err)
				}
				got, err := s.Wait(ctx)
				if err != nil || got.Record.Status != "done" || got.Record.Answer == 0 {
					t.Fatal("generation did not settle once with its answer", got.Record, err)
				}
				return got
			}
			assertLedger := func(c *ConversationHandle, want *goai.Usage) {
				t.Helper()
				ledger, err := c.Usage(ctx)
				got, found := ledger.Tools["usage"]
				if err != nil || found != (want != nil) || want != nil && !reflect.DeepEqual(got, *want) {
					t.Fatal("wrong tool usage ledger", got, found, want, err)
				}
			}
			wantEffects := int64(1)
			if tc.prior != nil {
				send(c, Input{Content: "seed"})
				watchReceive(t, toolIDs)
				assertLedger(c, tc.prior)
				wantEffects++
			}
			got := send(c, Input{Content: "probe", RequestID: "usage-probe"})
			toolID := watchReceive(t, toolIDs)
			verify := func(h *Harness, c *ConversationHandle) {
				t.Helper()
				if effects.Load() != wantEffects || requests.Load() != 2*wantEffects || apps.Load() != tc.appCalls {
					t.Errorf("effects=%d/%d requests=%d/%d app callbacks=%d/%d", effects.Load(), wantEffects, requests.Load(), 2*wantEffects, apps.Load(), tc.appCalls)
				}
				task, found, err := h.Task(ctx, toolID)
				if err != nil || !found || task.Owner == 0 || task.State.Status != "terminal" || task.State.Outcome == nil {
					t.Fatal("missing terminal owned tool", task, found, err)
				}
				out := task.State.Outcome
				if tc.fault != "" {
					if out.Status != "faulted" || out.Error == nil || out.Error.Message != tc.fault || out.Result != nil {
						t.Fatal("wrong tool fault or leaked result receipt", out.Status, out.Error, out.Result)
					}
				} else {
					want := "completed"
					if tc.pairedError {
						want = "failed"
						if out.Error == nil || out.Error.Message != "Tool usage threw" {
							t.Fatal("changed Go executor-error contract", out.Error)
						}
					}
					if out.Status != want || out.Result == nil || !out.Result.Present {
						t.Fatal("wrong valid outcome", out.Status, out.Result)
					}
					ref := generationObject(out.Result.Value)
					receipt, found, err := h.Entry(ctx, submissionEntryID(ref["entryId"]))
					if err != nil || !found || ref["kind"] != "model" || receipt.Kind != "pi.tool-result" || receipt.ByTask != toolID || receipt.Conversation != c.ID() || len(receipt.Model) != 1 || receipt.Model[0].IsError != tc.pairedError || !reflect.DeepEqual(receipt.Model[0].Usage, tc.usage) {
						t.Fatal("wrong attributed tool receipt", receipt, found, err)
					}
				}
				assertLedger(c, tc.total)
				entries, err := c.Entries(ctx, EntryQuery{Order: Ascending}, 24, nil)
				if err != nil || entries.Next != nil {
					t.Fatal("wrong bounded transcript", entries.Next, err)
				}
				var writes, receipts int
				for _, entry := range entries.Items {
					if entry.ByTask == toolID && entry.Kind == "pi.tool-result" {
						receipts++
					}
					if entry.Kind == "app.usage-settled" {
						if entry.ByTask != toolID {
							t.Fatal("wrong app write owner", entry)
						}
						writes++
					}
				}
				wantWrites, wantReceipts := 0, 0
				if tc.fault == "" {
					wantWrites, wantReceipts = int(tc.appCalls), 1
				}
				if writes != wantWrites || receipts != wantReceipts {
					t.Fatal("app write or tool receipt escaped atomic settlement", writes, wantWrites, receipts, wantReceipts)
				}
			}
			verify(h, c)
			if err := h.Close(ctx); err != nil {
				t.Fatal(err)
			}
			reopened, err := openSQLiteTestDirectory(dir, SQLiteOptions{})
			if err != nil {
				t.Fatal(err)
			}
			h2 := openHarness(t, reopened, options)
			c2, err := h2.Conversation(ctx, c.ID())
			if err != nil {
				t.Fatal(err)
			}
			again := send(c2, Input{RequestID: "usage-probe"})
			if again.Record.ID != got.Record.ID || again.Record.Answer != got.Record.Answer {
				t.Fatal("request identity changed on reopen", got.Record, again.Record)
			}
			verify(h2, c2)
		})
	}
}
