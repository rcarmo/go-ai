package durable

import (
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestToolDiagnosticsCommittedDetachedAndRenderedOutsideDetails(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		registry := NewRegistry()
		var escaped *ToolAPI
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "diagnostic", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "diagnostic.test", Version: 1, Execute: func(_ context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			escaped = api
			if err := api.Diagnostic(ToolDiagnostic{Severity: "warn", Code: "remark", Message: "partial remark"}); err != nil {
				return ToolResult{}, err
			}
			if err := api.Details(nil); err != nil {
				return ToolResult{}, err
			}
			return ToolResult{Content: "data", Details: JSON{"owned": true}, Diagnostics: []ToolDiagnostic{{Severity: "info", Message: "final remark"}}, IsError: true}, nil
		}}); err != nil {
			t.Fatal(err)
		}
		calls := 0
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			calls++
			ch := make(chan goai.Event, 1)
			if calls == 1 {
				ch <- toolAnswer("diagnostic-call", "diagnostic", JSON{})
			} else {
				found := false
				for _, m := range input.Messages {
					if m.Role == goai.RoleToolResult {
						found = true
						if !m.IsError || len(m.Content) != 2 || m.Content[0].Text != "data" || m.Content[1].Text != "<harness>\n[warn] partial remark\n[info] final remark\n</harness>" || !equalJSONValue(m.Details, JSON{"owned": true}) {
							t.Error("diagnostic receipt", m)
						}
					}
				}
				if !found {
					t.Error("result absent")
				}
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		watch, err := h.WatchEvents(bg, c.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		var mu sync.Mutex
		seen := []ToolDiagnostic{}
		done := make(chan struct{}, 1)
		if err := watch.Start(func(_ context.Context, events []AgentEvent) error {
			mu.Lock()
			defer mu.Unlock()
			for _, event := range events {
				seen = append(seen, event.Diagnostics...)
				if len(event.Diagnostics) > 0 {
					event.Diagnostics[0].Message = "mutated"
				}
				if event.Type == "run_end" {
					select {
					case done <- struct{}{}:
					default:
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		sub, err := c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("event run end")
		}
		mu.Lock()
		if len(seen) != 1 || seen[0].Message != "partial remark" {
			t.Error("diagnostic updates", seen)
		}
		mu.Unlock()
		if err := escaped.Diagnostic(ToolDiagnostic{Severity: "info", Message: "late"}); !errors.Is(err, ErrSealed) {
			t.Fatal("escaped diagnostic live", err)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			if task.Kind == "pi.tool" {
				receipt := canonicalModelToolReceipt(t, h, task.ID)
				want := []ToolDiagnostic{{Severity: "warn", Code: "remark", Message: "partial remark"}, {Severity: "info", Message: "final remark"}}
				if !reflect.DeepEqual(receipt.Diagnostics, want) {
					t.Fatal("diagnostic alias or ordering", receipt.Diagnostics, want)
				}

			}
		}
	})
}

// Source tool.ts diagnostic/finalResult/appendToolResult only copies strict JSON.
// Paired with the independent original-runtime diagnostic-severity probe.
func TestToolDiagnosticJSONSeveritiesPreserveProgressAndResult(t *testing.T) {
	for _, severity := range []string{"invalid", "", "ERROR", "🧪", "error"} {
		name := severity
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				var h *Harness
				registry := NewRegistry()
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "diagnostic_json", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "diagnostic.json", Version: 1, Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
					if err := api.Diagnostic(ToolDiagnostic{Severity: severity, Message: "progress"}); err != nil {
						return ToolResult{}, err
					}
					if err := api.Details(nil); err != nil {
						return ToolResult{}, err
					}
					task, found, err := api.Task(ctx, api.TaskID())
					if err != nil || !found {
						return ToolResult{}, errors.New("tool invocation task missing")
					}
					state, err := h.Snapshot(ctx)
					if err != nil {
						return ToolResult{}, err
					}
					saw := false
					for _, doc := range state.Documents {
						if doc.Kind != "pi.live" || doc.Owner != task.Conversation || doc.Retired {
							continue
						}
						slots, _ := doc.Value["tools"].([]any)
						for _, raw := range slots {
							slot := generationObject(raw)
							if submissionEntryID(slot["taskId"]) == task.ID {
								saw = true
								if !equalJSONValue(slot["diagnostics"], []any{JSON{"severity": severity, "message": "progress"}}) {
									t.Error("progress diagnostic JSON changed", slot)
								}
							}
						}
					}
					if !saw {
						t.Error("diagnostic progress slot missing")
					}
					if _, err := api.Commit(ctx, func(tx *Tx) (any, error) {
						return tx.AppendEntry(task.Conversation, JSON{"kind": "probe.diagnostic-effect", "data": JSON{"severity": severity}})
					}); err != nil {
						return ToolResult{}, err
					}
					return ToolResult{Blocks: []goai.ContentBlock{{Type: "text", Text: "result"}}, Diagnostics: []ToolDiagnostic{{Severity: severity, Message: "returned"}}}, nil
				}}); err != nil {
					t.Fatal(err)
				}
				calls := 0
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					calls++
					ch := make(chan goai.Event, 1)
					if calls == 1 {
						ch <- toolAnswer("diagnostic-json", "diagnostic_json", JSON{})
					} else {
						found := false
						for _, message := range input.Messages {
							if message.Role == goai.RoleToolResult {
								found = true
								if message.IsError || message.ToolCallID != "diagnostic-json" || len(message.Content) != 2 || message.Content[0].Text != "result" || message.Content[1].Text != "<harness>\n["+severity+"] progress\n["+severity+"] returned\n</harness>" {
									t.Error("diagnostic provider receipt", message)
								}
							}
						}
						if !found {
							t.Error("diagnostic result missing from provider context")
						}
						ch <- terminal("done")
					}
					close(ch)
					return ch
				})
				options.Registry = registry
				h = openHarness(t, b.store, options)
				c := root(t, h, ref)
				sub, err := c.Submit(bg, Input{Content: "go"})
				if err != nil {
					t.Fatal(err)
				}
				if result := waitSubmission(t, sub); result.Record.Status != "done" {
					t.Fatal(result.Record)
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				effects, results := 0, 0
				for _, entry := range state.Entries {
					if entry.Kind == "pi.tool-result" {
						wantData := JSON{"diagnostics": []any{JSON{"severity": severity, "message": "progress"}, JSON{"severity": severity, "message": "returned"}}}
						if !entry.HasData || !equalJSONValue(entry.Data, wantData) {
							t.Error("tool result data is not exact source diagnostic JSON", entry)
						}
					}
					if entry.Kind == "probe.diagnostic-effect" {
						effects++
						if !equalJSONValue(entry.Data, JSON{"severity": severity}) {
							t.Error("diagnostic effect data", entry)
						}
					}
				}
				for _, task := range state.Tasks {
					if task.Kind != "pi.tool" {
						continue
					}
					record, found, err := h.Task(bg, task.ID)
					if err != nil || !found || record.State.Outcome == nil || record.State.Outcome.Status != "completed" {
						t.Fatal("diagnostic result task did not complete", record, err)
					}
					receipt := canonicalModelToolReceipt(t, h, task.ID)
					results++
					want := []ToolDiagnostic{{Severity: severity, Message: "progress"}, {Severity: severity, Message: "returned"}}
					if receipt.IsError || !reflect.DeepEqual(receipt.Diagnostics, want) {
						t.Error("diagnostic result JSON changed", receipt.Diagnostics, want)
					}
				}
				if effects != 1 || results != 1 || calls != 2 {
					t.Fatal("diagnostic effects/results/requests", effects, results, calls)
				}
			})
		})
	}
}

