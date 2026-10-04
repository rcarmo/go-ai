package durable

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fakeModel(api goai.Api) *goai.Model {
	return &goai.Model{ID: "durable-test", Provider: goai.ProviderOpenAI, Api: api, BaseURL: "https://unreachable.invalid", ContextWindow: 8192, MaxTokens: 256}
}
func terminal(text string) *goai.DoneEvent {
	return &goai.DoneEvent{Reason: goai.StopReasonStop, Message: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: text}}, StopReason: goai.StopReasonStop, Usage: &goai.Usage{Input: 3, Output: 2, TotalTokens: 5}}}
}
func setupFakeAPI(t *testing.T, fn goai.ProviderStream) (ModelRef, Options) {
	t.Helper()
	api := goai.Api("durable-test-" + strings.ReplaceAll(t.Name(), "/", "-"))
	goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: fn})
	t.Cleanup(func() { goai.UnregisterApi(api) })
	ref := ModelRef{Provider: goai.ProviderOpenAI, ID: "durable-test"}
	options := Options{Models: func(goai.Provider, string) *goai.Model { return fakeModel(api) }}
	return ref, options
}
func openHarness(t *testing.T, s Storage, options Options) *Harness {
	t.Helper()
	h, e := Open(bg, s, options)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = h.Close(bg) })
	return h
}
func root(t *testing.T, h *Harness, ref ModelRef) *ConversationHandle {
	t.Helper()
	r, e := h.Root(bg, AgentChange{Model: ref, SystemPrompt: "system"})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func waitSubmission(t *testing.T, s *SubmissionHandle) Settlement {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	settled, e := s.Wait(ctx)
	if e != nil {
		t.Fatal(e)
	}
	return settled
}

func TestM1bProductionHTTPPersistentGeneration(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer SECRET_SENTINEL" {
			t.Errorf("missing fakecredential")
		}
		var body map[string]any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		if body["model"] != "durable-test" {
			t.Error(body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"response\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"persistent answer\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"response\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "private")
	store, e := OpenJournal(dir, JournalOptions{})
	if e != nil {
		t.Fatal(e)
	}
	model := fakeModel(goai.ApiOpenAICompletions)
	model.BaseURL = server.URL
	options := Options{Models: func(goai.Provider, string) *goai.Model { return model }, RequestOptions: func(context.Context, ModelRef) (*goai.StreamOptions, error) {
		return &goai.StreamOptions{APIKey: "SECRET_SENTINEL", Headers: map[string]string{"x-sensitive": "HEADER_SENTINEL"}}, nil
	}}
	h := openHarness(t, store, options)
	r := root(t, h, ModelRef{goai.ProviderOpenAI, model.ID})
	sub, e := r.Submit(bg, Input{Content: "hello", RequestID: "request-1"})
	if e != nil {
		t.Fatal(e)
	}
	result := waitSubmission(t, sub)
	if result.Submission.Status != "done" || result.Message == nil || result.Message.Content[0].Text != "persistent answer" || result.Message.Usage.TotalTokens != 5 {
		t.Fatalf("%+v", result)
	}
	snapshot, e := h.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	if len(snapshot.Entries) != 2 || len(snapshot.Tasks) != 1 || len(snapshot.Submissions) != 1 {
		t.Fatal(snapshot)
	}
	assistantSeq := uint64(0)
	for _, entry := range snapshot.Entries {
		if entry.Value["role"] == string(goai.RoleAssistant) {
			assistantSeq = entry.Seq
		}
	}
	if assistantSeq != snapshot.Seq {
		t.Fatal("terminal not atomicallyadopted")
	}
	if e = h.Close(bg); e != nil {
		t.Fatal(e)
	}
	journal, e := os.ReadFile(filepath.Join(dir, "journal.bin"))
	if e != nil {
		t.Fatal(e)
	}
	for _, sentinel := range []string{"SECRET_SENTINEL", "HEADER_SENTINEL", server.URL} {
		if strings.Contains(string(journal), sentinel) {
			t.Fatalf("persisted process-local secret/endpoint %s", sentinel)
		}
	}
	store, e = OpenJournal(dir, JournalOptions{})
	if e != nil {
		t.Fatal(e)
	}
	h2 := openHarness(t, store, options)
	r2, e := h2.Conversation(bg, 1)
	if e != nil {
		t.Fatal(e)
	}
	again, e := r2.Submit(bg, Input{Content: "different ignored idempotentretry", RequestID: "request-1"})
	if e != nil || again.ID() != sub.ID() {
		t.Fatal(e)
	}
	settled := waitSubmission(t, again)
	if settled.Submission.Status != "done" || calls.Load() != 1 {
		t.Fatal("alreadyterminal redispatched", calls.Load())
	}
}

func TestM1bTerminalAndExplicitUnsupported(t *testing.T) {
	for _, mode := range []string{"normal", "error-usage", "missing", "nilmessage", "duplicate", "tool", "deferred", "invalidusage"} {
		t.Run(mode, func(t *testing.T) {
			ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 3)
				m := terminal("answer")
				switch mode {
				case "normal":
					ch <- m
				case "error-usage":
					m.Message.StopReason = goai.StopReasonError
					ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: m.Message, Err: fmt.Errorf("SECRET_ERROR_SENTINEL")}
				case "missing":
				case "nilmessage":
					ch <- &goai.DoneEvent{}
				case "duplicate":
					ch <- m
					ch <- m
				case "tool":
					m.Message.StopReason = goai.StopReasonToolUse
					m.Message.Content = []goai.ContentBlock{{Type: "toolCall", ID: "call", Name: "danger"}}
					ch <- m
				case "deferred":
					m.Message.StopReason = goai.StopReasonDeferred
					m.Message.Deferred = &goai.DeferredHandle{ID: "SECRET_HANDLE"}
					ch <- m
				case "invalidusage":
					m.Message.Usage.Input = -1
					ch <- m
				}
				close(ch)
				return ch
			})
			s, _ := NewMemory()
			h := openHarness(t, s, options)
			r := root(t, h, ref)
			sub, e := r.Submit(bg, Input{Content: "prompt", RequestID: mode})
			if e != nil {
				t.Fatal(e)
			}
			result := waitSubmission(t, sub)
			want := "failed"
			if mode == "normal" {
				want = "done"
			}
			if result.Submission.Status != want {
				t.Fatalf("%s got%s", mode, result.Submission.Status)
			}
			if mode == "error-usage" && (result.Message == nil || result.Message.Usage == nil || result.Message.Usage.TotalTokens != 5) {
				t.Fatal("lost billed usage")
			}
			snapshot, e := h.Snapshot(bg)
			if e != nil {
				t.Fatal(e)
			}
			data, _ := json.Marshal(snapshot)
			if strings.Contains(string(data), "SECRET_") {
				t.Fatal("secret error/handle persisted")
			}
		})
	}
}

