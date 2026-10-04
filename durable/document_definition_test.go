package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func mustDefinition(t *testing.T, o DefinitionOptions) *DocumentDefinition {
	t.Helper()
	d, e := DefineDocument(o)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func TestDocumentDefinitionFamilySeedScopeAndSealedOwnership(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		session, e := OpenSession(b.store)
		if e != nil {
			t.Fatal(e)
		}
		defer session.Close(bg)
		var initCalls int
		var retained JSON
		family := mustDefinition(t, DefinitionOptions{Kind: "definition.family", Version: 1, Scope: "session", Family: true, Initial: func(seed JSON) (JSON, error) {
			initCalls++
			retained = JSON{"seed": seed, "nested": JSON{"value": "owned"}}
			return retained, nil
		}})
		key := ""
		seed := JSON{"input": "first"}
		var escaped *DocumentHandle
		var id ID
		_, e = session.Commit(bg, func(tx *Tx) error {
			h, e := tx.AcquireDocument(family, 0, &key, seed)
			if e != nil {
				return e
			}
			escaped = h
			id = h.id
			seed["input"] = "caller"
			retained["nested"].(JSON)["value"] = "caller"
			second, e := tx.AcquireDocument(family, 0, &key, JSON{"input": "ignored"})
			if e != nil {
				return e
			}
			if second.id != h.id {
				return errors.New("duplicatefamilyincarnation")
			}
			value, e := second.Get()
			if e != nil {
				return e
			}
			if value["seed"].(map[string]any)["input"] != "first" || value["nested"].(map[string]any)["value"] != "owned" {
				return errors.New("initializer/inputalias")
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
		if initCalls != 1 {
			t.Fatal(initCalls)
		}
		if _, e = escaped.Get(); !errors.Is(e, ErrSealed) {
			t.Fatal(e)
		}
		value, ok, e := session.SnapshotDefinition(bg, family, 0, &key)
		if e != nil || !ok {
			t.Fatal(value, ok, e)
		}
		value["nested"].(map[string]any)["value"] = "reader"
		value, ok, e = session.SnapshotDefinition(bg, family, 0, &key)
		if e != nil || !ok || value["nested"].(map[string]any)["value"] != "owned" {
			t.Fatal(value, ok, e)
		}
		for _, run := range []func() error{
			func() error { _, _, e := session.SnapshotDefinition(bg, family, 1, &key); return e },
			func() error { _, _, e := session.SnapshotDefinition(bg, family, 0, nil); return e },
		} {
			rejected(t, run())
		}
		_, e = session.Commit(bg, func(tx *Tx) error {
			if e := tx.RetireDefinition(family, 0, &key); e != nil {
				return e
			}
			_, e := tx.AcquireDocument(family, 0, &key, JSON{"input": "second"})
			return e
		})
		if e != nil {
			t.Fatal(e)
		}
		state, e := session.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		if !state.Documents[id].Retired || initCalls != 2 {
			t.Fatal("recreateinitializer", initCalls)
		}
	})
}

func TestDocumentDefinitionMigrationSnapshotsRequiredBaseAndRollback(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		session, e := OpenSession(b.store)
		if e != nil {
			t.Fatal(e)
		}
		defer session.Close(bg)
		old := mustDefinition(t, DefinitionOptions{Kind: "definition.version", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"count": 2}, nil }})
		var calls int
		var retained JSON
		current := mustDefinition(t, DefinitionOptions{Kind: "definition.version", Version: 3, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{}, nil }, Migrate: func(value JSON, version uint64) (JSON, error) {
			if version != 1 {
				return nil, errors.New("wrongfromversion")
			}
			calls++
			retained = JSON{"count": value["count"], "labels": []any{"migrated"}}
			return retained, nil
		}})
		var id ID
		_, e = session.Commit(bg, func(tx *Tx) error {
			h, e := tx.AcquireDocument(old, 0, nil, nil)
			if e == nil {
				id = h.id
			}
			return e
		})
		if e != nil {
			t.Fatal(e)
		}
		before, e := session.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 2; i++ {
			v, ok, e := session.SnapshotDefinition(bg, current, 0, nil)
			if e != nil || !ok || v["count"] != json.Number("2") {
				t.Fatal(v, ok, e)
			}
		}
		retained["labels"].([]any)[0] = "escaped"
		if calls != 1 {
			t.Fatal("coldmigrationcount", calls)
		}
		after, e := session.Snapshot(bg)
		if e != nil || !equalSnapshot(before, after) {
			t.Fatal("snapshotpersistedmigration", e)
		}
		unrelated := mustDefinition(t, DefinitionOptions{Kind: "definition.other", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{}, nil }})
		_, e = session.Commit(bg, func(tx *Tx) error { _, e := tx.AcquireDocument(unrelated, 0, nil, nil); return e })
		if e != nil {
			t.Fatal(e)
		}
		if _, _, e = session.SnapshotDefinition(bg, current, 0, nil); e != nil || calls != 1 {
			t.Fatal("unrelatedcommitinvalidatedmigration", e, calls)
		}
		before, e = session.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		_, e = session.Commit(bg, func(tx *Tx) error {
			h, e := tx.AcquireDocument(current, 0, nil, nil)
			if e != nil {
				return e
			}
			if e = h.Set(JSON{"count": 9, "labels": []any{"rollback"}}); e != nil {
				return e
			}
			return errors.New("callbackrollback")
		})
		if e == nil {
			t.Fatal("callbackaccepted")
		}
		after, _ = session.Snapshot(bg)
		if !equalSnapshot(before, after) {
			t.Fatal("migrationcallbackwrote")
		}
		// The first successful typed transaction persists a required base even
		// when the migrated value itself is not edited.
		seq, e := session.Commit(bg, func(tx *Tx) error { _, e := tx.AcquireDocument(current, 0, nil, nil); return e })
		if e != nil {
			t.Fatal(e)
		}
		state, e := session.Snapshot(bg)
		if e != nil || state.Seq != seq || state.Documents[id].Version != 3 || state.Documents[id].Value["labels"].([]any)[0] != "migrated" {
			t.Fatal(state, e)
		}
		if calls != 1 {
			t.Fatal("cachedmigrationrerun", calls)
		}
		_, _, e = session.SnapshotDefinition(bg, old, 0, nil)
		rejected(t, e)
		if e = session.UnloadDocuments(bg); e != nil {
			t.Fatal(e)
		}
		v, ok, e := session.SnapshotDefinition(bg, current, 0, nil)
		if e != nil || !ok || v["count"] != json.Number("2") || calls != 1 {
			t.Fatal(v, ok, e, calls)
		}
	})
}

