package durable

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// v1.6.1 harness-nested-tools.test.ts:249–283,555–599. The third case
// combines the two boundaries: replay reads an already rewritten receipt.
func TestSourceV161NestedHookAndReopenReceipts(t *testing.T) {
	for _, mode := range []string{"hooks", "reopen", "reopen-rewritten"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close(bg) })
			var mu sync.Mutex
			var echoRuns []string
			var parents []ToolInvocation
			var runs, rewrites atomic.Int32
			type attempt struct {
				caller, firstID, secondID ID
				first, second             ToolResult
			}
			observed := make(chan attempt, 2)
			echo := wrapRegistration("echo")
			echo.Execute = func(_ context.Context, args any, _ *ToolAPI) (ToolResult, error) {
				text, _ := generationObject(args)["text"].(string)
				mu.Lock()
				echoRuns = append(echoRuns, text)
				mu.Unlock()
				return ToolResult{Content: "echo " + text}, nil
			}
			batch := wrapRegistration("batch")
			batch.Execute = func(run context.Context, _ any, api *ToolAPI) (ToolResult, error) {
				n := runs.Add(1)
				invoke := func(text string) (ID, ToolResult, error) {
					call, err := api.StartToolAuto(run, "echo", JSON{"text": text})
					if err != nil {
						return 0, ToolResult{}, err
					}
					result, err := call.Wait(run)
					return call.ID(), result, err
				}
				first, second := "a", "b"
				if mode == "hooks" {
					first, second = "secret", "public"
				}
				id, result, err := invoke(first)
				if err != nil {
					return ToolResult{}, err
				}
				seen := attempt{caller: api.TaskID(), firstID: id, first: result}
				if mode != "hooks" && n == 1 {
					observed <- seen
					<-run.Done()
					return ToolResult{}, run.Err()
				}
				seen.secondID, seen.second, err = invoke(second)
				if err != nil {
					return ToolResult{}, err
				}
				observed <- seen
				if mode == "hooks" {
					return ToolResult{}, nil
				}
				return ToolResult{Content: fmt.Sprintf("run %d: %s, %s", n, seen.first.StructuredOutput, seen.second.StructuredOutput)}, nil
			}
			h, c := openNestedResultHarness(t, store, echo, batch)
			if err := h.options.Registry.Install(&Extension{Name: "inspect-nested-receipt", ToolHooks: ToolHooks{
				BeforeTool: func(run context.Context, call goai.ToolCall, api *HookAPI) (*BeforeToolResult, error) {
					if mode != "hooks" {
						return nil, nil
					}
					who, err := api.ToolInvocation(run)
					if err != nil {
						return nil, err
					}
					mu.Lock()
					parents = append(parents, who)
					mu.Unlock()
					if who.Caller == "tools" && generationObject(call.Arguments)["text"] == "secret" {
						return &BeforeToolResult{Block: "no secrets"}, nil
					}
					return nil, nil
				},
				AfterTool: func(run context.Context, call goai.ToolCall, result ToolResult, api *HookAPI) (*ToolResult, error) {
					who, err := api.ToolInvocation(run)
					if err != nil {
						return nil, err
					}
					if who.Caller != "tools" || (mode != "hooks" && (mode != "reopen-rewritten" || generationObject(call.Arguments)["text"] != "a")) {
						return nil, nil
					}
					if !result.IsError {
						rewrites.Add(1)
					}
					result.Content = ""
					result.Blocks = []goai.ContentBlock{{Type: "text", Text: "rewritten"}}
					return &result, nil
				},
			}}); err != nil {
				t.Fatal(err)
			}
			sub, err := c.Submit(ctx, Input{Type: "input", Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			first := watchReceive(t, observed)
			requireValue := func(result ToolResult, want string) {
				t.Helper()
				if result.IsError || !result.HasStructuredOutput || result.StructuredOutput != want {
					t.Fatal("wrong nested structured value", result, want)
				}
			}
			if mode == "hooks" {
				want := []ToolDiagnostic{{Severity: "error", Code: "blocked", Message: "Tool call blocked: no secrets"}}
				if !first.first.IsError || !slices.Equal(first.first.Diagnostics, want) {
					t.Fatal("wrong blocked nested result", first.first)
				}
				requireValue(first.second, "rewritten")
			} else {
				want := "echo a"
				if mode == "reopen-rewritten" {
					want = "rewritten"
				}
				requireValue(first.first, want)
				child, found, err := store.Task(ctx, first.firstID)
				if err != nil || !found || child.Owner != first.caller || child.State.Status != "terminal" || child.State.Outcome == nil || child.State.Outcome.Status != "completed" || child.State.Outcome.Result == nil || generationObject(child.State.Outcome.Result.Value)["kind"] != "nested" {
					t.Fatal("expected actual committed nested child before close", child, found, err)
				}
				if err := h.Close(ctx); err != nil {
					t.Fatal(err)
				}
				h = openHarness(t, reopenStoreAfterHarnessClose(t, store), h.options)
				c, err = h.Conversation(ctx, c.ID())
				if err != nil || c == nil {
					t.Fatal("reopen conversation", err)
				}
				sub, err = h.Submission(ctx, sub.ID())
				if err != nil || sub == nil {
					t.Fatal("reopen submission", err)
				}
				if err := h.Resume(ctx); err != nil {
					t.Fatal(err)
				}
				second := watchReceive(t, observed)
				if second.caller != first.caller || second.firstID != first.firstID || second.secondID == 0 || second.secondID == first.firstID {
					t.Fatal("reopened caller failed to reuse the existing child and start a different second one", first, second)
				}
				requireValue(second.first, want)
				requireValue(second.second, "echo b")
				if result, err := sub.Wait(ctx); err != nil || result.Record.Status != "done" {
					t.Fatal("reopened submission did not finish", result.Record, err)
				}
				raw := v160ToolResult(t, ctx, c, "c1")
				if raw.Role != goai.RoleToolResult || raw.IsError || v160ToolResultText(raw) != "run 2: "+want+", echo b" {
					t.Fatal("wrong raw model result text (not the nested structured value)", raw)
				}
			}
			if err := c.WaitForIdle(ctx); err != nil {
				t.Fatal(err)
			}
			tasks := nestedKeyToolTasks(t, c)
			if len(tasks) != 3 {
				t.Fatal("expected only caller and two nested tasks", tasks)
			}
			if mode != "hooks" {
				nested, firstResultNested := 0, false
				for _, task := range tasks {
					if task.Input != nil && task.Input.Present && generationObject(task.Input.Value)["kind"] == "nested" {
						nested++
					}
					if task.ID == first.firstID && task.State.Status == "terminal" && task.State.Outcome != nil && task.State.Outcome.Result != nil {
						firstResultNested = generationObject(task.State.Outcome.Result.Value)["kind"] == "nested"
					}
				}
				if nested != 2 || !firstResultNested {
					t.Fatalf("after reopen: nested inputs=%d, first result nested=%v; want two nested inputs and the saved nested result", nested, firstResultNested)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if mode == "hooks" {
				raw := v160ToolResult(t, ctx, c, "c1")
				if raw.Role != goai.RoleToolResult || raw.IsError || v160ToolResultText(raw) != "" {
					t.Fatal("nested rewrite changed the raw caller result", raw)
				}
				if len(parents) != 3 || parents[0].Caller != "model" || parents[0].ParentCallID != "" || parents[1].Caller != "tools" || parents[2].Caller != "tools" || parents[1].ParentTask != first.caller || parents[2].ParentTask != first.caller || parents[1].ParentCallID != "c1" || parents[2].ParentCallID != "c1" || !slices.Equal(echoRuns, []string{"public"}) || rewrites.Load() != 1 || runs.Load() != 1 {
					t.Fatal("wrong hook parent identities or exact invocation counts", parents, echoRuns, rewrites.Load(), runs.Load())
				}
			} else {
				var wantRewrites int32
				if mode == "reopen-rewritten" {
					wantRewrites = 1
				}
				if !slices.Equal(echoRuns, []string{"a", "b"}) || rewrites.Load() != wantRewrites || runs.Load() != 2 {
					t.Fatal("recovery reran the first child or its hook", echoRuns, rewrites.Load(), runs.Load())
				}
			}
		})
	}
}