func TestM1bIntentBeforeDispatchAndPinnedContext(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	var h *Harness
	ref, options := setupFakeAPI(t, func(ctx context.Context, m *goai.Model, c *goai.Context, o *goai.StreamOptions) <-chan goai.Event {
		calls.Add(1)
		s, e := h.Snapshot(bg)
		if e != nil {
			t.Error(e)
		}
		found := false
		for _, task := range s.Tasks {
			var cp generationCheckpoint
			_ = fromObject(task.Checkpoint, &cp, h.session.limits)
			if cp.Phase == "intent" && cp.Attempt == 1 && len(cp.Messages) == 1 {
				found = true
			}
		}
		if !found {
			t.Error("effect before committed intent")
		}
		if c.SystemPrompt != "system" || c.Messages[0].Content[0].Text != "first" {
			t.Error(c)
		}
		close(entered)
		ch := make(chan goai.Event)
		go func() { <-release; ch <- terminal("answer"); close(ch) }()
		return ch
	})
	s, _ := NewMemory()
	h = openHarness(t, s, options)
	r := root(t, h, ref)
	sub, e := r.Submit(bg, Input{Content: "first"})
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	if e = r.Configure(bg, AgentChange{Model: ref, SystemPrompt: "changed"}); e != nil {
		t.Fatal(e)
	}
	snapshot, e := h.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	for _, task := range snapshot.Tasks {
		var cp generationCheckpoint
		_ = fromObject(task.Checkpoint, &cp, h.session.limits)
		if cp.Agent.SystemPrompt != "system" {
			t.Fatal("inflight configuration changed")
		}
	}
	close(release)
	if waitSubmission(t, sub).Submission.Status != "done" || calls.Load() != 1 {
		t.Fatal("badsettlement")
	}
}

