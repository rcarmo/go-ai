package durable

import (
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestToolDiagnosticsCommittedDetachedAndRenderedOutsideDetails(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var escaped *ToolAPI
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "diagnostic", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "diagnostic.test", Version: 1, Execute: func(_ context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			escaped = api
			if err := api.Diagnostic(ToolDiagnostic{Severity: "warn", Code: "remark", Message: "partial remark"}); err != nil {
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
		if len(seen) != 2 || seen[0].Message != "partial remark" || seen[1].Message != "final remark" {
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
				var cp toolCheckpoint
				if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
					t.Fatal(err)
				}
				if len(cp.Diagnostics) != 2 || cp.Diagnostics[0].Message != "partial remark" {
					t.Fatal("diagnostic alias", cp)
				}
			}
		}
	})
}
func TestToolDiagnosticsInvalidReturnRejectsAppMutation(t *testing.T) {
	registry := NewRegistry()
	committed := false
	if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "bad-diagnostic", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "diagnostic.bad", Version: 1, Execute: func(_ context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
		if err := api.Diagnostic(ToolDiagnostic{Severity: "invalid", Message: "bad"}); err == nil {
			t.Error("invalid progress diagnostic accepted")
		}
		return ToolResult{Diagnostics: []ToolDiagnostic{{Severity: "invalid", Message: "bad"}}, Commit: func(*Tx) error { committed = true; return nil }}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		calls++
		ch := make(chan goai.Event, 1)
		if calls == 1 {
			ch <- toolAnswer("bad-call", "bad-diagnostic", JSON{})
		} else {
			for _, m := range input.Messages {
				if m.Role == goai.RoleToolResult && (!m.IsError || !strings.Contains(m.Content[0].Text, "invalid_diagnostics")) {
					t.Error(m)
				}
			}
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	store, _ := NewMemory()
	h := openHarness(t, store, options)
	c := root(t, h, ref)
	sub, err := c.Submit(bg, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	waitSubmission(t, sub)
	if committed {
		t.Fatal("invalid diagnostics ran callback")
	}
}
