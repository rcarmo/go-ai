package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCompactionSerializationOmitsSystemImagesAndBoundsToolText(t *testing.T) {
	messages := []MessageReceipt{
		{Role: goai.RoleSystem, Content: []goai.ContentBlock{{Type: "text", Text: "system private"}}},
		{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "question"}, {Type: "image", Data: "IMAGE_PAYLOAD", MimeType: "image/png"}}},
		{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "thinking", Thinking: "reasoning"}, {Type: "text", Text: "answer"}, {Type: "toolCall", ID: "c", Name: "read", Arguments: map[string]any{"path": "/tmp/x", "count": 2}}}},
		{Role: goai.RoleToolResult, Content: []goai.ContentBlock{{Type: "text", Text: strings.Repeat("x", 2500)}}},
	}
	text, err := SerializeConversation(messages, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[User]: question", "[Assistant]: answer", "read(count=2, path=\"/tmp/x\")", strings.Repeat("x", 2000), "[... 500 more characters truncated]"} {
		if !strings.Contains(text, want) {
			t.Error("serialization missing", want)
		}
	}
	if strings.Contains(text, "reasoning") || strings.Contains(text, "[Assistant thinking]") || strings.Contains(text, "system private") || strings.Contains(text, "IMAGE_PAYLOAD") || strings.Contains(text, strings.Repeat("x", 2001)) {
		t.Fatal("serialization leaked unbounded/control content")
	}
	unicode := truncateCompactionText(strings.Repeat("🙂", 1001), 2000)
	if !strings.HasPrefix(unicode, strings.Repeat("🙂", 1000)) || !strings.Contains(unicode, "2 more characters truncated") {
		t.Fatal("unicode truncation", unicode)
	}
}

func TestSourceCompactionSerializationOmitsThinking(t *testing.T) {
	// Literal harness-compaction.test.ts:351-370 at f9a2636; every original
	// positive and negative assertion is retained.
	empty, hidden, hello := "", "hidden", "hello"
	messages := []MessageReceipt{
		{Role: goai.RoleSystem, ContentText: &empty, Sections: map[string]*string{"s": &hidden}},
		{Role: goai.RoleUser, ContentText: &hello},
		{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "thinking", Thinking: "hmm"}, {Type: "text", Text: "sure"}, {Type: "toolCall", ID: "c", Name: "read", Arguments: JSON{"path": "a.ts"}}}},
		{Role: goai.RoleToolResult, ToolCallID: "c", Content: []goai.ContentBlock{{Type: "text", Text: strings.Repeat("y", 2500)}}},
	}
	text, err := SerializeConversation(messages, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"hidden", "hmm", "[Assistant thinking]"} {
		if strings.Contains(text, absent) {
			t.Errorf("source transcript retained %q", absent)
		}
	}
	for _, present := range []string{"[User]: hello", "[Assistant]: sure", `[Assistant tool calls]: read(path="a.ts")`, "[Tool result]: " + strings.Repeat("y", 2000) + "\n\n[... 500 more characters truncated]"} {
		if !strings.Contains(text, present) {
			t.Errorf("source transcript omitted %q", present)
		}
	}
}

// Literal source string arm from f9a2636 src/harness/compaction.ts:385-417.
func TestCompactionSerializationPreservesSourceStringContent(t *testing.T) {
	question, empty, system := "question", "", "private"
	text, err := SerializeConversation([]MessageReceipt{
		{Role: goai.RoleSystem, ContentText: &system},
		{Role: goai.RoleUser, ContentText: &question},
		{Role: goai.RoleUser, ContentText: &empty},
	}, DefaultLimits())
	if err != nil || text != "[User]: question" {
		t.Fatalf("source string transcript = %q, err=%v", text, err)
	}
}