func TestM1bUnpinnedBehaviorOptionsRejectBeforeEffects(t *testing.T) {
	for _, mode := range []string{"sampling", "reasoning", "toolchoice", "deferred", "temperature"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				calls.Add(1)
				ch := make(chan goai.Event)
				close(ch)
				return ch
			})
			options.RequestOptions = func(context.Context, ModelRef) (*goai.StreamOptions, error) {
				o := &goai.StreamOptions{APIKey: "SECRET"}
				switch mode {
				case "sampling":
					o.SamplingParams = map[string]any{"top_p": 0.1}
				case "reasoning":
					v := goai.ThinkingLevel("high")
					o.Reasoning = &v
				case "toolchoice":
					o.ToolChoice = goai.ToolChoiceRequired
				case "deferred":
					o.Deferred = &goai.DeferredOptions{}
				case "temperature":
					v := 0.5
					o.Temperature = &v
				}
				return o, nil
			}
			s, _ := NewMemory()
			h := openHarness(t, s, options)
			r := root(t, h, ref)
			sub, e := r.Submit(bg, Input{Content: "options"})
			if e != nil {
				t.Fatal(e)
			}
			if waitSubmission(t, sub).Submission.Status != "failed" || calls.Load() != 0 {
				t.Fatal("unfrozen behavior dispatched")
			}
		})
	}
}

func TestM1bPayloadMutationHookUnsupported(t *testing.T) {
	var effects, hooks atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		effects.Add(1)
		ch := make(chan goai.Event)
		close(ch)
		return ch
	})
	options.RequestOptions = func(context.Context, ModelRef) (*goai.StreamOptions, error) {
		return &goai.StreamOptions{OnPayload: func(any, *goai.Model) (any, error) { hooks.Add(1); return map[string]any{"model": "changed"}, nil }}, nil
	}
	s, _ := NewMemory()
	h := openHarness(t, s, options)
	r := root(t, h, ref)
	sub, e := r.Submit(bg, Input{Content: "pin"})
	if e != nil {
		t.Fatal(e)
	}
	if waitSubmission(t, sub).Submission.Status != "failed" || effects.Load() != 0 || hooks.Load() != 0 {
		t.Fatal("rewritehook invoked")
	}
}

func TestM1bCommittedBuiltinsQueueAndUsage(t *testing.T) {
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		calls.Add(1)
		ch := make(chan goai.Event, 1)
		done := terminal("answer")
		done.Message.Provider = refProvider()
		done.Message.Model = "durable-test"
		ch <- done
		close(ch)
		return ch
	})
	s, _ := NewMemory()
	h := openHarness(t, s, options)
	r := root(t, h, ref)
	for k := 0; k < 2; k++ {
		sub, e := r.Submit(bg, Input{Content: "spend"})
		if e != nil {
			t.Fatal(e)
		}
		if waitSubmission(t, sub).Submission.Status != "done" {
			t.Fatal("failed")
		}
	}
	state, e := h.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"pi.agent", "pi.live", "pi.inbox", "pi.usage"} {
		var found Document
		for _, d := range state.Documents {
			if d.Kind == kind {
				found = d
			}
		}
		if found.ID == 0 || found.Version != 1 {
			t.Fatal("builtinmissing", kind)
		}
		switch kind {
		case "pi.live":
			if len(found.Value) != 0 {
				t.Fatal("run notcleared")
			}
		case "pi.inbox":
			if len(found.Value["items"].([]any)) != 0 {
				t.Fatal("settledinputstillqueued")
			}
		case "pi.usage":
			models := found.Value["models"].(map[string]any)
			var usage goai.Usage
			if e = fromObject(JSON(models["openai/durable-test"].(map[string]any)), &usage, h.session.limits); e != nil || usage.TotalTokens != 10 {
				t.Fatal("usage notsummedonce", usage, e)
			}
		}
	}
}
func refProvider() goai.Provider { return goai.ProviderOpenAI }

