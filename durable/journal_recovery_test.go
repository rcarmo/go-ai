package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestM1cStoredCurrentReplayPolicyAndIdentity(t *testing.T) {
	for _, mode := range []string{"both-safe", "stored-unsafe", "current-unsafe", "changed-id", "changed-version", "changed-schema", "missing"} {
		t.Run(mode, func(t *testing.T) {
			var effects, repairs atomic.Int64
			entered, release := make(chan struct{}), make(chan struct{})
			registry := NewRegistry()
			storedSafe := mode != "stored-unsafe"
			registration := ToolRegistration{Definition: goai.Tool{Name: "compute", Parameters: json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"],"additionalProperties":false}`)}, Implementation: "compute.impl", Version: 1, ReplaySafe: storedSafe, Validator: func(context.Context, JSON) (JSON, error) { repairs.Add(1); return JSON{"n": 3}, nil }, Execute: func(ctx context.Context, args JSON, api *ToolAPI) (ToolResult, error) {
				effects.Add(1)
				close(entered)
				<-release
				<-ctx.Done() // Explicit unfinished cancellation, not a racing nil error.
				return ToolResult{}, ctx.Err()
			}}
			if e := registry.Register(registration); e != nil {
				t.Fatal(e)
			}
			var modelCalls atomic.Int64
			ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, conv *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if modelCalls.Add(1) == 1 {
					ch <- toolAnswer("same-call", "compute", JSON{"n": 2})
				} else {
					if mode == "both-safe" {
						assertOriginalCallAndResult(t, conv, "compute", "same-call", "n", json.Number("2"), "3")
					}
					ch <- terminal("answer")
				}
				close(ch)
				return ch
			})
			options.Registry = registry
			store, dir := newJournal(t)
			h := openHarness(t, store, options)
			sub, e := root(t, h, ref).Submit(bg, Input{Content: "replay", RequestID: "replay"})
			if e != nil {
				t.Fatal(e)
			}
			<-entered
			closeDone := make(chan error, 1)
			go func() { closeDone <- h.Close(bg) }()
			<-h.life.Done()
			close(release)
			if e = <-closeDone; e != nil {
				t.Fatal(e)
			}
			current := registration
			current.ReplaySafe = mode != "current-unsafe"
			current.Execute = func(ctx context.Context, args JSON, api *ToolAPI) (ToolResult, error) {
				effects.Add(1)
				if args["n"] != json.Number("3") {
					t.Error("recovery did not use final stored args", args)
				}
				return ToolResult{Content: "3"}, nil
			}
			current.Validator = func(context.Context, JSON) (JSON, error) { repairs.Add(1); return JSON{"n": 99}, nil }
			switch mode {
			case "changed-id":
				current.Implementation = "other.impl"
			case "changed-version":
				current.Version = 2
			case "changed-schema":
				current.Definition.Parameters = json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer","maximum":100}},"required":["n"],"additionalProperties":false}`)
			}
			if mode == "missing" {
				registry.Remove("compute")
			} else if e = registry.Register(current); e != nil {
				t.Fatal(e)
			}
			store, e = OpenJournal(dir, JournalOptions{})
			if e != nil {
				t.Fatal(e)
			}
			h2 := openHarness(t, store, options)
			snapshot, e := h2.Snapshot(bg)
			if e != nil {
				t.Fatal(e)
			}
			if effects.Load() != 1 {
				t.Fatal("Open effect")
			}
			assertStoredOriginalAndExecutionArgs(t, snapshot, "compute", "n", json.Number("2"), json.Number("3"), h2.session.limits)
			for _, task := range snapshot.Tasks {
				if task.Kind == "pi.tool" && task.Status != "pending" {
					t.Fatal("tool status after reopen", task.Status, task.Checkpoint)
				}
			}
			resumed, e := h2.Submission(bg, sub.ID())
			if e != nil {
				t.Fatal(e)
			}
			result := waitSubmission(t, resumed)
			if result.Submission.Status != "done" {
				t.Fatal(result)
			}
			want := int64(1)
			if mode == "both-safe" {
				want = 2
			}
			if effects.Load() != want || repairs.Load() != 1 {
				t.Fatal("replay safety or repair repeated", effects.Load(), repairs.Load())
			}
			snapshot, e = h2.Snapshot(bg)
			if e != nil {
				t.Fatal(e)
			}
			children := 0
			for _, task := range snapshot.Tasks {
				if task.Kind == "pi.tool" {
					children++
					var cp toolCheckpoint
					_ = fromObject(task.Checkpoint, &cp, h2.session.limits)
					if cp.Result == nil {
						t.Fatal("no terminal result")
					}
					if mode == "stored-unsafe" || mode == "current-unsafe" {
						if cp.ErrorCode != "interrupted" {
							t.Fatal(cp.ErrorCode)
						}
					} else if mode != "both-safe" && cp.ErrorCode != "tool_unavailable" {
						t.Fatal(cp.ErrorCode)
					}
				}
			}
			if children != 1 {
				t.Fatal("providerCallID/newtaskcollision")
			}
			assertStoredOriginalAndExecutionArgs(t, snapshot, "compute", "n", json.Number("2"), json.Number("3"), h2.session.limits)
		})
	}
}