func TestDocumentDefinitionVersionPolicyInvalidCallbacksAndConcurrentReads(t *testing.T) {
	s, e := NewMemory()
	if e != nil {
		t.Fatal(e)
	}
	session, e := OpenSession(s)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close(bg)
	for _, options := range []DefinitionOptions{{Kind: "k", Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{}, nil }}, {Kind: "k", Version: 1, Scope: "session"}, {Kind: "k", Version: 1, Scope: "conversation", History: "latest", Fork: "asOf", Initial: func(JSON) (JSON, error) { return JSON{}, nil }}} {
		_, e := DefineDocument(options)
		rejected(t, e)
	}
	old := mustDefinition(t, DefinitionOptions{Kind: "versions", Version: 2, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"value": "stored"}, nil }})
	_, e = session.Commit(bg, func(tx *Tx) error { _, e := tx.AcquireDocument(old, 0, nil, nil); return e })
	if e != nil {
		t.Fatal(e)
	}
	for _, version := range []uint64{1, 3} {
		def := mustDefinition(t, DefinitionOptions{Kind: "versions", Version: version, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{}, nil }})
		_, _, e = session.SnapshotDefinition(bg, def, 0, nil)
		rejected(t, e)
		_, e = session.Commit(bg, func(tx *Tx) error { _, e := tx.AcquireDocument(def, 0, nil, nil); return e })
		rejected(t, e)
	}
	bad := mustDefinition(t, DefinitionOptions{Kind: "versions", Version: 3, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{}, nil }, Migrate: func(JSON, uint64) (JSON, error) { return JSON{"bad": func() {}}, nil }})
	_, _, e = session.SnapshotDefinition(bg, bad, 0, nil)
	rejected(t, e)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				v, ok, e := session.SnapshotDefinition(bg, old, 0, nil)
				if e != nil || !ok {
					t.Errorf("snapshot %v", e)
					return
				}
				v["value"] = "caller"
			}
		}()
	}
	wg.Wait()
	v, ok, e := session.SnapshotDefinition(bg, old, 0, nil)
	if e != nil || !ok || v["value"] != "stored" {
		t.Fatal(v, ok, e)
	}
	if e = session.UnloadDocuments(bg); e != nil {
		t.Fatal(e)
	}
	if len(session.definitionCache) != 0 || session.definitionCacheBytes != 0 {
		t.Fatal("unloadcache")
	}
}