func TestSourceCompactionTruncationKeepsUTF16SliceBoundary(t *testing.T) {
	// Source compaction.ts:419-422 uses String.slice, not scalar truncation.
	input := strings.Repeat("x", 1999) + "🙂"
	want := "[Tool result]: " + strings.Repeat("x", 1999) + string([]byte{0xed, 0xa0, 0xbd}) + "\n\n[... 1 more characters truncated]"
	got, err := SerializeConversation([]MessageReceipt{{Role: goai.RoleToolResult, Content: []goai.ContentBlock{{Type: "text", Text: input}}}}, DefaultLimits())
	if err != nil || got != want {
		t.Fatalf("source UTF-16 slice = %q; expected %q; err=%v", got, want, err)
	}
	limits := DefaultLimits()
	block := compactionRequestBlock(got, nil, true)
	encoded, err := encodeBounded(block, limits, limits.MaxRecordBytes)
	if err != nil || !strings.Contains(string(encoded), `\ud83d`) {
		t.Fatal("provider encoding lost the isolated source unit", string(encoded), err)
	}
	ordinary := goai.ContentBlock{Type: "text", Text: got}
	if _, err := encodeBounded(ordinary, limits, limits.MaxRecordBytes); err == nil {
		t.Fatal("source truncation widened ordinary text authority")
	}

}

