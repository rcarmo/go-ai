package durable

import (
	"context"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestGenerationAbortConvertsOnlyCommittedPartialAndNoSyntheticAssistant(t *testing.T) {
	for _, partial := range []bool{false, true} {
		backends(t, func(t *testing.T, b backend) {
			entered := make(chan struct{})
			ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event)
				go func() { close(entered); <-ctx.Done(); close(ch) }()
				return ch
			})
			h := openHarness(t, b.store, options)
			conversation := root(t, h, ref)
			sub, err := conversation.Submit(bg, Input{Content: "abort"})
			if err != nil {
				t.Fatal(err)
			}
			awaitTaskSignal(t, entered)
			if partial {
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				for _, task := range state.Tasks {
					if task.Kind != "pi.generation" {
						continue
					}
					var cp generationCheckpoint
					if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
						t.Fatal(err)
					}
					if err := h.commitPartial(task.ID, MessageReceipt{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "committed partial"}}, Api: cp.Model.Api, Provider: cp.Model.Provider, Model: cp.Model.ID, Usage: &goai.Usage{Output: 2, TotalTokens: 2}}); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := conversation.Abort(bg); err != nil {
				t.Fatal(err)
			}
			settled := waitSubmission(t, sub)
			if settled.Submission.Status != "aborted" {
				t.Fatal(settled)
			}
			state, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, entry := range state.Entries {
				if entry.Value["role"] != string(goai.RoleAssistant) {
					continue
				}
				count++
				var receipt MessageReceipt
				if err := fromObject(entry.Value, &receipt, h.session.limits); err != nil || receipt.StopReason != goai.StopReasonAborted || receipt.Content[0].Text != "committed partial" || receipt.ErrorCode != "" {
					t.Fatal("partial changed on abort", receipt, err)
				}
			}
			want := 0
			if partial {
				want = 1
			}
			if count != want || (settled.Message != nil) != partial {
				t.Fatal("abort synthesized/lost assistant", count, want, settled)
			}
			if err := h.Close(bg); err != nil {
				t.Fatal(err)
			}
			h = openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
			handle, err := h.Submission(bg, sub.ID())
			if err != nil {
				t.Fatal(err)
			}
			if reopened := waitSubmission(t, handle); reopened.Submission.Status != "aborted" || (reopened.Message != nil) != partial {
				t.Fatal("abort reopen disagreement", reopened)
			}
		})
	}
}
