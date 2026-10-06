package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
	"time"
)

func TestToolProgressCoalescesFirehosePreservesMemoAndTerminalFlush(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		limits := ToolOutputLimits{64, 2, "tail"}
		registry := NewRegistry()
		visible, release := make(chan struct{}), make(chan struct{})
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "firehose", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "firehose.test", Version: 1, OutputLimits: &limits, Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
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
						if m.Content[0].Text != "line\nline\n" || !strings.Contains(m.Content[1].Text, "99 lines, 496 bytes dropped") {
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
		// Details waits for its committed batch. Pause the host until the pending
		// firehose window and memo have both adopted; no timing-dependent sleeps.
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		var toolID ID
		for {
			snapshot, err := h.Snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			ready := false
			for _, task := range snapshot.Tasks {
				if task.Kind == "pi.tool" {
					toolID = task.ID
					var cp toolCheckpoint
					if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
						t.Fatal(err)
					}
					if cp.HasDetails && task.Execution != nil && task.Execution.Builtin.Memos["progress.memo"] != nil {
						ready = true
					}
				}
			}
			if ready {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("memo admission absent")
			default:
			}
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
		task := after.Tasks[toolID]
		var cp toolCheckpoint
		if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
			t.Fatal(err)
		}
		if cp.Output != "line\nline\n" || cp.DroppedBytes != 496 || cp.DroppedLines != 99 || !cp.HasDetails || cp.Diagnostics[len(cp.Diagnostics)-1].Code != "truncated" {
			t.Fatal("retained terminal progress", cp)
		}
		if after.Seq-before.Seq >= 100 {
			t.Fatal("output marks committed individually", after.Seq-before.Seq)
		}
	})
}