func TestCompactionPromptsMatchPinnedSourceLiterals(t *testing.T) {
	data, err := os.ReadFile("testdata/pi-durable-v150/compaction-prompts.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Target, System, User string }
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Target != "f9a26365568127461c47a07a37dcb42c140a3ac5" {
		t.Fatal("wrong source target")
	}
	if compactionSystemPrompt != fixture.System {
		t.Error("summarization system prompt differs from source")
	}
	empty, focus := "", "preserve paths"
	for _, instructions := range []*string{nil, &empty, &focus} {
		want := "<conversation>\n[User]: hello\n</conversation>\n\n" + fixture.User
		if instructions != nil {
			want += "\n\nAdditional focus: " + *instructions
		}
		if got := compactionRequestText("[User]: hello", instructions); got != want {
			t.Errorf("source summary request differs: %q", got)
		}
	}
}

func TestCompactionReopenPinsRequestButRehydratesModel(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		entered := make(chan struct{})
		var calls atomic.Int64
		var changed atomic.Bool
		ref, options := setupFakeAPI(t, func(ctx context.Context, model *goai.Model, input *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
			n := calls.Add(1)
			wantWindow := 128000
			if n == 2 {
				wantWindow = 123
			}
			if model.ContextWindow != wantWindow || opts.MaxTokens == nil || *opts.MaxTokens != 100 || opts.TimeoutMs == nil || *opts.TimeoutMs != 1234 || opts.Deferred != nil {
				t.Error("summary behavior changed", model.ContextWindow, opts)
			}
			if !strings.Contains(input.Messages[0].Content[0].Text, "<conversation>\n[User]: old") || !strings.Contains(input.Messages[0].Content[0].Text, "Additional focus: paths") {
				t.Error("summary transcript", input)
			}
			if n == 2 && (model.BaseURL != "https://new.invalid" || model.APIKey != "runtime-secret") {
				t.Error("transport not rehydrated", model.BaseURL)
			}
			ch := make(chan goai.Event, 1)
			if n == 1 {
				go func() { close(entered); <-ctx.Done(); close(ch) }()
			} else {
				ch <- terminal("pinned summary")
				close(ch)
			}
			return ch
		})
		original := options.Models
		options.Models = func(provider goai.Provider, id string) *goai.Model {
			model := *original(provider, id)
			if changed.Load() {
				model.ContextWindow = 123
				model.BaseURL = "https://new.invalid"
				model.APIKey = "runtime-secret"
			}
			return &model
		}
		first := openHarness(t, b.store, options)
		timeout := 1234
		conversation, err := first.Root(bg, AgentChange{Model: ref, Settings: RequestSettings{TimeoutMs: &timeout, Deferred: &goai.DeferredOptions{}}})
		if err != nil {
			t.Fatal(err)
		}
		var recent ID
		_, err = conversation.Commit(bg, func(tx *Tx) (any, error) {
			for _, text := range []string{"old", "recent"} {
				id, err := tx.MintID()
				if err != nil {
					return nil, err
				}
				if text == "recent" {
					recent = id
				}
				wire, err := dtoObject(userReceipt(text), tx.limits)
				if err != nil {
					return nil, err
				}
				if err = tx.AppendEntry(Entry{ID: id, Conversation: conversation.ID(), Kind: "message", Value: wire}); err != nil {
					return nil, err
				}
			}
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		configureCompactionTestPolicy(t, conversation, compactionTestInt(goai.EstimateTextTokens("recent")), compactionTestInt((100*5+3)/4))
		instructions := "paths"
		id, err := conversation.Compact(bg, &instructions)
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		heldState, err := first.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range heldState.Tasks {
			if task.Kind != "pi.compaction" {
				continue
			}
			record, err := CanonicalTask(task, first.session.limits)
			if err != nil {
				t.Fatal(err)
			}
			checkpoint := taskCheckpointMap(record.State.Checkpoint)
			want := JSON{"phase": "summarize", "attempt": 1, "model": JSON{"provider": string(ref.Provider), "modelId": ref.ID}, "thinkingLevel": "off", "streamOptions": JSON{"timeoutMs": 1234, "deferred": true}, "maxTokens": 100, "tail": int(recent), "firstKept": int(recent)}
			if !equalJSONValue(checkpoint, want) {
				t.Errorf("noncanonical summary request checkpoint: %#v", checkpoint)
			}
			for _, private := range []string{"pinnedModel", "retry"} {
				if _, exists := checkpoint[private]; exists {
					t.Errorf("source request must not persist %s snapshot", private)
				}
			}
		}
		if err := first.Close(bg); err != nil {
			t.Fatal(err)
		}
		changed.Store(true)
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		if result := waitPublicTask(t, second, id); result.State.Outcome.Status != "completed" {
			t.Fatal(result)
		}
		state, err := second.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range state.Entries {
			if entry.Kind == "pi.compaction" {
				found = true
				if legacyEntryValue(entry)["reason"] != "manual" || entry.Model[0].Timestamp == 0 {
					t.Fatal("compaction metadata missing", entry)
				}
			}
		}
		if !found || calls.Load() != 2 {
			t.Fatal("summary not recovered", found, calls.Load())
		}
	})
}

// Source manual compact has only optional instructions. Test-specific policy
// belongs in the agent settings, never in a second per-call options contract.
func configureCompactionTestPolicy(t *testing.T, conversation *ConversationHandle, keep, reserve *int) {
	t.Helper()
	state, err := conversation.h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	doc, ok := agentDocument(state, conversation.ID())
	if !ok {
		t.Fatal("missing agent settings")
	}
	var agent agentState
	if err := fromObject(doc.Value, &agent, conversation.h.session.limits); err != nil {
		t.Fatal(err)
	}
	settings := agent.Settings
	overrides := CompactionSettings{}
	if settings.CompactionOverrides != nil {
		overrides = *settings.CompactionOverrides
	}
	if keep != nil {
		overrides.KeepRecentTokens = keep
	}
	if reserve != nil {
		overrides.ReserveTokens = reserve
	}
	settings.CompactionOverrides = &overrides
	if err := conversation.ConfigurePatch(bg, AgentPatch{Settings: &settings}); err != nil {
		t.Fatal(err)
	}
}
func compactionTestInt(value int) *int { return &value }

func TestSourceManualCompactionInstructionPresence(t *testing.T) {
	// Source index.ts exports CompactionInput from compaction.ts; it is not
	// the larger anonymous object passed to beforeCompact in harness/types.ts.
	if typ := reflect.TypeFor[CompactionInput](); typ.NumField() != 2 {
		t.Error("task CompactionInput must contain only reason and instructions", typ)
	}
	empty, focus := "", "preserve paths"
	for _, test := range []struct {
		name         string
		instructions *string
	}{{"absent", nil}, {"empty", &empty}, {"nonempty", &focus}} {
		t.Run(test.name, func(t *testing.T) {
			wire, err := json.Marshal(CompactionInput{Reason: "manual", Instructions: test.instructions})
			want := JSON{"reason": "manual"}
			if test.instructions != nil {
				want["instructions"] = *test.instructions
			}
			if err != nil || !equalJSONValue(dropinJSON(t, wire), want) {
				t.Error("public CompactionInput wire differs from original", string(wire), err)
			}
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			requests := make(chan string, 1)
			ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				if input.SystemPrompt != compactionSystemPrompt {
					t.Error("not the exact source system prompt")
				}
				requests <- input.Messages[0].Content[0].Text
				ch := make(chan goai.Event, 1)
				ch <- terminal("summary")
				close(ch)
				return ch
			})
			options.Registry = NewRegistry()
			if err := options.Registry.Install(&Extension{Name: "instruction-owner", CompactionHooks: CompactionHooks{BeforeCompact: func(_ context.Context, input BeforeCompactInput, _ *HookAPI) (*CompactionDecision, error) {
				if (input.Instructions == nil) != (test.instructions == nil) {
					t.Error("hook lost instruction presence")
				}
				if input.Instructions != nil {
					if *input.Instructions != *test.instructions {
						t.Error("hook input changed")
					}
					*input.Instructions = "hook mutation"
				}
				return nil, nil
			}}}); err != nil {
				t.Fatal(err)
			}
			if err := options.Registry.Install(&Extension{Name: "instruction-observer", CompactionHooks: CompactionHooks{BeforeCompact: func(_ context.Context, input BeforeCompactInput, _ *HookAPI) (*CompactionDecision, error) {
				if (input.Instructions == nil) != (test.instructions == nil) {
					t.Error("later hook lost instruction presence")
				}
				if input.Instructions != nil && test.instructions != nil && *input.Instructions != *test.instructions {
					t.Error("earlier hook mutation escaped into later hook", *input.Instructions)
				}
				return nil, nil
			}}}); err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, store, options)
			c := root(t, h, ref)
			configureCompactionTestPolicy(t, c, compactionTestInt(0), nil)
			if _, err := c.Commit(bg, func(tx *Tx) (any, error) {
				for _, text := range []string{"old", "recent"} {
					id, err := tx.MintID()
					if err != nil {
						return nil, err
					}
					if err := tx.AppendEntry(Entry{ID: id, Conversation: c.ID(), Kind: "pi.user", Model: []MessageReceipt{userReceipt(text)}}); err != nil {
						return nil, err
					}
				}
				return nil, nil
			}); err != nil {
				t.Fatal(err)
			}
			var instructions *string
			wantInput := JSON{"reason": "manual"}
			if test.instructions != nil {
				value := *test.instructions
				instructions = &value
				wantInput["instructions"] = value
			}
			id, err := c.Compact(bg, instructions)
			if err != nil {
				t.Fatal(err)
			}
			if instructions != nil {
				*instructions = "caller mutation"
			}
			record := waitPublicTask(t, h, id)
			if record.State.Outcome.Status != "completed" || !equalJSONValue(record.Input.Value, wantInput) {
				t.Fatal("noncanonical compaction input", record)
			}
			request := watchReceive(t, requests)
			suffix := ""
			if test.instructions != nil {
				suffix = "\n\nAdditional focus: " + *test.instructions
			}
			if !strings.HasSuffix(request, compactionPrompt+suffix) {
				t.Fatal("instruction presence changed across durable phases", request)
			}
		})
	}
}