func TestM1bProducerMutationAfterTerminalDetach(t *testing.T) {
	h := &Harness{session: &Session{limits: DefaultLimits()}}
	message := terminal("stable").Message
	ch := make(chan goai.Event, 1)
	ch <- &goai.DoneEvent{Message: message}
	close(ch)
	receipt, success, received := h.drain(ch, fakeModel("producer-mutation"))
	if !success || !received {
		t.Fatal("failedcapture")
	}
	message.Content[0].Text = "producerchanged"
	message.Usage.Input = 999
	if receipt.Content[0].Text != "stable" || receipt.Usage.Input != 3 {
		t.Fatal("terminalaliasauthority")
	}
}

func TestM1bPinnedTerminalIdentityUsageAndReopen(t *testing.T) {
	for _, mode := range []string{"missing", "conflicting"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int64
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				calls.Add(1)
				done := terminal("attributed answer")
				if mode == "conflicting" {
					done.Message.Provider = goai.ProviderAnthropic
					done.Message.Model = "wrong-model"
					done.Message.Api = goai.ApiAnthropicMessages
				}
				ch := make(chan goai.Event, 1)
				ch <- done
				close(ch)
				return ch
			})
			dir := filepath.Join(t.TempDir(), "private")
			store, e := OpenJournal(dir, JournalOptions{})
			if e != nil {
				t.Fatal(e)
			}
			h := openHarness(t, store, options)
			r := root(t, h, ref)
			var first ID
			check := func(h *Harness, want int) {
				t.Helper()
				state, e := h.Snapshot(bg)
				if e != nil {
					t.Fatal(e)
				}
				for _, entry := range state.Entries {
					if entry.Value["role"] == string(goai.RoleAssistant) {
						var message MessageReceipt
						if e = fromObject(entry.Value, &message, h.session.limits); e != nil {
							t.Fatal(e)
						}
						if message.Provider != ref.Provider || message.Model != ref.ID || message.Api != options.Models(ref.Provider, ref.ID).Api {
							t.Fatal("terminal identity not pinned", message)
						}
					}
				}
				for _, d := range state.Documents {
					if d.Kind == "pi.usage" {
						models := d.Value["models"].(map[string]any)
						if len(models) != 1 {
							t.Fatal("cross-model usage buckets", models)
						}
						value, ok := models[string(ref.Provider)+"/"+ref.ID].(map[string]any)
						if !ok {
							t.Fatal("missing pinned usage bucket", models)
						}
						var usage goai.Usage
						if e = fromObject(JSON(value), &usage, h.session.limits); e != nil || usage.TotalTokens != want {
							t.Fatal("pinned aggregate", usage, e)
						}
					}
				}
			}
			for k := 0; k < 2; k++ {
				sub, e := r.Submit(bg, Input{Content: "identity", RequestID: fmt.Sprintf("identity-%d", k)})
				if e != nil {
					t.Fatal(e)
				}
				if k == 0 {
					first = sub.ID()
				}
				result := waitSubmission(t, sub)
				if result.Submission.Status != "done" || result.Message.Provider != ref.Provider || result.Message.Model != ref.ID {
					t.Fatal("receipt attribution", result)
				}
			}
			check(h, 10)
			if e = h.Close(bg); e != nil {
				t.Fatal(e)
			}
			store, e = OpenJournal(dir, JournalOptions{})
			if e != nil {
				t.Fatal(e)
			}
			h2 := openHarness(t, store, options)
			check(h2, 10)
			r2, e := h2.Conversation(bg, 1)
			if e != nil {
				t.Fatal(e)
			}
			again, e := r2.Submit(bg, Input{Content: "ignored", RequestID: "identity-0"})
			if e != nil || again.ID() != first {
				t.Fatal(e)
			}
			if waitSubmission(t, again).Submission.Status != "done" || calls.Load() != 2 {
				t.Fatal("terminal identity retry redispatched")
			}
			check(h2, 10)
			next, e := r2.Submit(bg, Input{Content: "third", RequestID: "identity-2"})
			if e != nil {
				t.Fatal(e)
			}
			if waitSubmission(t, next).Submission.Status != "done" || calls.Load() != 3 {
				t.Fatal("postreopen generation")
			}
			check(h2, 15)
		})
	}
}

