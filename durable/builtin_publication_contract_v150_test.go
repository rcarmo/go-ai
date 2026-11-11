package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sync"
	"sync/atomic"
	"testing"
)

// Independent source-derived control paired with the real Harness probe in
// notes/oracles/session-admission.upstream.test.ts. It failed six assertions
// at 1eafca1 (version/state, early listener_error and lost tool publications).
func TestMainBuiltinCanonicalPublicationsV150(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "tool-c1", Description: "tool-c1 tool", Parameters: []byte(`{"type":"object","properties":{}}`)}, Implementation: "oracle.c1", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{}, nil }}); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("c1", "tool-c1", JSON{})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	h := openHarness(t, store, options)
	c := root(t, h, ref)
	var mu sync.Mutex
	var changes []ReadonlyValue
	var ends []WatchEnd
	guard, err := h.ObserveCommits(CommitObserver{Advance: func(_ context.Context, p CommitPublication) error {
		mu.Lock()
		defer mu.Unlock()
		for i := range p.Changes.Len() {
			change, _ := p.Changes.Index(i)
			changes = append(changes, change)
		}
		return nil
	}, End: func(end WatchEnd) error { mu.Lock(); defer mu.Unlock(); ends = append(ends, end); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Detach()
	sub, err := c.Submit(bg, Input{Type: "input", Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if result := waitSubmission(t, sub); result.Submission.Status != "done" {
		t.Fatal(result)
	}
	mu.Lock()
	beforeClose := append([]WatchEnd(nil), ends...)
	mu.Unlock()
	if len(beforeClose) != 0 {
		t.Errorf("stored observer ended before close: %+v", beforeClose)
	}
	if err := h.Close(bg); err != nil {
		t.Fatal(err)
	}
	var generation, work, request ReadonlyValue
	for _, change := range changes {
		kind := immutableViewMember(t, change, "type").String()
		if kind != "task" && kind != "entry" {
			continue
		}
		value := immutableViewMember(t, change, "value")
		switch immutableViewMember(t, value, "kind").String() {
		case "pi.generation":
			if generation == nil {
				generation = value
			}
		case "pi.tool":
			if work == nil {
				work = value
			}
		case "pi.assistant":
			if request == nil {
				request = value
			}
		}
	}
	if generation == nil {
		t.Error("missing generation publication")
	} else {
		if got := immutableViewMember(t, generation, "version").Number().String(); got != "2" {
			t.Errorf("generation version=%s, want source2", got)
		}
		if value := immutableViewMember(t, generation, "input"); !equalJSONValue(value.Copy(), JSON{}) {
			t.Error("generation input differs from source empty object")
		}
		if state := immutableViewMember(t, generation, "state"); !equalJSONValue(state.Copy(), JSON{"status": "pending", "checkpoint": JSON{"phase": "prepare", "attempt": 1}}) {
			t.Errorf("generation initial state differs from source pending/prepare/attempt1: %v", state.Copy())
		}
	}
	if request == nil {
		t.Error("missing assistant tool-call publication")
	}
	if work == nil {
		t.Error("missing model-tool publication")
	} else {
		if got := immutableViewMember(t, work, "version").Number().String(); got != "3" {
			t.Errorf("tool version=%s, want source3", got)
		}
		if request != nil {
			expected := JSON{"kind": "model", "assistant": immutableViewMember(t, request, "id").Number(), "callId": "c1"}
			if input := immutableViewMember(t, work, "input"); !equalJSONValue(input.Copy(), expected) {
				t.Error("model-tool input differs from source")
			}
		}
		if state := immutableViewMember(t, work, "state"); !equalJSONValue(state.Copy(), JSON{"status": "pending", "checkpoint": JSON{"phase": "call"}}) {
			t.Error("tool initial state differs from source")
		}
	}
	if len(ends) != 1 || ends[0].Reason != "failed" {
		t.Errorf("source close ending differs: %+v", ends)
	}
}