func TestOriginalCompactionSummaryRequestExecutesAfterSQLiteReopen(t *testing.T) {
	for _, name := range []string{"compaction-request.json", "compaction-retry.json"} {
		t.Run(name, func(t *testing.T) { testOriginalCompactionCheckpoint(t, name) })
	}
}

func testOriginalCompactionCheckpoint(t *testing.T, name string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata/pi-durable-v150", name))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Target        string
		Conversations []Conversation
		Entries       []Entry
		Tasks         []TaskRecord
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Target != "f9a26365568127461c47a07a37dcb42c140a3ac5" || len(fixture.Tasks) != 1 {
		t.Fatal("invalid original fixture")
	}
	path := filepath.Join(t.TempDir(), "summary.db")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	batches := make([][]StorageWrite, 3)
	for _, record := range fixture.Conversations {
		batches[0] = append(batches[0], StorageWrite{Type: "conversation", Value: record})
	}
	for _, record := range fixture.Entries {
		batches[1] = append(batches[1], StorageWrite{Type: "entry", Value: record})
	}
	for _, record := range fixture.Tasks {
		batches[2] = append(batches[2], StorageWrite{Type: "task", Value: record})
	}
	result, err := store.Commit(bg, batches, nil)
	if err != nil || result.Stored != 3 || result.Refused != nil {
		t.Fatal(result, err)
	}
	var original struct{ Tasks []json.RawMessage }
	if err := json.Unmarshal(data, &original); err != nil {
		t.Fatal(err)
	}
	stored, found, err := store.Task(bg, fixture.Tasks[0].ID)
	if err != nil || !found {
		t.Fatal(stored, found, err)
	}
	encoded, err := json.Marshal(stored)
	if err != nil || !reflect.DeepEqual(dropinJSON(t, encoded), dropinJSON(t, original.Tasks[0])) {
		t.Fatal("original task record changed before execution", string(encoded), err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	fakeRef, options := setupFakeAPI(t, func(_ context.Context, model *goai.Model, input *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
		requests.Add(1)
		if model.Provider != "faux" || model.ID != "faux-1" || opts.MaxTokens == nil || *opts.MaxTokens != 100 || opts.TimeoutMs == nil || *opts.TimeoutMs != 1234 || opts.Deferred != nil {
			t.Error("original request changed", model, opts)
		}
		text := input.Messages[0].Content[0].Text
		if !strings.HasPrefix(text, "<conversation>\n[User]: old\n</conversation>") || !strings.HasSuffix(text, "\n\nAdditional focus: paths") {
			t.Error("original historical range changed", text)
		}
		ch := make(chan goai.Event, 1)
		ch <- terminal("source request resumed")
		close(ch)
		return ch
	})
	resolve := options.Models
	options.Models = func(provider goai.Provider, id string) *goai.Model {
		if provider != "faux" || id != "faux-1" {
			return nil
		}
		model := *resolve(fakeRef.Provider, fakeRef.ID)
		model.Provider = provider
		model.ID = id
		return &model
	}
	clock := int64(42)
	if fixture.Tasks[0].State.Until != nil {
		clock = *fixture.Tasks[0].State.Until
	}
	options.Now = func() int64 { return clock }
	h := openHarness(t, store, options)
	// This selected-record fixture is not a complete source document-store
	// snapshot. Initialise missing harness builtins through the public root API.
	if _, err := h.Root(bg, AgentChange{Model: ModelRef{Provider: "faux", ID: "faux-1"}}); err != nil {
		t.Fatal(err)
	}
	if err := h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	record := waitPublicTask(t, h, fixture.Tasks[0].ID)
	if record.State.Outcome.Status != "completed" || requests.Load() != 1 {
		t.Fatalf("original checkpoint outcome=%+v error=%+v requests=%d", record.State.Outcome, record.State.Outcome.Error, requests.Load())
	}
}

// Source compaction.ts:summaryText uses String.trim, whose whitespace set
// includes BOM and excludes NEL. These are real provider/placement controls.
func TestSourceCompactionProviderSummaryTrim(t *testing.T) {
	for _, test := range []struct {
		name, text, want string
		completed        bool
	}{
		{"bom-wrapped", "\ufeff summary \ufeff", "summary", true},
		{"nel-is-text", "\u0085", "\u0085", true},
		{"bom-only", "\ufeff", "", false},
		{"mongolian-vowel-is-text", "\u180e", "\u180e", true},
		{"ordinary-whitespace", " \t\n", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				ch <- terminal(test.text)
				close(ch)
				return ch
			})
			h := openHarness(t, store, options)
			c := root(t, h, ref)
			configureCompactionTestPolicy(t, c, compactionTestInt(0), nil)
			if _, err := c.Commit(bg, func(tx *Tx) (any, error) {
				for _, text := range []string{"old", "recent"} {
					id, err := tx.MintID()
					if err != nil {
						return nil, err
					}
					if err := tx.AppendEntry(Entry{ID: id, Conversation: c.ID(), Kind: "pi.user", Model: []MessageReceipt{userReceipt(text)}}); err != nil {
						return nil, err
					}
				}
				return nil, nil
			}); err != nil {
				t.Fatal(err)
			}
			id, err := c.Compact(bg, nil)
			if err != nil {
				t.Fatal(err)
			}
			record := waitPublicTask(t, h, id)
			view, err := c.ContextView(bg, 0)
			if err != nil {
				t.Fatal(err)
			}
			if test.completed {
				if record.State.Outcome.Status != "completed" || view.Head == nil || view.Messages[0].Content[0].Text != compactionSummaryPrefix+test.want+"\n</summary>" {
					t.Fatal("source summary changed", record.State.Outcome, view)
				}
			} else if record.State.Outcome.Status != "failed" || record.State.Outcome.Error == nil || record.State.Outcome.Error.Message != "Summarization produced no text" || view.Head != nil {
				t.Fatal("source empty summary accepted", record.State.Outcome, view)
			}
		})
	}
}