func TestM1bLargeLocalModelAuthorizationHTTP(t *testing.T) {
	header := "Bearer LOCAL_MODEL_HEADER_SENTINEL_" + strings.Repeat("x", 600<<10)
	var calls atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != header {
			t.Error("large local model Authorization not sent")
		}
		var body map[string]any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		if body["model"] != "durable-test" || body["temperature"] != 0.25 || body["max_tokens"] != float64(77) || body["top_p"] != 0.5 {
			t.Error("pinned request behavior changed", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"large header answer\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: [DONE]\n\n")
	}))
	server.Config.MaxHeaderBytes = 2 << 20
	server.Start()
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "private")
	store, e := OpenJournal(dir, JournalOptions{})
	if e != nil {
		t.Fatal(e)
	}
	model := fakeModel(goai.ApiOpenAICompletions)
	model.BaseURL = server.URL
	model.Headers = map[string]string{"Authorization": header}
	model.CompletionsCompat = &goai.OpenAICompletionsCompat{MaxTokensField: "max_tokens"}
	model.SamplingParams = map[string]any{"top_p": 0.5}
	// Avoid an environment key taking precedence over model default headers;
	// this test specifically exercises header-only authentication on real HTTP.
	options := Options{Models: func(goai.Provider, string) *goai.Model { return model }, RequestOptions: func(context.Context, ModelRef) (*goai.StreamOptions, error) {
		return &goai.StreamOptions{Env: goai.ProviderEnv{"OPENAI_API_KEY": ""}}, nil
	}}
	h := openHarness(t, store, options)
	temperature := 0.25
	maxTokens := 77
	r, e := h.Root(bg, AgentChange{Model: ModelRef{model.Provider, model.ID}, Settings: RequestSettings{Temperature: &temperature, MaxTokens: &maxTokens}})
	if e != nil {
		t.Fatal(e)
	}
	sub, e := r.Submit(bg, Input{Content: "large local credential", RequestID: "large-header"})
	if e != nil {
		t.Fatal(e)
	}
	result := waitSubmission(t, sub)
	if result.Submission.Status != "done" || calls.Load() != 1 {
		t.Fatal("local header consumed persistent budget", result)
	}
	state, e := h.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	for _, task := range state.Tasks {
		var cp generationCheckpoint
		if e = fromObject(task.Checkpoint, &cp, h.session.limits); e != nil {
			t.Fatal(e)
		}
		if cp.Model == nil || cp.Model.BaseURL != "" || cp.Model.Headers != nil || cp.Model.APIKey != "" {
			t.Fatal("unsanitized model intent")
		}
		encoded, e := encodeBounded(cp.Model, h.session.limits, 2048)
		if e != nil || len(encoded) >= 2048 {
			t.Fatal("model intent not compact", e)
		}
		if cp.Model.Api != goai.ApiOpenAICompletions || cp.Model.MaxTokens != 256 || cp.Model.SamplingParams["top_p"] != json.Number("0.5") || cp.Model.CompletionsCompat == nil || cp.Model.CompletionsCompat.MaxTokensField != "max_tokens" {
			t.Fatal("lost pinned model behavior")
		}
	}
	// Persisted behavior budgets still apply; process-local auth is not a waiver.
	oversized := *model
	oversized.Name = strings.Repeat("n", DefaultLimits().MaxStringBytes+1)
	if _, e = cloneModel(&oversized, h.session.limits); e == nil {
		t.Fatal("persistent model string cap weakened")
	}
	if e = h.Close(bg); e != nil {
		t.Fatal(e)
	}
	journal, e := os.ReadFile(filepath.Join(dir, "journal.bin"))
	if e != nil {
		t.Fatal(e)
	}
	for _, sentinel := range []string{"LOCAL_MODEL_HEADER_SENTINEL", "LOCAL_MODEL_API_KEY_SENTINEL", server.URL} {
		if strings.Contains(string(journal), sentinel) {
			t.Fatal("local transport/auth leaked")
		}
	}
	store, e = OpenJournal(dir, JournalOptions{})
	if e != nil {
		t.Fatal(e)
	}
	h2 := openHarness(t, store, options)
	r2, e := h2.Conversation(bg, 1)
	if e != nil {
		t.Fatal(e)
	}
	again, e := r2.Submit(bg, Input{RequestID: "large-header"})
	if e != nil {
		t.Fatal(e)
	}
	if waitSubmission(t, again).Submission.Status != "done" || calls.Load() != 1 {
		t.Fatal("large-header terminal replay")
	}
}
