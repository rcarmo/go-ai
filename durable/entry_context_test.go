package durable

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func entryToken(t *testing.T, kind string) *EntryDefinition {
	t.Helper()
	d, e := DefineEntry(kind)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func TestEntryContextTypedOwnedDataAndNarrowing(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, reopen func() *Session) {
		d := entryToken(t, "entry.data")
		input := map[string]any{"x": []any{1}}
		var id ID
		checkpointCommit(t, s, func(tx *Tx) error {
			v, e := tx.AppendTypedEntry(d, 1, EntryContent{Data: input, HasData: true, Model: []goai.Message{goai.UserMessage("owned")}})
			if e != nil {
				return e
			}
			id = v.ID
			input["x"].([]any)[0] = 99
			v.Model[0].Content[0].Text = "mutated"
			return nil
		})
		got, ok, e := s.TypedEntry(bg, d, 1, id)
		if e != nil || !ok || got.Model[0].Content[0].Text != "owned" || got.Data.(map[string]any)["x"].([]any)[0].(json.Number) != "1" {
			t.Fatal(got, ok, e)
		}
		got.Data.(map[string]any)["x"].([]any)[0] = 33
		if _, ok, e = s.TypedEntry(bg, entryToken(t, "entry.other"), 1, id); e != nil || ok {
			t.Fatal("wrong token", ok, e)
		}
		s = reopen()
		got, ok, e = s.TypedEntry(bg, d, 1, id)
		if e != nil || !ok || got.Data.(map[string]any)["x"].([]any)[0].(json.Number) != "1" {
			t.Fatal("reopen data", got, e)
		}
		checkpointCommit(t, s, func(tx *Tx) error {
			_, e := tx.AppendTypedEntry(d, 1, EntryContent{Data: nil, HasData: true})
			return e
		})
	})
}
func TestEntryContextHeadEditsToolPairsAndAncestor(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, reopen func() *Session) {
		d := entryToken(t, "entry.context")
		var first, cutoff ID
		checkpointCommit(t, s, func(tx *Tx) error {
			v, e := tx.AppendTypedEntry(d, 1, EntryContent{Model: []goai.Message{goai.UserMessage("old")}})
			if e != nil {
				return e
			}
			first = v.ID
			v, e = tx.AppendTypedEntry(d, 1, EntryContent{Model: []goai.Message{{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "a", Name: "one", Arguments: map[string]any{}}, {Type: "toolCall", ID: "b", Name: "two", Arguments: map[string]any{}}}}}})
			if e != nil {
				return e
			}
			cutoff = v.ID
			return nil
		})
		var child Conversation
		checkpointCommit(t, s, func(tx *Tx) error { var e error; child, e = tx.ForkConversation(1, cutoff, 0); return e })
		replacement := []MessageReceipt{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "edited ancestor"}}}}
		checkpointCommit(t, s, func(tx *Tx) error {
			_, e := tx.AppendTypedEntry(d, child.ID, EntryContent{Head: first, Edits: []ContextEdit{{Target: first, Action: "replace", Messages: replacement}}, Model: []goai.Message{{Role: goai.RoleToolResult, ToolCallID: "b", ToolName: "two", Content: []goai.ContentBlock{{Type: "text", Text: "result b"}}}, {Role: goai.RoleToolResult, ToolCallID: "extra", Content: []goai.ContentBlock{{Type: "text", Text: "discard"}}}}})
			return e
		})
		view, e := s.ContextView(bg, child.ID, 0)
		if e != nil {
			t.Fatal(e)
		}
		// Head marker is first in active contributions, then non-head entries. Its
		// orphan results precede the assistant and are dropped; missing a/b results
		// are synthesized immediately after their calls.
		if len(view.Messages) != 4 || view.Messages[0].Content[0].Text != "edited ancestor" || view.Messages[2].Details["reason"] != "missing_result" || view.Messages[3].ToolCallID != "b" || view.Messages[3].Details["reason"] != "missing_result" {
			t.Fatal("reduced context", view.Messages)
		}
		old, e := s.ContextView(bg, child.ID, cutoff)
		if e != nil || old.Messages[0].Content[0].Text != "old" {
			t.Fatal("cutoff captured", old, e)
		}
		replacement[0].Content[0].Text = "retained mutation"
		s = reopen()
		view, e = s.ContextView(bg, child.ID, 0)
		if e != nil || view.Messages[0].Content[0].Text != "edited ancestor" {
			t.Fatal("edit reopen", view, e)
		}
		checkpointCommit(t, s, func(tx *Tx) error {
			_, e := tx.AppendTypedEntry(d, child.ID, EntryContent{HeadSelf: true, Model: []goai.Message{goai.UserMessage("new head")}})
			return e
		})
		view, e = s.ContextView(bg, child.ID, 0)
		if e != nil || len(view.Messages) != 1 || view.Messages[0].Content[0].Text != "new head" {
			t.Fatal("head self", view, e)
		}
	})
}
func TestEntryContextContributionControlsRejectBeforeStage(t *testing.T) {
	store, e := OpenMemory(MemoryOptions{})
	if e != nil {
		t.Fatal(e)
	}
	s, e := OpenSession(store)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(bg)
	d := entryToken(t, "entry.safe")
	for _, m := range []goai.Message{
		{Role: goai.RoleUser, Deferred: &goai.DeferredHandle{ID: "OPAQUE_SECRET"}},
		{Role: goai.RoleAssistant, ErrorMessage: "RAW_SECRET"},
		{Role: goai.RoleAssistant, Diagnostics: []goai.AssistantMessageDiagnostic{{Error: goai.DiagnosticError{Message: "RAW_SECRET"}}}},
		{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "x", TextSignaturePresent: true}}},
		{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{{Name: "x", Parameters: json.RawMessage(`{"type":"object"}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar"}}}},
		{Role: goai.RoleToolResult, Details: func() {}},
	} {
		before := checkpointSnapshot(t, s)
		_, e = s.Commit(bg, func(tx *Tx) error {
			_, e := tx.AppendTypedEntry(d, 1, EntryContent{Model: []goai.Message{m}})
			return e
		})
		if e == nil {
			t.Fatal("unsupported control persisted")
		}
		after := checkpointSnapshot(t, s)
		before.HighWater = after.HighWater
		if !reflect.DeepEqual(before, after) {
			t.Fatal("rejected contribution changed records")
		}
	}
	checkpointCommit(t, s, func(tx *Tx) error {
		_, e := tx.AppendTypedEntry(d, 1, EntryContent{Model: []goai.Message{{Role: goai.RoleToolResult, Details: []any{1, "owned"}}}})
		return e
	})
	for _, v := range checkpointSnapshot(t, s).Entries {
		if v.Model[0].HasDetails != true || len(v.Model[0].DetailsValue.([]any)) != 2 {
			t.Fatal("primitive details adaptation", v)
		}
	}
}
func TestEntryContextActualHTTPSystemToolsEditedAncestorReopen(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := json.NewDecoder(r.Body).Decode(&request); e != nil {
			t.Error(e)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"context answer\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "private")
	model := &goai.Model{ID: "entry-http", Api: goai.ApiOpenAICompletions, Provider: goai.ProviderOpenAI, BaseURL: server.URL, ContextWindow: 4096, MaxTokens: 128, Input: []string{"text"}, Cost: goai.ModelCost{}}
	options := Options{Models: func(goai.Provider, string) *goai.Model { return model }, RequestOptions: func(context.Context, ModelRef) (*goai.StreamOptions, error) {
		return &goai.StreamOptions{APIKey: "AUTH_SECRET_ONLY"}, nil
	}}
	open := func() *Harness {
		store, e := OpenJournal(dir, JournalOptions{})
		if e != nil {
			t.Fatal(e)
		}
		h, e := Open(bg, store, options)
		if e != nil {
			t.Fatal(e)
		}
		return h
	}
	h := open()
	root, e := h.Root(bg, AgentChange{Model: ModelRef{model.Provider, model.ID}, SystemPrompt: "base"})
	if e != nil {
		t.Fatal(e)
	}
	token := entryToken(t, "entry.http")
	var first, at ID
	section := "section-owned"
	tool := goai.Tool{Name: "keep", Description: "kept", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}
	checkpointCommit(t, h.session, func(tx *Tx) error {
		v, e := tx.AppendTypedEntry(token, root.ID(), EntryContent{Model: []goai.Message{goai.UserMessage("ancestor original")}})
		if e != nil {
			return e
		}
		first = v.ID
		v, e = tx.AppendTypedEntry(token, root.ID(), EntryContent{Model: []goai.Message{{Role: goai.RoleSystem, Content: []goai.ContentBlock{}, Sections: map[string]*string{"policy": &section}, ToolsAdded: []goai.Tool{tool, {Name: "remove", Description: "removed", Parameters: json.RawMessage(`{"type":"object"}`)}}}}})
		if e != nil {
			return e
		}
		at = v.ID
		for _, stop := range []goai.StopReason{goai.StopReasonPending, goai.StopReasonAborted, goai.StopReasonError, goai.StopReasonDeferred} {
			v, e = tx.AppendTypedEntry(token, root.ID(), EntryContent{Model: []goai.Message{{Role: goai.RoleAssistant, StopReason: stop, Content: []goai.ContentBlock{{Type: "text", Text: "literal-" + string(stop)}}}}})
			if e != nil {
				return e
			}
			at = v.ID
		}
		return nil
	})
	section = "caller mutated"
	tool.Parameters[0] = 'x'
	child, e := root.Fork(bg, at)
	if e != nil {
		t.Fatal(e)
	}
	checkpointCommit(t, h.session, func(tx *Tx) error {
		_, e := tx.AppendTypedEntry(token, child.ID(), EntryContent{Edits: []ContextEdit{{Target: first, Action: "replace", Messages: []MessageReceipt{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "edited ancestor"}}}}}}, Model: []goai.Message{{Role: goai.RoleSystem, Content: []goai.ContentBlock{}, ToolsRemoved: []goai.ToolReference{{Name: "remove"}}}}})
		return e
	})
	childID := child.ID()
	if e = h.Close(bg); e != nil {
		t.Fatal(e)
	}
	h = open()
	defer h.Close(bg)
	child, e = h.Conversation(bg, childID)
	if e != nil {
		t.Fatal(e)
	}
	sub, e := child.Submit(bg, Input{Content: "actual request", RequestID: "entry-context-http"})
	if e != nil {
		t.Fatal(e)
	}
	result := waitSubmission(t, sub)
	if result.Message == nil || result.Message.Content[0].Text != "context answer" {
		t.Fatal(result)
	}
	messages := request["messages"].([]any)
	encoded, _ := json.Marshal(messages)
	if !strings.Contains(string(encoded), "section-owned") || !strings.Contains(string(encoded), "edited ancestor") || !strings.Contains(string(encoded), "literal-pending") || strings.Contains(string(encoded), "literal-aborted") || strings.Contains(string(encoded), "literal-error") || strings.Contains(string(encoded), "literal-deferred") || strings.Contains(string(encoded), "ancestor original") || strings.Contains(string(encoded), "caller mutated") {
		t.Fatal("real provider context", string(encoded))
	}
	tools := request["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["function"].(map[string]any)["name"] != "keep" {
		t.Fatal("real provider tools", tools)
	}
	journal, e := os.ReadFile(filepath.Join(dir, "journal.bin"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(journal), "AUTH_SECRET_ONLY") {
		t.Fatal("credential persisted")
	}
}

func TestEntryContextPendingPreservedAndExactExcludedStops(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, _ func() *Session) {
		token := entryToken(t, "entry.pending")
		checkpointCommit(t, s, func(tx *Tx) error {
			for _, stop := range []goai.StopReason{goai.StopReasonAborted, goai.StopReasonError, goai.StopReasonDeferred} {
				if _, e := tx.AppendTypedEntry(token, 1, EntryContent{Model: []goai.Message{{Role: goai.RoleAssistant, StopReason: stop, Content: []goai.ContentBlock{{Type: "text", Text: "excluded-" + string(stop)}}}}}); e != nil {
					return e
				}
			}
			if _, e := tx.AppendTypedEntry(token, 1, EntryContent{Model: []goai.Message{{Role: goai.RoleAssistant, StopReason: goai.StopReasonPending, Content: []goai.ContentBlock{{Type: "toolCall", ID: "pending-call", Name: "pending_tool", Arguments: map[string]any{}}}}}}); e != nil {
				return e
			}
			_, e := tx.AppendTypedEntry(token, 1, EntryContent{Model: []goai.Message{{Role: goai.RoleToolResult, ToolCallID: "pending-call", ToolName: "pending_tool", Content: []goai.ContentBlock{{Type: "text", Text: "kept-result"}}}}})
			return e
		})
		view, e := s.ContextView(bg, 1, 0)
		if e != nil || len(view.Messages) != 2 || view.Messages[0].StopReason != goai.StopReasonPending || view.Messages[1].Content[0].Text != "kept-result" {
			t.Fatal("pending/excluded/pair contract", view, e)
		}
	})
}

func TestEntryContextEmptyArgumentsWitnessOwnedSnapshotReopenAndHTTP(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, reopen func() *Session) {
		token := entryToken(t, "entry.empty.args")
		empty := map[string]any{}
		nonempty := map[string]any{"nested": []any{"owned"}}
		var id ID
		checkpointCommit(t, s, func(tx *Tx) error {
			v, e := tx.AppendTypedEntry(token, 1, EntryContent{Model: []goai.Message{{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "empty", Name: "empty_tool", Arguments: empty}, {Type: "toolCall", ID: "full", Name: "full_tool", Arguments: nonempty}}}}})
			id = v.ID
			empty["mutated"] = true
			nonempty["nested"].([]any)[0] = "caller"
			return e
		})
		check := func(s *Session) {
			snapshot := checkpointSnapshot(t, s)
			receipt := snapshot.Entries[id].Model[0]
			if !reflect.DeepEqual(receipt.EmptyArguments, []int{0}) || receipt.Content[0].Arguments == nil || len(receipt.Content[0].Arguments) != 0 || receipt.Content[1].Arguments["nested"].([]any)[0] != "owned" {
				t.Fatal("lossless public arguments", receipt)
			}
			receipt.Content[0].Arguments["snapshot"] = true
			receipt.Content[1].Arguments["nested"].([]any)[0] = "snapshot"
			v, ok, e := s.TypedEntry(bg, token, 1, id)
			if e != nil || !ok || len(v.Model[0].Content[0].Arguments) != 0 || v.Model[0].Content[1].Arguments["nested"].([]any)[0] != "owned" {
				t.Fatal("public snapshot authority", v, e)
			}
		}
		check(s)
		s = reopen()
		check(s)
		for _, receipt := range []MessageReceipt{
			{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: "bad", Name: "bad", Arguments: nil}}},
			{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "bad"}}, EmptyArguments: []int{0}},
			{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: "bad", Name: "bad", Arguments: map[string]any{"x": 1}}}, EmptyArguments: []int{0}},
			{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: "bad", Name: "bad", Arguments: map[string]any{}}}, EmptyArguments: []int{0, 0}},
			{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: "bad", Name: "bad", Arguments: map[string]any{}}}, EmptyArguments: []int{-1}},
			{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: "bad", Name: "bad", Arguments: map[string]any{}}}, EmptyArguments: []int{3}},
		} {
			before := checkpointSnapshot(t, s)
			_, e := s.Commit(bg, func(tx *Tx) error {
				_, e := tx.AppendTypedEntry(token, 1, EntryContent{Edits: []ContextEdit{{Target: id, Action: "replace", Messages: []MessageReceipt{receipt}}}})
				return e
			})
			if e == nil {
				t.Fatal("malformed witness accepted")
			}
			after := checkpointSnapshot(t, s)
			before.HighWater = after.HighWater
			if !reflect.DeepEqual(before, after) {
				t.Fatal("witness rejection state")
			}
		}
		if _, e := contributionReceipt(goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: "nil", Name: "nil", Arguments: nil}}}, s.limits); e == nil {
			t.Fatal("caller nil accepted")
		}
		if _, e := contributionReceipt(goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: "code", Name: "code", Arguments: map[string]any{"x": func() {}}}}}, s.limits); e == nil {
			t.Fatal("caller executable argument accepted")
		}
	})
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&request)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"empty checked\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "private")
	model := &goai.Model{ID: "empty-http", Api: goai.ApiOpenAICompletions, Provider: goai.ProviderOpenAI, BaseURL: server.URL, ContextWindow: 4096, MaxTokens: 128, Input: []string{"text"}}
	open := func() *Harness {
		store, e := OpenJournal(dir, JournalOptions{})
		if e != nil {
			t.Fatal(e)
		}
		h, e := Open(bg, store, Options{Models: func(goai.Provider, string) *goai.Model { return model }, RequestOptions: func(context.Context, ModelRef) (*goai.StreamOptions, error) {
			return &goai.StreamOptions{APIKey: "local"}, nil
		}})
		if e != nil {
			t.Fatal(e)
		}
		return h
	}
	h := open()
	root, e := h.Root(bg, AgentChange{Model: ModelRef{model.Provider, model.ID}})
	if e != nil {
		t.Fatal(e)
	}
	token := entryToken(t, "entry.empty.http")
	checkpointCommit(t, h.session, func(tx *Tx) error {
		_, e := tx.AppendTypedEntry(token, 1, EntryContent{Model: []goai.Message{{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "empty-wire", Name: "empty_tool", Arguments: map[string]any{}}}}, {Role: goai.RoleToolResult, ToolCallID: "empty-wire", ToolName: "empty_tool", Content: []goai.ContentBlock{{Type: "text", Text: "result-owned"}}}}})
		return e
	})
	h.Close(bg)
	h = open()
	defer h.Close(bg)
	root, e = h.Conversation(bg, 1)
	if e != nil {
		t.Fatal(e)
	}
	sub, e := root.Submit(bg, Input{Content: "check empty", RequestID: "empty-http"})
	if e != nil {
		t.Fatal(e)
	}
	waitSubmission(t, sub)
	messages := request["messages"].([]any)
	found := false
	for i, v := range messages {
		m := v.(map[string]any)
		if m["role"] != "assistant" {
			continue
		}
		calls, ok := m["tool_calls"].([]any)
		if !ok || len(calls) != 1 {
			continue
		}
		fn := calls[0].(map[string]any)["function"].(map[string]any)
		if fn["arguments"] != "{}" || i+1 >= len(messages) || messages[i+1].(map[string]any)["role"] != "tool" {
			t.Fatal("empty actualHTTP/callresult", messages)
		}
		found = true
	}
	encoded, _ := json.Marshal(request)
	if !found || strings.Contains(string(encoded), "emptyArguments") {
		t.Fatal("witness leaked/dropped emptyHTTP", string(encoded))
	}
}
