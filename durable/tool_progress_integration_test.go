package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func TestToolProgressCoalescesFirehosePreservesMemoAndTerminalFlush(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		limits := ToolOutputLimits{64, 2, "tail"}
		registry := NewRegistry()
		visible, release := make(chan struct{}), make(chan struct{})
		memoCommitted := make(chan ID, 1)
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "firehose", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "firehose.test", Version: 1, OutputLimits: &limits, Execute: func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
			if err := api.Output("first\n"); err != nil {
				return ToolResult{}, err
			}
			if err := api.Details(JSON{"committed": true}); err != nil {
				return ToolResult{}, err
			}
			close(visible)
			for i := 0; i < 100; i++ {
				if err := api.Output("line\n"); err != nil {
					return ToolResult{}, err
				}
			}
			if _, err := api.MemoCandidate(ctx, "progress.memo", JSON{"preserved": true}); err != nil {
				return ToolResult{}, err
			}
			memoCommitted <- api.TaskID()
			<-release
			return ToolResult{}, nil
		}}); err != nil {
			t.Fatal(err)
		}
		calls := 0
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			calls++
			ch := make(chan goai.Event, 1)
			if calls == 1 {
				ch <- toolAnswer("firehose-call", "firehose", JSON{})
			} else {
				for _, m := range input.Messages {
					if m.Role == goai.RoleToolResult {
						if len(m.Content) != 2 || m.Content[0].Text != "line\nline\n" || !strings.Contains(m.Content[1].Text, "99 lines, 496 bytes dropped") {
							t.Error("terminal firehose flush", m)
						}
					}
				}
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		c := root(t, h, ref)
		sub, err := c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, visible)
		// Memo completion is an adopted-publication signal, not a polling loop.
		toolID := watchReceive(t, memoCommitted)
		record, found, err := h.Task(bg, toolID)
		if err != nil || !found || record.Memos["progress.memo"] == nil || !record.Memos["progress.memo"].Present || !equalJSONValue(record.Memos["progress.memo"].Value, JSON{"preserved": true}) {
			t.Fatal("memo admission absent", record, err)
		}

		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(release)
		waitSubmission(t, sub)
		after, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		receipt := canonicalModelToolReceipt(t, h, toolID)
		if len(receipt.Content) != 2 || receipt.Content[0].Text != "line\nline\n" || !receipt.HasDetails || !equalJSONValue(receipt.DetailsValue, JSON{"committed": true}) || len(receipt.Diagnostics) != 1 || receipt.Diagnostics[0].Code != "truncated" || receipt.Diagnostics[0].Message != "Output truncated to its end: 99 lines, 496 bytes dropped" {
			t.Fatal("retained terminal progress", receipt)
		}

		if after.Seq-before.Seq >= 100 {
			t.Fatal("output marks committed individually", after.Seq-before.Seq)
		}
	})
}
