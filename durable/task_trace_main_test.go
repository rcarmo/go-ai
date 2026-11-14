package durable

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type mainTaskTraceSpan struct {
	name    string
	context goai.TelemetryCarrier
	parent  goai.TelemetryCarrier
	attrs   map[string]any
	links   []goai.TelemetryCarrier
	ended   bool
	status  string
}
type mainTaskTraceBackend struct {
	t     *testing.T
	scope string
	mu    sync.Mutex
	spans []*mainTaskTraceSpan
	root  *goai.TelemetrySpan
	h     *Harness
}

func mainTaskTraceCopy(c goai.TelemetryCarrier) goai.TelemetryCarrier {
	if c == nil {
		return nil
	}
	out := make(goai.TelemetryCarrier, len(c))
	for k, v := range c {
		out[k] = v
	}
	return out
}
func newMainTaskTraceBackend(t *testing.T) *mainTaskTraceBackend {
	b := &mainTaskTraceBackend{t: t, scope: t.Name()}
	root := &goai.TelemetrySpan{}
	root.StartSpan = func(n string, a map[string]any) *goai.TelemetrySpan { return b.create(n, nil, a, nil) }
	root.StartRootSpan = func(n string, a map[string]any, links []*goai.TelemetrySpan) *goai.TelemetrySpan {
		return b.create(n, nil, a, links)
	}
	// Optional reflected capability keeps the unchanged API baseline compilable.
	// It only supplies a backend; no task record or phase result is synthesised.
	field := reflect.ValueOf(root).Elem().FieldByName("ExtractContext")
	extract := func(c goai.TelemetryCarrier) *goai.TelemetrySpan {
		if b.h != nil {
			if _, err := b.h.Snapshot(bg); err != nil {
				t.Error("extract on Session line", err)
			}
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		for _, s := range b.spans {
			if reflect.DeepEqual(s.context, c) {
				return b.handle(s)
			}
		}
		return root
	}
	value := reflect.ValueOf(extract)
	if field.IsValid() && field.CanSet() && value.Type().AssignableTo(field.Type()) {
		field.Set(value)
	}
	b.root = root
	return b
}
func (b *mainTaskTraceBackend) create(n string, parent goai.TelemetryCarrier, a map[string]any, links []*goai.TelemetrySpan) *goai.TelemetrySpan {
	if b.h != nil {
		if _, err := b.h.Snapshot(bg); err != nil {
			b.t.Error("start span on Session line", err)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	s := &mainTaskTraceSpan{name: n, context: goai.TelemetryCarrier{"scope": b.scope, "span": strconv.Itoa(len(b.spans) + 1)}, parent: mainTaskTraceCopy(parent), attrs: map[string]any{}}
	for k, v := range a {
		s.attrs[k] = v
	}
	for _, l := range links {
		s.links = append(s.links, mainTaskTraceCopy(l.Context))
	}
	b.spans = append(b.spans, s)
	return b.handle(s)
}
func (b *mainTaskTraceBackend) handle(s *mainTaskTraceSpan) *goai.TelemetrySpan {
	return &goai.TelemetrySpan{Context: mainTaskTraceCopy(s.context), Value: s,
		StartSpan: func(n string, a map[string]any) *goai.TelemetrySpan { return b.create(n, s.context, a, nil) },
		StartRootSpan: func(n string, a map[string]any, links []*goai.TelemetrySpan) *goai.TelemetrySpan {
			return b.create(n, nil, a, links)
		},
		SetAttributes: func(a map[string]any) {
			b.mu.Lock()
			defer b.mu.Unlock()
			for k, v := range a {
				s.attrs[k] = v
			}
		},
		AddLink: func(other *goai.TelemetrySpan) {
			if b.h != nil {
				if _, err := b.h.Snapshot(bg); err != nil {
					b.t.Error("link on Session line", err)
				}
			}
			b.mu.Lock()
			defer b.mu.Unlock()
			if s.ended {
				b.t.Error("link added to ended span", s.name)
			}
			s.links = append(s.links, mainTaskTraceCopy(other.Context))
		},
		SetStatus: func(status, _, _ string) { b.mu.Lock(); s.status = status; b.mu.Unlock() },
		End:       func() { b.mu.Lock(); s.ended = true; b.mu.Unlock() },
	}
}
func mainTaskTraceWire(t *testing.T, v any) map[string]json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func mainTaskTraceFields(t *testing.T, v any, want goai.TelemetryCarrier, links []goai.TelemetryCarrier) {
	t.Helper()
	wire := mainTaskTraceWire(t, v)
	var got goai.TelemetryCarrier
	if raw := wire["trace"]; raw != nil {
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Error("task trace", got, "want", want)
	}
	var gotLinks []goai.TelemetryCarrier
	if raw := wire["links"]; raw != nil {
		if err := json.Unmarshal(raw, &gotLinks); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(gotLinks, links) {
		t.Error("task links", gotLinks, "want", links)
	}
}
func mainTaskTraceOptions(t *testing.T, data string) TaskOptions {
	t.Helper()
	o := TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}
	if err := json.Unmarshal([]byte(data), &o); err != nil {
		t.Fatal(err)
	}
	return o
}

// Complete selected source scenario f9a26365 harness-telemetry.test.ts:303-345:
// one shared queued run, persisted first-parent/other-link, both phase links.
// Native legacy placeholder tasks are selected by actual submission ID instead
// of the source facade's second scan row; no task count assertion is removed.
func TestMainTaskTraceQueuedFirstInvocation(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		telemetry := newMainTaskTraceBackend(t)
		entered, release := make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, release)
		var calls atomic.Int64
		ref, o := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			go func() {
				defer close(ch)
				if calls.Add(1) == 1 {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return
					}
				}
				ch <- terminal("shared")
			}()
			return ch
		})
		o.Telemetry = telemetry.root
		o.Settings = &HarnessSettings{Stream: RequestSettings{FollowUpMode: "all"}}
		h := openHarness(t, b.store, o)
		telemetry.h = h
		c := root(t, h, ref)
		first, err := c.Submit(bg, Input{Content: "first"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		second, err := c.Submit(bg, Input{Content: "second"})
		if err != nil {
			t.Fatal(err)
		}
		third, err := c.Submit(bg, Input{Content: "third"})
		if err != nil {
			t.Fatal(err)
		}
		secondStatus, err := second.Status(bg)
		if err != nil {
			t.Fatal(err)
		}
		thirdStatus, err := third.Status(bg)
		if err != nil {
			t.Fatal(err)
		}
		secondTrace := goai.TelemetryCarrier(mainTraceRecordCarrier(t, secondStatus))
		thirdTrace := goai.TelemetryCarrier(mainTraceRecordCarrier(t, thirdStatus))
		releaseTaskGate(release)
		waitSubmission(t, first)
		waitSubmission(t, second)
		waitSubmission(t, third)
		if err := h.WaitForIdle(bg); err != nil {
			t.Fatal(err)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var shared Task
		for _, task := range state.Tasks {
			if task.Kind == "pi.generation" && submissionEntryID(task.Checkpoint["submission"]) == second.ID() {
				shared = task
			}
		}
		if shared.ID == 0 {
			t.Fatal("shared task missing")
		}
		mainTaskTraceFields(t, shared, secondTrace, []goai.TelemetryCarrier{thirdTrace})
		canonical, err := CanonicalTask(shared, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		mainTaskTraceFields(t, canonical, secondTrace, []goai.TelemetryCarrier{thirdTrace})
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		telemetry.mu.Lock()
		defer telemetry.mu.Unlock()
		var phases []string
		for _, span := range telemetry.spans {
			if !span.ended {
				t.Error("unsettled span", span.name)
			}
			if span.name == "pi.durable.phase" && span.attrs["task.id"] == uint64(shared.ID) {
				phase, _ := span.attrs["phase"].(string)
				phases = append(phases, phase)
				if !reflect.DeepEqual(span.parent, secondTrace) {
					t.Error("shared phase parent", span.parent, secondTrace)
				}
				if !reflect.DeepEqual(span.links, []goai.TelemetryCarrier{thirdTrace}) {
					t.Error("shared phase links", span.links, thirdTrace)
				}
			}
		}
		if !reflect.DeepEqual(phases, []string{"prepare", "request"}) || calls.Load() != 2 {
			t.Error("source phase sequence/requests", phases, calls.Load())
		}
	})
}

// Source177-214: omitted/undefined inherit the creator; explicit root stores no
// carrier and creates a phase under the harness root. Go nil adapts undefined.
func TestMainTaskTraceInheritedAndExplicitRoot(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		telemetry := newMainTaskTraceBackend(t)
		def := taskDefinition(t, "task.trace-inheritance", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h := taskTestHarnessOptions(t, b.store, Options{Telemetry: telemetry.root}, def)
		telemetry.h = h
		creator := telemetry.create("creator", nil, nil, nil)
		carrier := &invocationTelemetry{}
		carrier.span.Store(creator)
		ctx := context.WithValue(bg, telemetryContextKey{}, carrier)
		var ids []ID
		_, err := h.commitTasks(ctx, 1, func(tx *Tx) error {
			for _, data := range []string{`{}`, `{}`, `{"trace":"root"}`} {
				id, err := tx.CreateTask(def, nil, mainTaskTraceOptions(t, data))
				if err != nil {
					return err
				}
				ids = append(ids, id)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		creator.End()
		for i, id := range ids {
			got := waitPublicTask(t, h, id)
			want := creator.Context
			if i == 2 {
				want = nil
			}
			mainTaskTraceFields(t, got, want, nil)
		}
		if err := h.WaitForIdle(bg); err != nil {
			t.Fatal(err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		telemetry.mu.Lock()
		defer telemetry.mu.Unlock()
		var parents []goai.TelemetryCarrier
		for _, span := range telemetry.spans {
			if !span.ended {
				t.Error("unsettled", span.name)
			}
			if span.name == "pi.durable.phase" {
				parents = append(parents, span.parent)
			}
		}
		want := []goai.TelemetryCarrier{creator.Context, creator.Context, nil}
		if !reflect.DeepEqual(parents, want) {
			t.Error("creator/root phase parents", parents, want)
		}
	})
}

// Source-derived transaction.ts449-471: only the first ID supplies the parent;
// later available carriers supply links, regardless of explicit trace options.
func TestMainTaskTraceSubmissionSelectionNoCarrier(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		telemetry := newMainTaskTraceBackend(t)
		def := taskDefinition(t, "task.trace-selection", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h := taskTestHarnessOptions(t, b.store, Options{Telemetry: telemetry.root}, def)
		telemetry.h = h
		creator := telemetry.create("creator", nil, nil, nil)
		linked := telemetry.create("linked", nil, nil, nil)
		linked.End()
		ctxCarrier := &invocationTelemetry{}
		ctxCarrier.span.Store(creator)
		ctx := context.WithValue(bg, telemetryContextKey{}, ctxCarrier)
		var id ID
		_, err := h.commitTasks(ctx, 1, func(tx *Tx) error {
			createdFirst, err := tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "queued"})
			first := createdFirst.ID
			if err != nil {
				return err
			}
			createdNext, err := tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "queued", Trace: &linked.Context})
			next := createdNext.ID
			if err != nil {
				return err
			}
			raw, err := json.Marshal(map[string]any{"submissionIds": []ID{first, next}, "trace": creator.Context, "links": []goai.TelemetryCarrier{creator.Context}})
			if err != nil {
				return err
			}
			id, err = tx.CreateTask(def, nil, mainTaskTraceOptions(t, string(raw)))
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		creator.End()
		record := waitPublicTask(t, h, id)
		mainTaskTraceFields(t, record, nil, []goai.TelemetryCarrier{linked.Context})
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		telemetry.mu.Lock()
		defer telemetry.mu.Unlock()
		var found bool
		for _, s := range telemetry.spans {
			if s.name == "pi.durable.phase" {
				found = true
				if s.parent != nil || !reflect.DeepEqual(s.links, []goai.TelemetryCarrier{linked.Context}) {
					t.Error("carrierless first must use root and retain later links", s.parent, s.links)
				}
			}
		}
		if !found {
			t.Fatal("phase missing")
		}
	})
}