// Supplemental native JSON-resource and internal argument-witness guards. The
// source runtime has no severity enum validator; these are distinct boundaries.
func TestToolDiagnosticBoundsAndArgumentContext(t *testing.T) {
	limits := DefaultLimits()
	for _, diagnostic := range []ToolDiagnostic{
		{Severity: "\xff", Message: "valid"},
		{Severity: "invalid", Message: "\xff"},
		{Severity: "invalid", Message: "valid", Code: "\xff"},
		{Severity: "warn", Message: "valid", Code: "invalid_arguments", argumentError: true},
		{Severity: "error", Message: "valid", Code: "other", argumentError: true},
	} {
		if err := validateToolDiagnostics([]ToolDiagnostic{diagnostic}, limits); err == nil {
			t.Fatal("invalid JSON or argument witness accepted", diagnostic)
		}
	}
	limited := limits
	limited.MaxStringBytes = 8
	for _, diagnostic := range []ToolDiagnostic{
		{Severity: "123456789", Message: "valid"},
		{Severity: "invalid", Message: "123456789"},
		{Severity: "invalid", Message: "valid", Code: "123456789"},
	} {
		if err := validateToolDiagnostics([]ToolDiagnostic{diagnostic}, limited); err == nil {
			t.Fatal("oversized diagnostic field accepted", diagnostic)
		}
	}
	limited = limits
	limited.MaxMembers = 1
	if err := validateToolDiagnostics([]ToolDiagnostic{{Severity: "unknown", Message: "one"}, {Severity: "unknown", Message: "two"}}, limited); err == nil {
		t.Fatal("diagnostic count bound omitted")
	}
}
