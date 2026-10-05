package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAgentSelectionAndHooksAreDetachedRequestLocalAndOrdered(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var before, after, afterTools, beforeTool, afterTool atomic.Int64
		var escaped *HookAPI
		extension := &Extension{Name: "delegate", Sections: []PromptSection{{Key: "worker", Render: func(context.Context, PromptInput) (*string, error) { return promptString("worker section"), nil }}}}
		extension.Tools = []ToolRegistration{{Definition: goai.Tool{Name: "echo", Parameters: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)}, Implementation: "hooks.echo", Version: 1, Execute: func(_ context.Context, args JSON, _ *ToolAPI) (ToolResult, error) {
			if args["text"] != "rewritten" {
				t.Error("beforeTool replacement lost", args)
			}
			return ToolResult{Content: "raw result", Details: JSON{"nested": JSON{"value": "original"}}}, nil
		}}}
		extension.Hooks = GenerationHooks{
			BeforeRequest: func(ctx context.Context, messages []MessageReceipt, api *HookAPI) ([]MessageReceipt, error) {
				before.Add(1)
				escaped = api
				if _, err := api.MemoCandidate(ctx, "hook.request", JSON{"set": true}); err != nil {
					return nil, err
				}
				messages = append(messages, userReceipt("request only"))
				return messages, nil
			}, AfterResponse: func(_ context.Context, message MessageReceipt, _ *HookAPI) error {
				after.Add(1)
				if len(message.Content) > 0 {
					message.Content[0].Text = "mutated response"
				}
				return nil
			},
			AfterTools: func(_ context.Context, messages []MessageReceipt, _ *HookAPI) error {
				afterTools.Add(1)
				if len(messages) != 1 || messages[0].ToolCallID != "hook-call" || messages[0].Content[0].Text != "hook result" {
					t.Error("afterTools result order", messages)
				}
				messages[0].Content[0].Text = "mutated result"
				return nil
			},
		}
		extension.ToolHooks = ToolHooks{BeforeTool: func(_ context.Context, call goai.ToolCall, _ *HookAPI) (*BeforeToolResult, error) {
			beforeTool.Add(1)
			return &BeforeToolResult{Arguments: JSON{"text": "rewritten"}}, nil
		}, AfterTool: func(_ context.Context, _ goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
			afterTool.Add(1)
			result.Content = "hook result"
			return &result, nil
		}}
		if err := registry.Install(extension); err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			number := calls.Add(1)
			ch := make(chan goai.Event, 1)
			if number <= 2 {
				if len(input.Tools) != 1 || input.Tools[0].Name != "echo" || !strings.Contains(input.SystemPrompt, "worker section") {
					t.Error("selected extension missing", input)
				}
				if input.Messages[len(input.Messages)-1].Content[0].Text != "request only" {
					t.Error("request hook not applied", input.Messages)
				}
				if number == 1 {
					ch <- toolAnswer("hook-call", "echo", JSON{"text": "model"})
				} else {
					ch <- terminal("unmodified answer")
				}
			} else {
				if len(input.Tools) != 0 || strings.Contains(input.SystemPrompt, "worker section") {
					t.Error("child loadout leaked", input)
				}
				for _, message := range input.Messages {
					for _, block := range message.Content {
						if block.Text == "request only" {
							t.Error("disabled child hook ran")
						}
					}
				}
				ch <- terminal("child answer")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		parent := root(t, h, ref)
		sub, err := parent.Submit(bg, Input{Content: "parent"})
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, sub)
		if result.Message == nil || len(result.Message.Content) == 0 || result.Message.Content[0].Text != "unmodified answer" {
			t.Fatal("response mutation escaped", result)
		}
		if before.Load() != 2 || after.Load() != 2 || beforeTool.Load() != 1 || afterTool.Load() != 1 || afterTools.Load() != 1 {
			t.Fatal("hook counts", before.Load(), after.Load(), beforeTool.Load(), afterTool.Load(), afterTools.Load())
		}
		if _, _, err := escaped.Memo(bg, "hook.request"); !errors.Is(err, ErrSealed) {
			t.Fatal("escaped hook still usable", err)
		}
		none := []string{}
		child, err := h.CreateConversation(bg, AgentChange{Model: ref, Extensions: &none, Tools: &none})
		if err != nil {
			t.Fatal(err)
		}
		none = append(none, "delegate") // caller owns the source slice after configure.
		childInput, err := child.Submit(bg, Input{Content: "child"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, childInput)
		if before.Load() != 2 || after.Load() != 2 {
			t.Fatal("disabled extension hooks ran")
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range state.Entries {
			for _, message := range entry.Model {
				for _, block := range message.Content {
					if block.Text == "request only" {
						t.Fatal("hook replacement persisted")
					}
				}
			}
			if entry.Kind == "message" {
				var message MessageReceipt
				if err := fromObject(entry.Value, &message, h.session.limits); err != nil {
					t.Fatal(err)
				}
				for _, block := range message.Content {
					if block.Text == "request only" || block.Text == "mutated response" || block.Text == "mutated result" {
						t.Fatal("hook mutation persisted", block)
					}
				}
			}
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		handle, err := second.Conversation(bg, child.ID())
		if err != nil {
			t.Fatal(err)
		}
		another, err := handle.Submit(bg, Input{Content: "child after reopen"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, another)
		if before.Load() != 2 {
			t.Fatal("selection lost on reopen")
		}
	})
}
func TestBeforeToolBlockOrThrowPreventsEffectAndRevalidatesReplacement(t *testing.T) {
	for _, mode := range []string{"block", "panic", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			var executed atomic.Int64
			registry := NewRegistry()
			tool := ToolRegistration{Definition: goai.Tool{Name: "guarded", Parameters: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`)}, Implementation: "hooks.guard", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
				executed.Add(1)
				return ToolResult{Content: "effect"}, nil
			}}
			if err := registry.Install(&Extension{Name: "guard", Tools: []ToolRegistration{tool}, ToolHooks: ToolHooks{BeforeTool: func(context.Context, goai.ToolCall, *HookAPI) (*BeforeToolResult, error) {
				if mode == "panic" {
					panic("host secret")
				}
				if mode == "block" {
					return &BeforeToolResult{Block: "do not run"}, nil
				}
				return &BeforeToolResult{Arguments: JSON{"n": "invalid"}}, nil
			}}}); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int64
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if requests.Add(1) == 1 {
					ch <- toolAnswer("guard-call", "guarded", JSON{"n": 1})
				} else {
					ch <- terminal("blocked")
				}
				close(ch)
				return ch
			})
			options.Registry = registry
			store, _ := NewMemory()
			h := openHarness(t, store, options)
			conversation := root(t, h, ref)
			sub, err := conversation.Submit(bg, Input{Content: "run"})
			if err != nil {
				t.Fatal(err)
			}
			waitSubmission(t, sub)
			state, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, entry := range state.Entries {
				if entry.Kind != "message" {
					continue
				}
				var message MessageReceipt
				if err := fromObject(entry.Value, &message, h.session.limits); err != nil {
					t.Fatal(err)
				}
				if message.Role == goai.RoleToolResult {
					found = true
					code := "tool_blocked"
					if mode == "invalid" {
						code = "invalid_tool_arguments"
					}
					if message.ErrorCode != code {
						t.Fatal(message)
					}
				}
			}
			if !found || executed.Load() != 0 {
				t.Fatal("hook allowed effect", found, executed.Load())
			}
		})
	}
}

func TestOnYieldContinuationUsesSameRunButQueuedUserWins(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(fmt.Sprint(queued), func(t *testing.T) {
			registry := NewRegistry()
			entered, release := make(chan struct{}), make(chan struct{})
			var yields, requests atomic.Int64
			if err := registry.Install(&Extension{Name: "yield", Hooks: GenerationHooks{OnYield: func(ctx context.Context, message MessageReceipt, _ *HookAPI) (*Input, error) {
				if yields.Add(1) == 1 {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
					return &Input{Content: "continue hook"}, nil
				}
				return nil, nil
			}}}); err != nil {
				t.Fatal(err)
			}
			ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				n := requests.Add(1)
				if n == 2 {
					last := input.Messages[len(input.Messages)-1].Content[0].Text
					want := "continue hook"
					if queued {
						want = "queued user"
					}
					if last != want {
						t.Error("yield precedence", last, want)
					}
				}
				ch := make(chan goai.Event, 1)
				ch <- terminal(fmt.Sprint("answer ", n))
				close(ch)
				return ch
			})
			options.Registry = registry
			store, _ := NewMemory()
			h := openHarness(t, store, options)
			cleanupTaskGates(t, release)
			conversation := root(t, h, ref)
			first, err := conversation.Submit(bg, Input{Content: "first"})
			if err != nil {
				t.Fatal(err)
			}
			awaitTaskSignal(t, entered)
			var follow *SubmissionHandle
			if queued {
				follow, err = conversation.Submit(bg, Input{Content: "queued user"})
				if err != nil {
					t.Fatal(err)
				}
			}
			releaseTaskGate(release)
			result := waitSubmission(t, first)
			if queued {
				if result.Message.Content[0].Text != "answer 1" {
					t.Fatal("queued user did not end original", result)
				}
				next := waitSubmission(t, follow)
				if next.Task.ID == result.Task.ID {
					t.Fatal("queued user merged with hook continuation")
				}
			} else if result.Message.Content[0].Text != "answer 2" {
				t.Fatal("original input did not wait continuation", result)
			}
			if requests.Load() != 2 {
				t.Fatal("yield requests", requests.Load())
			}
			state, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			if !queued && len(state.Tasks) != 1 {
				t.Fatal("yield created another run", state.Tasks)
			}
		})
	}
}

func TestBeforeCompactDecisionDetachmentAndFirstWins(t *testing.T) {
	for _, mode := range []string{"summary", "decline", "panic"} {
		t.Run(mode, func(t *testing.T) {
			registry := NewRegistry()
			var first, later, requests, reports atomic.Int64
			if err := registry.Install(&Extension{Name: "first", CompactionHooks: CompactionHooks{BeforeCompact: func(ctx context.Context, input CompactionInput, api *HookAPI) (*CompactionDecision, error) {
				first.Add(1)
				if input.Reason != "manual" || input.FirstKept == 0 || len(input.Messages) != 2 || len(input.Entries) != 2 {
					t.Error("compaction hook selection", input)
				}
				input.Messages[0].Content[0].Text = "mutated"
				input.Entries[0].Value["changed"] = true
				if _, err := api.MemoCandidate(ctx, "compaction.hook", true); err != nil {
					return nil, err
				}
				if mode == "panic" {
					panic("secret hook")
				}
				if mode == "decline" {
					return &CompactionDecision{Decline: true}, nil
				}
				summary := "hook summary"
				return &CompactionDecision{Summary: &summary}, nil
			}}}); err != nil {
				t.Fatal(err)
			}
			if err := registry.Install(&Extension{Name: "later", CompactionHooks: CompactionHooks{BeforeCompact: func(context.Context, CompactionInput, *HookAPI) (*CompactionDecision, error) {
				later.Add(1)
				return nil, nil
			}}}); err != nil {
				t.Fatal(err)
			}
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				requests.Add(1)
				ch := make(chan goai.Event, 1)
				ch <- terminal("provider summary")
				close(ch)
				return ch
			})
			options.Registry = registry
			options.OnReport = func(error) { reports.Add(1) }
			store, _ := NewMemory()
			h := openHarness(t, store, options)
			conversation := root(t, h, ref)
			_, err := conversation.Commit(bg, func(tx *Tx) error {
				for _, text := range []string{"old", "answer", "recent"} {
					id, err := tx.MintID()
					if err != nil {
						return err
					}
					value, err := dtoObject(userReceipt(text), tx.limits)
					if err != nil {
						return err
					}
					if err = tx.AppendEntry(Entry{ID: id, Conversation: conversation.ID(), Kind: "message", Value: value}); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			id, err := conversation.Compact(bg, CompactionOptions{KeepRecentTokens: goai.EstimateMessageTokens(receiptMessage(userReceipt("recent")))})
			if err != nil {
				t.Fatal(err)
			}
			result := waitPublicTask(t, h, id)
			if result.State.Outcome.Status != "completed" || first.Load() != 1 {
				t.Fatal(result, first.Load())
			}
			wantLater, wantRequests, wantReports := int64(0), int64(0), int64(0)
			if mode == "panic" {
				wantLater, wantRequests, wantReports = 1, 1, 1
			}
			if later.Load() != wantLater || requests.Load() != wantRequests || reports.Load() != wantReports {
				t.Fatal("compaction hook dispatch", later.Load(), requests.Load(), reports.Load())
			}
			state, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range state.Entries {
				if entry.Value["changed"] != nil {
					t.Fatal("compaction entries escaped")
				}
			}
			view, err := conversation.ContextView(bg, 0)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "decline" {
				if view.Head != nil {
					t.Fatal("declined compaction placed")
				}
			} else {
				want := "hook summary"
				if mode == "panic" {
					want = "provider summary"
				}
				if view.Head == nil || !strings.Contains(view.Messages[0].Content[0].Text, want) {
					t.Fatal("summary decision lost", view)
				}
			}
		})
	}
}
func TestBeforeToolIntentSurvivesReopenWithoutHookReplay(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var before, effects atomic.Int64
		entered := make(chan struct{})
		if err := registry.Install(&Extension{Name: "replay", Tools: []ToolRegistration{{Definition: goai.Tool{Name: "effect", Parameters: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)}, Implementation: "hook.replay", Version: 1, ReplaySafe: true, Execute: func(ctx context.Context, args JSON, _ *ToolAPI) (ToolResult, error) {
			if args["text"] != "intent" {
				t.Error("recovered rewritten args", args)
			}
			if effects.Add(1) == 1 {
				close(entered)
				<-ctx.Done()
				return ToolResult{}, ctx.Err()
			}
			return ToolResult{Content: "effect done"}, nil
		}}}, ToolHooks: ToolHooks{BeforeTool: func(ctx context.Context, call goai.ToolCall, api *HookAPI) (*BeforeToolResult, error) {
			before.Add(1)
			if _, err := api.MemoCandidate(ctx, "intent-hook", true); err != nil {
				return nil, err
			}
			return &BeforeToolResult{Arguments: JSON{"text": "intent"}}, nil
		}}}); err != nil {
			t.Fatal(err)
		}
		var requests atomic.Int64
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				ch <- toolAnswer("replay-call", "effect", JSON{"text": "model"})
			} else {
				ch <- terminal("answer")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		first := openHarness(t, b.store, options)
		conversation := root(t, first, ref)
		sub, err := conversation.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		if err := first.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		handle, err := second.Submission(bg, sub.ID())
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, handle)
		if result.Submission.Status != "done" || before.Load() != 1 || effects.Load() != 2 || requests.Load() != 2 {
			t.Fatal("beforeTool replayed", result, before.Load(), effects.Load(), requests.Load())
		}
	})
}

func TestBeforeRequestRecoveryRunsAgainWithoutPersistingRewrite(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var hooks, requests atomic.Int64
		entered := make(chan struct{})
		if err := registry.Install(&Extension{Name: "request", Hooks: GenerationHooks{BeforeRequest: func(ctx context.Context, messages []MessageReceipt, api *HookAPI) ([]MessageReceipt, error) {
			hooks.Add(1)
			if _, err := api.MemoCandidate(ctx, "first-request", JSON{"candidate": "same"}); err != nil {
				return nil, err
			}
			messages = append(messages, userReceipt("temporary rewrite"))
			return messages, nil
		}}}); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			n := requests.Add(1)
			if input.Messages[len(input.Messages)-1].Content[0].Text != "temporary rewrite" {
				t.Error("recovery request not transformed", input.Messages)
			}
			ch := make(chan goai.Event, 1)
			if n == 1 {
				go func() { close(entered); <-ctx.Done(); close(ch) }()
			} else {
				ch <- terminal("after reopen")
				close(ch)
			}
			return ch
		})
		options.Registry = registry
		first := openHarness(t, b.store, options)
		conversation := root(t, first, ref)
		sub, err := conversation.Submit(bg, Input{Content: "original"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		state, err := first.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			if task.Kind == "pi.generation" {
				var cp generationCheckpoint
				if err := fromObject(task.Checkpoint, &cp, first.session.limits); err != nil {
					t.Fatal(err)
				}
				if cp.Messages[len(cp.Messages)-1].Content[0].Text != "original" {
					t.Fatal("transient rewrite became request intent", cp)
				}
			}
		}
		if err := first.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		handle, err := second.Submission(bg, sub.ID())
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, handle)
		if result.Submission.Status != "done" || hooks.Load() != 2 || requests.Load() != 2 {
			t.Fatal("request hook recovery", result, hooks.Load(), requests.Load())
		}
	})
}

func TestPostToolsRefreshesAgentLoadoutAndPromptWithoutDuplicatingInput(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		entered, release := make(chan struct{}), make(chan struct{})
		tool := ToolRegistration{Definition: goai.Tool{Name: "held", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "hooks.refresh", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			close(entered)
			<-release
			return ToolResult{Content: "finished"}, nil
		}}
		if err := registry.Install(&Extension{Name: "old", Tools: []ToolRegistration{tool}, Sections: []PromptSection{{Key: "loadout", Render: func(context.Context, PromptInput) (*string, error) { return promptString("old section"), nil }}}}); err != nil {
			t.Fatal(err)
		}
		if err := registry.Install(&Extension{Name: "new", Sections: []PromptSection{{Key: "loadout", Render: func(context.Context, PromptInput) (*string, error) { return promptString("new section"), nil }}}}); err != nil {
			t.Fatal(err)
		}
		var requests atomic.Int64
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			n := requests.Add(1)
			ch := make(chan goai.Event, 1)
			if n == 1 {
				if len(input.Tools) != 1 || !strings.Contains(input.SystemPrompt, "old section") {
					t.Error("initial loadout", input)
				}
				ch <- toolAnswer("held-call", "held", JSON{})
			} else {
				if len(input.Tools) != 0 || strings.Contains(input.SystemPrompt, "old section") || !strings.Contains(input.SystemPrompt, "new section") {
					t.Error("posttools loadout stale", input)
				}
				inputs := 0
				for _, message := range input.Messages {
					if message.Role == goai.RoleUser {
						for _, block := range message.Content {
							if block.Text == "original" {
								inputs++
							}
						}
					}
				}
				if inputs != 1 {
					t.Error("duplicated user at continuation", inputs)
				}
				ch <- terminal("answer")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		oldSelection, newSelection := []string{"old"}, []string{"new"}
		conversation, err := h.Root(bg, AgentChange{Model: ref, Extensions: &oldSelection})
		if err != nil {
			t.Fatal(err)
		}
		sub, err := conversation.Submit(bg, Input{Content: "original"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		if err := conversation.Configure(bg, AgentChange{Model: ref, Extensions: &newSelection}); err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(release)
		result := waitSubmission(t, sub)
		if result.Submission.Status != "done" || requests.Load() != 2 {
			t.Fatal(result, requests.Load())
		}
	})
}