func TestDocumentDefinitionHistoricalAncestorAndCopyMigration(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		s := b.store
		session, e := OpenSession(s)
		if e != nil {
			t.Fatal(e)
		}
		defer session.Close(bg)
		old := mustDefinition(t, DefinitionOptions{Kind: "history.typed", Version: 1, Scope: "conversation", History: "rewindable", Fork: "asOf", Initial: func(JSON) (JSON, error) { return JSON{"value": "initial"}, nil }})
		var cut ID
		_, e = session.Commit(bg, func(tx *Tx) error {
			var e error
			cut, e = tx.MintID()
			if e != nil {
				return e
			}
			if e = tx.AppendEntry(Entry{ID: cut, Conversation: 1, Kind: "note", Value: JSON{}}); e != nil {
				return e
			}
			h, e := tx.AcquireDocument(old, 1, nil, nil)
			if e != nil {
				return e
			}
			return h.Set(JSON{"value": "at-cut"})
		})
		if e != nil {
			t.Fatal(e)
		}
		_, e = session.Commit(bg, func(tx *Tx) error {
			h, e := tx.AcquireDocument(old, 1, nil, nil)
			if e != nil {
				return e
			}
			return h.Set(JSON{"value": "later"})
		})
		if e != nil {
			t.Fatal(e)
		}
		var calls int
		current := mustDefinition(t, DefinitionOptions{Kind: old.options.Kind, Version: 2, Scope: "conversation", History: "rewindable", Fork: "asOf", Initial: old.options.Initial, Migrate: func(v JSON, _ uint64) (JSON, error) { calls++; v["migrated"] = true; return v, nil }})
		var child Conversation
		_, e = session.Commit(bg, func(tx *Tx) error { var e error; child, e = tx.ForkConversation(1, cut, 0); return e })
		if e != nil {
			t.Fatal(e)
		}
		if calls != 0 {
			t.Fatal("forkinvokedmigration")
		}
		v, ok, e := session.SnapshotDefinitionAsOf(bg, current, child.ID, nil, cut)
		if e != nil || !ok || v["value"] != "at-cut" {
			t.Fatal(v, ok, e)
		}
		_, e = session.Commit(bg, func(tx *Tx) error { _, e := tx.AcquireDocument(current, child.ID, nil, nil); return e })
		if e != nil {
			t.Fatal(e)
		}
		parent, ok, e := session.SnapshotDefinition(bg, old, 1, nil)
		if e != nil || !ok || parent["value"] != "later" {
			t.Fatal(parent, ok, e)
		}
		v, ok, e = session.SnapshotDefinition(bg, current, child.ID, nil)
		if e != nil || !ok || v["migrated"] != true || v["value"] != "at-cut" {
			t.Fatal(v, ok, e)
		}
	})
}