func TestM1cActualToolProcessCrash(t *testing.T) {
	dir, phase := os.Getenv("GOAI_M1C_CRASH_DIR"), os.Getenv("GOAI_M1C_CRASH_PHASE")
	makeRegistry := func(execute func(context.Context, JSON, *ToolAPI) (ToolResult, error)) *Registry {
		r := NewRegistry()
		if e := r.Register(ToolRegistration{Definition: goai.Tool{Name: "counter", Parameters: json.RawMessage(`{"type":"object","additionalProperties":false}`)}, Implementation: "counter.impl", Version: 1, Execute: execute}); e != nil {
			t.Fatal(e)
		}
		return r
	}
	if dir != "" {
		api := goai.Api("m1c-process")
		goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- toolAnswer("repeat", "counter", JSON{})
			close(ch)
			return ch
		}})
		store, e := OpenJournal(dir, JournalOptions{})
		if e != nil {
			os.Exit(91)
		}
		registry := makeRegistry(func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			if phase == "intent" {
				_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
			}
			f, e := os.OpenFile(filepath.Join(dir, "effect.txt"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if e != nil {
				os.Exit(92)
			}
			_, _ = f.WriteString("effect\n")
			_ = f.Sync()
			_ = f.Close()
			if phase == "post-effect" {
				_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
			}
			return ToolResult{Content: "counter"}, nil
		})
		if phase == "landed-outcome" {
			store.file = &crashToolOutcomeFile{journalFile: store.file}
		}
		h, e := Open(bg, store, Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return fakeModel(api) }})
		if e != nil {
			os.Exit(93)
		}
		r, e := h.Root(bg, AgentChange{Model: ModelRef{goai.ProviderOpenAI, "durable-test"}})
		if e != nil {
			os.Exit(94)
		}
		sub, e := r.Submit(bg, Input{Content: "counter", RequestID: "process-tool"})
		if e != nil {
			os.Exit(95)
		}
		settlement, waitErr := sub.Wait(bg)
		state, _ := h.Snapshot(bg)
		fmt.Printf("unexpectedsettlement %+v err%v state%+v\n", settlement, waitErr, state)
		os.Exit(96)
	}
	for _, phase := range []string{"intent", "post-effect", "landed-outcome"} {
		t.Run(phase, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "private")
			cmd := exec.Command(os.Args[0], "-test.run=^TestM1cActualToolProcessCrash$")
			cmd.Env = append(os.Environ(), "GOAI_M1C_CRASH_DIR="+dir, "GOAI_M1C_CRASH_PHASE="+phase)
			output, processErr := cmd.CombinedOutput()
			exit, ok := processErr.(*exec.ExitError)
			if !ok {
				t.Fatalf("child not killed: %v %s", processErr, output)
			}
			status, ok := exit.Sys().(syscall.WaitStatus)
			if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
				t.Fatalf("child did not reach crash point: %v %s", processErr, output)
			}
			var effects atomic.Int64
			registry := makeRegistry(func(context.Context, JSON, *ToolAPI) (ToolResult, error) { effects.Add(1); return ToolResult{}, nil })
			api := goai.Api("m1c-process")
			goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				ch <- terminal("answer")
				close(ch)
				return ch
			}})
			defer goai.UnregisterApi(api)
			store, e := OpenJournal(dir, JournalOptions{})
			if e != nil {
				t.Fatal(e)
			}
			h := openHarness(t, store, Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return fakeModel(api) }})
			r, e := h.Conversation(bg, 1)
			if e != nil {
				t.Fatal(e)
			}
			sub, e := r.Submit(bg, Input{RequestID: "process-tool"})
			if e != nil {
				t.Fatal(e)
			}
			if waitSubmission(t, sub).Submission.Status != "done" || effects.Load() != 0 {
				t.Fatal("unsafe effect rerun/status", effects.Load())
			}
			state, e := h.Snapshot(bg)
			if e != nil {
				t.Fatal(e)
			}
			results := 0
			for _, entry := range state.Entries {
				if entry.Value["role"] == string(goai.RoleToolResult) {
					results++
				}
			}
			if results != 1 {
				t.Fatal("duplicate/missing tool outcome")
			}
			data, e := os.ReadFile(filepath.Join(dir, "effect.txt"))
			if phase == "intent" {
				if !errors.Is(e, os.ErrNotExist) {
					t.Fatal("unexpectedeffect")
				}
			} else if e != nil || string(data) != "effect\n" {
				t.Fatal("effect counter", e)
			}
		})
	}
}

