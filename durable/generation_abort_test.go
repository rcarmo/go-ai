package durable

import (
	"context"
	"reflect"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// harness-generation.test.ts:306: converts the committed partial when aborted
// during streaming. No-partial, usage and reopen are supplemental source probes.
func TestGenerationAbortConvertsOnlyCommittedPartialAndNoSyntheticAssistant(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "no-partial"
		if partial {
			name = "partial"
		}
		t.Run(name, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				entered := make(chan struct{})
				ref, options := setupFakeAPI(t, func(ctx context.Context, model *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					if partial {
						ch <- &goai.TextDeltaEvent{ContentIndex: 0, Delta: "committed partial", Partial: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "committed partial"}}, Api: model.Api, Provider: model.Provider, Model: model.ID, Usage: &goai.Usage{Output: 2, TotalTokens: 2}}}
					}
					go func() { close(entered); <-ctx.Done(); close(ch) }()
					return ch
				})
				zero := int64(0)
				window := float64(0)
				options.Settings = &HarnessSettings{Progress: &ProgressSettings{PartialIntervalMs: &zero, PersistWithinMs: &window}}
				h := openHarness(t, b.store, options)
				conversation := root(t, h, ref)
				publications := recordGenerationPublications(t, h)
				sub, err := conversation.Submit(bg, Input{Content: "abort"})
				if err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered)
				if partial {
					deadline, cancel := context.WithTimeout(bg, 3*time.Second)
					defer cancel()
				waitPartial:
					for {
						select {
						case live := <-publications.live:
							if generationObject(generationObject(live["generation"])["message"]) != nil {
								break waitPartial
							}
						case <-deadline.Done():
							t.Fatal("no stored partial")
						}
					}
				}
				view, err := conversation.View(bg)
				if err != nil {
					t.Fatal(err)
				}
				live, _ := view.Docs.Get("pi.live")
				run, _ := live.Get("run")
				taskID, _ := run.Get("taskId")
				id := submissionEntryID(taskID.Copy())
				if id == 0 {
					t.Fatal("missing public run task", live.Copy())
				}
				if status, err := h.AbortTask(bg, id); err != nil || status != "marked" {
					t.Fatal(status, err)
				}
				settled := waitSubmission(t, sub)
				if settled.Record.Status != "unanswered" || settled.Record.Reason != "aborted" {
					t.Fatal(settled.Record)
				}
				ended, err := h.WaitForTask(bg, id)
				if err != nil || !reflect.DeepEqual(ended.State.Outcome, &TaskOutcome{Status: "aborted"}) {
					t.Fatal("abort outcome", ended, err)
				}
				view, err = conversation.View(bg)
				if err != nil {
					t.Fatal(err)
				}
				live, _ = view.Docs.Get("pi.live")
				if !equalJSONValue(live.Copy(), JSON{"runFailure": JSON{"taskId": int(id), "inputs": []any{int(sub.ID())}, "reason": "aborted", "first": int(settled.Record.Entry)}}) {
					t.Fatal("aborted live", live.Copy())
				}
				kinds := []string{}
				for i := 0; i < view.Entries.Len(); i++ {
					entry, _ := view.Entries.Index(i)
					kind, _ := entry.Get("kind")
					kinds = append(kinds, kind.String())
					if kind.String() != "pi.assistant" {
						continue
					}
					model, _ := entry.Get("model")
					wire, _ := model.Index(0)
					var receipt MessageReceipt
					if err := fromObject(JSON(wire.Copy().(map[string]any)), &receipt, DefaultLimits()); err != nil || receipt.StopReason != goai.StopReasonAborted || len(receipt.Content) != 1 || receipt.Content[0].Text != "committed partial" || receipt.ErrorCode != "" {
						t.Fatal("partial changed on abort", receipt, err)
					}
				}
				want := []string{"pi.user"}
				if partial {
					want = append(want, "pi.assistant")
				}
				if !reflect.DeepEqual(kinds, want) {
					t.Fatal("abort synthesized/lost assistant", kinds, want)
				}
				usage, err := conversation.Usage(bg)
				if err != nil {
					t.Fatal(err)
				}
				output, total := 0, 0
				for _, u := range usage.Models {
					output += u.Output
					total += u.TotalTokens
				}
				expected := 0
				if partial {
					expected = 2
				}
				if output != expected || total != expected {
					t.Fatal("partial usage", usage)
				}
				if err := h.Close(bg); err != nil {
					t.Fatal(err)
				}
				h = openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
				handle, err := h.Submission(bg, sub.ID())
				if err != nil {
					t.Fatal(err)
				}
				reopened := waitSubmission(t, handle)
				if !reflect.DeepEqual(reopened.Record, settled.Record) {
					t.Fatal("abort reopen disagreement", reopened.Record, settled.Record)
				}
				restored, err := h.Conversation(bg, conversation.ID())
				if err != nil {
					t.Fatal(err)
				}
				reopenedView, err := restored.View(bg)
				if err != nil || !equalJSONValue(reopenedView.Entries.Copy(), view.Entries.Copy()) {
					t.Fatal("reopen transcript", reopenedView, err)
				}
				reopenedUsage, err := restored.Usage(bg)
				if err != nil || !reflect.DeepEqual(reopenedUsage, usage) {
					t.Fatal("reopen usage", reopenedUsage, usage, err)
				}
			})
		})
	}
}