func TestDocumentDefinitionToolCommitProductionAndReopen(t *testing.T) {
	var calls, effects atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		var body map[string]any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"typed-call\",\"type\":\"function\",\"function\":{\"name\":\"typed\",\"arguments\":\"{}\"}}]},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
		} else {
			found := false
			for _, raw := range body["messages"].([]any) {
				m := raw.(map[string]any)
				if m["role"] == "tool" && m["content"] == "typed saved" {
					found = true
				}
			}
			if !found {
				t.Error("typedtoolresultmissing")
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"typed answer\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}
	}))
	defer server.Close()
	store, dir := newJournal(t)
	registry := NewRegistry()
	model := fakeModel(goai.ApiOpenAICompletions)
	model.BaseURL = server.URL
	options := Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }, RequestOptions: func(context.Context, ModelRef) (*goai.StreamOptions, error) {
		return &goai.StreamOptions{APIKey: "LOCAL_TEST"}, nil
	}}
	definition := mustDefinition(t, DefinitionOptions{Kind: "app.typed", Version: 1, Scope: "conversation", History: "rewindable", Fork: "asOf", Initial: func(JSON) (JSON, error) { return JSON{"value": "empty"}, nil }})
	if e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "typed", Description: "save typed document", Parameters: json.RawMessage(`{"type":"object","additionalProperties":false}`)}, Implementation: "typed.native", Version: 1, ReplaySafe: true, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
		effects.Add(1)
		return ToolResult{Content: "typed saved", Commit: func(tx *Tx) error {
			h, e := tx.AcquireDocument(definition, 1, nil, nil)
			if e != nil {
				return e
			}
			return h.Set(JSON{"value": "saved"})
		}}, nil
	}}); e != nil {
		t.Fatal(e)
	}
	h := openHarness(t, store, options)
	r := root(t, h, ModelRef{Provider: model.Provider, ID: model.ID})
	sub, e := r.Submit(bg, Input{Content: "save", RequestID: "typed"})
	if e != nil {
		t.Fatal(e)
	}
	settled := waitSubmission(t, sub)
	if settled.Submission.Status != "done" || settled.Message.Content[0].Text != "typed answer" || effects.Load() != 1 || calls.Load() != 2 {
		t.Fatal(settled, calls.Load(), effects.Load())
	}
	v, ok, e := h.session.SnapshotDefinition(bg, definition, 1, nil)
	if e != nil || !ok || v["value"] != "saved" {
		t.Fatal(v, ok, e)
	}
	if e = h.Close(bg); e != nil {
		t.Fatal(e)
	}
	store, e = OpenJournal(dir, JournalOptions{})
	if e != nil {
		t.Fatal(e)
	}
	h2 := openHarness(t, store, options)
	v, ok, e = h2.session.SnapshotDefinition(bg, definition, 1, nil)
	if e != nil || !ok || v["value"] != "saved" {
		t.Fatal(v, ok, e)
	}
	r, e = h2.Conversation(bg, 1)
	if e != nil {
		t.Fatal(e)
	}
	again, e := r.Submit(bg, Input{Content: "ignored", RequestID: "typed"})
	if e != nil {
		t.Fatal(e)
	}
	waitSubmission(t, again)
	if effects.Load() != 1 || calls.Load() != 2 {
		t.Fatal("typedreceiptredispatched")
	}
}

func TestDocumentDefinitionCacheBoundUnloadAndUnaccessedVersions(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxPage = 2
	store, e := OpenMemory(MemoryOptions{Limits: &limits})
	if e != nil {
		t.Fatal(e)
	}
	session, e := OpenSession(store)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close(bg)
	for i := 0; i < 5; i++ {
		d := mustDefinition(t, DefinitionOptions{Kind: fmt.Sprintf("cache.d%d", i), Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"value": "bounded"}, nil }})
		_, e = session.Commit(bg, func(tx *Tx) error { _, e := tx.AcquireDocument(d, 0, nil, nil); return e })
		if e != nil {
			t.Fatal(e)
		}
		_, _, e = session.SnapshotDefinition(bg, d, 0, nil)
		if e != nil {
			t.Fatal(e)
		}
		if len(session.definitionCache) > 2 || session.definitionCacheBytes > limits.MaxRetainedBytes {
			t.Fatal("unboundeddefinitioncache")
		}
	}
	before, e := session.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	if e = session.UnloadDocuments(bg); e != nil {
		t.Fatal(e)
	}
	after, e := session.Snapshot(bg)
	if e != nil || !equalSnapshot(before, after) {
		t.Fatal("unloadwrote", e)
	}
}