type crashToolOutcomeFile struct{ journalFile }

func (f *crashToolOutcomeFile) Write(p []byte) (int, error) {
	n, e := f.journalFile.Write(p)
	if e == nil && len(p) > 64 && p[10] == 2 && bytes.Contains(p, []byte(`"role":"toolResult"`)) {
		_ = f.journalFile.Sync()
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
	}
	return n, e
}

type uncertainToolOutcomeFile struct {
	journalFile
	uncertain bool
}

func (f *uncertainToolOutcomeFile) Write(p []byte) (int, error) {
	if len(p) > 64 && p[10] == 2 && bytes.Contains(p, []byte(`"role":"toolResult"`)) {
		f.uncertain = true
	}
	return f.journalFile.Write(p)
}
func (f *uncertainToolOutcomeFile) Sync() error {
	if f.uncertain {
		return errors.New("uncertain tool outcome")
	}
	return f.journalFile.Sync()
}
func TestM1cUncertainToolOutcomeReopenAdoptsOnce(t *testing.T) {
	var effects, requests atomic.Int64
	entered, release := make(chan struct{}), make(chan struct{})
	registry := NewRegistry()
	var document ID
	if e := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "effect", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "effect.impl", Version: 1, ReplaySafe: true, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
		effects.Add(1)
		close(entered)
		<-release
		return ToolResult{Content: "effect", Usage: &goai.Usage{TotalTokens: 2}, Commit: func(tx *Tx) error {
			h, e := tx.Document(document)
			if e != nil {
				return e
			}
			return h.Set(JSON{"winner": true})
		}}, nil
	}}); e != nil {
		t.Fatal(e)
	}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("call", "effect", JSON{})
		} else {
			ch <- terminal("answer")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	store, dir := newJournal(t)
	h := openHarness(t, store, options)
	r := root(t, h, ref)
	document = createAppDocument(t, r)
	sub, e := r.Submit(bg, Input{Content: "effect", RequestID: "uncertain-tool"})
	if e != nil {
		t.Fatal(e)
	}
	<-entered
	store.file = &uncertainToolOutcomeFile{journalFile: store.file}
	close(release)
	ctx, cancel := context.WithTimeout(bg, time.Second)
	defer cancel()
	if _, e = sub.Wait(ctx); !errors.Is(e, ErrPoisoned) {
		t.Fatal("uncertainoutcome notpoisoned", e)
	}
	_ = h.Close(bg)
	store, e = OpenJournal(dir, JournalOptions{})
	if e != nil {
		t.Fatal(e)
	}
	h2 := openHarness(t, store, options)
	state, e := h2.Snapshot(bg)
	if e != nil || state.Documents[document].Value["winner"] != true {
		t.Fatal("appdoc notatomicwinner")
	}
	assertToolUsageReceipts(t, state, "effect", &goai.Usage{TotalTokens: 2}, h2.session.limits)
	resumed, e := h2.Submission(bg, sub.ID())
	if e != nil {
		t.Fatal(e)
	}
	if waitSubmission(t, resumed).Submission.Status != "done" || effects.Load() != 1 {
		t.Fatal("uncertaincommittedeffectreplayed")
	}
	state, e = h2.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	assertToolUsageReceipts(t, state, "effect", &goai.Usage{TotalTokens: 2}, h2.session.limits)
}