func TestDocumentDefinitionTaskScopeLiveAndPolicyMismatch(t *testing.T) {
	store, e := NewMemory()
	if e != nil {
		t.Fatal(e)
	}
	task := mint(t, store)
	apply(t, store, Write{Op: "put-task", Task: &Task{ID: task, Conversation: 1, Kind: "custom", Status: "pending", Checkpoint: JSON{}}})
	session, e := OpenSession(store)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close(bg)
	def := mustDefinition(t, DefinitionOptions{Kind: "task.doc", Version: 1, Scope: "task", Initial: func(JSON) (JSON, error) { return JSON{"value": "live"}, nil }})
	_, e = session.Commit(bg, func(tx *Tx) error { _, e := tx.AcquireDocument(def, task, nil, nil); return e })
	if e != nil {
		t.Fatal(e)
	}
	_, e = session.Commit(bg, func(tx *Tx) error {
		return tx.PutTask(Task{ID: task, Conversation: 1, Kind: "custom", Status: "done", Checkpoint: JSON{}})
	})
	if e != nil {
		t.Fatal(e)
	}
	_, e = session.Commit(bg, func(tx *Tx) error { _, e := tx.AcquireDocument(def, task, nil, nil); return e })
	rejected(t, e)
	first := mustDefinition(t, DefinitionOptions{Kind: "policy", Version: 1, Scope: "conversation", History: "rewindable", Fork: "asOf", Initial: func(JSON) (JSON, error) { return JSON{}, nil }})
	_, e = session.Commit(bg, func(tx *Tx) error { _, e := tx.AcquireDocument(first, 1, nil, nil); return e })
	if e != nil {
		t.Fatal(e)
	}
	changed := mustDefinition(t, DefinitionOptions{Kind: "policy", Version: 1, Scope: "conversation", History: "latest", Fork: "current", Initial: first.options.Initial})
	_, _, e = session.SnapshotDefinition(bg, changed, 1, nil)
	rejected(t, e)
	_, e = session.Commit(bg, func(tx *Tx) error { _, e := tx.AcquireDocument(changed, 1, nil, nil); return e })
	rejected(t, e)
}

func TestDocumentDefinitionReadOnlyMigrationColdLoadOlderTokenAndRetirement(t *testing.T) {
	store, e := NewMemory()
	if e != nil {
		t.Fatal(e)
	}
	session, e := OpenSession(store)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close(bg)
	old := mustDefinition(t, DefinitionOptions{Kind: "cold", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"value": "old"}, nil }})
	var calls int
	current := mustDefinition(t, DefinitionOptions{Kind: "cold", Version: 2, Scope: "session", Initial: old.options.Initial, Migrate: func(v JSON, _ uint64) (JSON, error) { calls++; v["new"] = true; return v, nil }})
	_, e = session.Commit(bg, func(tx *Tx) error { _, e := tx.AcquireDocument(old, 0, nil, nil); return e })
	if e != nil {
		t.Fatal(e)
	}
	for i := 1; i <= 2; i++ {
		if e = session.UnloadDocuments(bg); e != nil {
			t.Fatal(e)
		}
		v, ok, e := session.SnapshotDefinition(bg, current, 0, nil)
		if e != nil || !ok || v["new"] != true || calls != i {
			t.Fatal(v, ok, e, calls)
		}
		v, ok, e = session.SnapshotDefinition(bg, old, 0, nil)
		if e != nil || !ok || v["new"] != nil {
			t.Fatal("oldertokenforcedinmemoryshape", v, ok, e)
		}
	}
	_, e = session.Commit(bg, func(tx *Tx) error { return tx.RetireDefinition(current, 0, nil) })
	if e != nil {
		t.Fatal(e)
	}
	state, e := session.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, d := range state.Documents {
		if d.Kind == "cold" {
			found = true
			if !d.Retired || d.Version != 2 || d.Value["new"] != true {
				t.Fatal("retirementlostrequiredmigrationbase", d)
			}
		}
	}
	if !found {
		t.Fatal("missingretired")
	}
	_, e = session.Commit(bg, func(tx *Tx) error { return tx.RetireDefinition(current, 0, nil) })
	if e != nil {
		t.Fatal("absentretireisnotnoop", e)
	}
}

func TestDocumentDefinitionCacheOversizedMigrationNotRetained(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxRetainedBytes = 2000
	store, e := OpenMemory(MemoryOptions{Limits: &limits})
	if e != nil {
		t.Fatal(e)
	}
	session, e := OpenSession(store)
	if e != nil {
		t.Fatal(e)
	}
	defer session.Close(bg)
	old := mustDefinition(t, DefinitionOptions{Kind: "cache.large", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"value": "small"}, nil }})
	_, e = session.Commit(bg, func(tx *Tx) error { _, e := tx.AcquireDocument(old, 0, nil, nil); return e })
	if e != nil {
		t.Fatal(e)
	}
	large := mustDefinition(t, DefinitionOptions{Kind: "cache.large", Version: 2, Scope: "session", Initial: old.options.Initial, Migrate: func(JSON, uint64) (JSON, error) { return JSON{"value": strings.Repeat("x", 3000)}, nil }})
	before, e := session.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	v, ok, e := session.SnapshotDefinition(bg, large, 0, nil)
	if e != nil || !ok || len(v["value"].(string)) != 3000 {
		t.Fatal(ok, e)
	}
	if len(session.definitionCache) != 0 || session.definitionCacheBytes != 0 {
		t.Fatal("oversizedmigrationretained")
	}
	_, e = session.Commit(bg, func(tx *Tx) error { _, e := tx.AcquireDocument(large, 0, nil, nil); return e })
	rejected(t, e)
	after, e := session.Snapshot(bg)
	if e != nil || !equalSnapshot(before, after) {
		t.Fatal("oversizedmigrationpersisted", e)
	}
}

func TestDocumentDefinitionInitializerFailureReentrancyAndClose(t *testing.T) {
	store, e := NewMemory()
	if e != nil {
		t.Fatal(e)
	}
	session, e := OpenSession(store)
	if e != nil {
		t.Fatal(e)
	}
	invalid := mustDefinition(t, DefinitionOptions{Kind: "invalid.init", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"invalid": func() {}}, nil }})
	before, e := session.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	_, e = session.Commit(bg, func(tx *Tx) error { _, e := tx.AcquireDocument(invalid, 0, nil, nil); return e })
	rejected(t, e)
	after, e := session.Snapshot(bg)
	if e != nil || !equalSnapshot(before, after) {
		t.Fatal("invalidinitializerwrote", e)
	}
	var active *Tx
	def := mustDefinition(t, DefinitionOptions{Kind: "reentrant.init", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) {
		if _, e := active.MintID(); !errors.Is(e, ErrConcurrent) {
			return nil, fmt.Errorf("reentrancy %v", e)
		}
		return JSON{"value": "owned"}, nil
	}})
	_, e = session.Commit(bg, func(tx *Tx) error { active = tx; _, e := tx.AcquireDocument(def, 0, nil, nil); return e })
	if e != nil {
		t.Fatal(e)
	}
	if e = session.Close(bg); e != nil {
		t.Fatal(e)
	}
	for _, run := range []func() error{
		func() error { _, _, e := session.SnapshotDefinition(bg, def, 0, nil); return e },
		func() error { return session.UnloadDocuments(bg) },
		func() error { _, e := active.AcquireDocument(def, 0, nil, nil); return e },
		func() error { return active.RetireDefinition(def, 0, nil) },
	} {
		e := run()
		if !errors.Is(e, ErrClosed) && !errors.Is(e, ErrSealed) {
			t.Fatal("definitionafterclose", e)
		}
	}
}

func TestDocumentDefinitionHistoricalMigrationDocumentBudget(t *testing.T) {
	for _, name := range []string{"memory", "journal"} {
		t.Run(name, func(t *testing.T) {
			limits := DefaultLimits()
			limits.MaxDocumentBytes, limits.MaxRecordBytes = 512, 2048
			limits.MaxStringBytes, limits.MaxRequestIDBytes = 1024, 512
			var store Storage
			var e error
			if name == "memory" {
				store, e = OpenMemory(MemoryOptions{Limits: &limits})
			} else {
				store, e = OpenJournal(filepath.Join(t.TempDir(), "private"), JournalOptions{Limits: &limits})
			}
			if e != nil {
				t.Fatal(e)
			}
			session, e := OpenSession(store)
			if e != nil {
				t.Fatal(e)
			}
			defer session.Close(bg)
			old := mustDefinition(t, DefinitionOptions{
				Kind: "migration.document-budget", Version: 1, Scope: "conversation", History: "rewindable", Fork: "asOf",
				Initial: func(JSON) (JSON, error) { return JSON{"value": "small"}, nil },
			})
			var cutoff ID
			_, e = session.Commit(bg, func(tx *Tx) error {
				var e error
				cutoff, e = tx.MintID()
				if e != nil {
					return e
				}
				if e = tx.AppendEntry(Entry{ID: cutoff, Conversation: 1, Kind: "note", Value: JSON{}}); e != nil {
					return e
				}
				_, e = tx.AcquireDocument(old, 1, nil, nil)
				return e
			})
			if e != nil {
				t.Fatal(e)
			}
			oversizedValue := JSON{"value": strings.Repeat("x", 700)}
			encoded, e := encodeBounded(oversizedValue, limits, limits.MaxRecordBytes)
			if e != nil || len(encoded) <= limits.MaxDocumentBytes || len(encoded) >= limits.MaxRecordBytes {
				t.Fatal("fixture must be strictly between document and record bounds", len(encoded), e)
			}
			oversized := mustDefinition(t, DefinitionOptions{
				Kind: old.options.Kind, Version: 2, Scope: "conversation", History: "rewindable", Fork: "asOf", Initial: old.options.Initial,
				Migrate: func(JSON, uint64) (JSON, error) { return oversizedValue, nil },
			})
			before, e := session.Snapshot(bg)
			if e != nil {
				t.Fatal(e)
			}
			for _, run := range []func() error{
				func() error { _, _, e := session.SnapshotDefinition(bg, oversized, 1, nil); return e },
				func() error { _, _, e := session.SnapshotDefinitionAsOf(bg, oversized, 1, nil, cutoff); return e },
				func() error {
					_, e := session.Commit(bg, func(tx *Tx) error { _, e := tx.AcquireDocument(oversized, 1, nil, nil); return e })
					return e
				},
			} {
				rejected(t, run())
				after, e := session.Snapshot(bg)
				if e != nil || !equalSnapshot(before, after) {
					t.Fatal("oversized migration changed adopted state", e)
				}
				if session.definitionCacheBytes > limits.MaxRetainedBytes || len(session.definitionCache) > limits.MaxPage {
					t.Fatal("oversized migration exceeded cache bounds")
				}
			}
			if len(session.definitionCache) != 0 || session.definitionCacheBytes != 0 {
				t.Fatal("rejected migration was retained")
			}
			valid := mustDefinition(t, DefinitionOptions{
				Kind: old.options.Kind, Version: 2, Scope: "conversation", History: "rewindable", Fork: "asOf", Initial: old.options.Initial,
				Migrate: func(v JSON, _ uint64) (JSON, error) { v["migrated"] = true; return v, nil },
			})
			value, ok, e := session.SnapshotDefinitionAsOf(bg, valid, 1, nil, cutoff)
			if e != nil || !ok || value["value"] != "small" || value["migrated"] != true {
				t.Fatal("valid historical access after rejection", value, ok, e)
			}
			_, e = session.Commit(bg, func(tx *Tx) error { _, e := tx.AcquireDocument(valid, 1, nil, nil); return e })
			if e != nil {
				t.Fatal("valid typed acquisition after rejection", e)
			}
			value, ok, e = session.SnapshotDefinition(bg, valid, 1, nil)
			if e != nil || !ok || value["value"] != "small" || value["migrated"] != true {
				t.Fatal(value, ok, e)
			}
		})
	}
}
