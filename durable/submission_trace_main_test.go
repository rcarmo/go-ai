package durable

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// The backend supplies data-only propagation state independently of Value.
// The baseline used an optional reflected field setter to remain compilable
// against the old API; production tests now use the ordinary typed field.
func mainTraceSpanContext(span *goai.TelemetrySpan, carrier map[string]string) {
	span.Context = goai.TelemetryCarrier(carrier)
}

func mainTraceRecordCarrier(t *testing.T, value any) map[string]string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	raw := object["trace"]
	if raw == nil {
		raw = object["Trace"]
	}
	if raw == nil {
		return nil
	}
	var carrier map[string]string
	if err := json.Unmarshal(raw, &carrier); err != nil {
		t.Fatal("trace must be a string carrier", err)
	}
	return carrier
}

func mainTraceMutate(value any) {
	var trace *goai.TelemetryCarrier
	switch record := value.(type) {
	case SubmissionRecord:
		trace = record.Trace
	case Submission:
		trace = record.Trace
	}
	if trace != nil && *trace != nil {
		(*trace)["caller-mutation"] = "must-not-escape"
	}
}

// Source-derived from f9a26365 types.ts SubmissionFields.trace and
// session/transaction.ts createSubmission/settle/place JSON-copy behaviour.
// Source356 additionally requires this same field on the public Submit path.
func TestMainSubmissionTraceDTOOwnershipAndReopen(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h := taskTestHarness(t, b.store)
		want := map[string]string{"traceparent": "opaque-parent", "tracestate": "vendor=value"}
		var record SubmissionRecord
		if err := json.Unmarshal([]byte(`{"id":2,"conversationId":1,"requestId":"trace-dto","type":"continue","status":"placed","trace":{"traceparent":"opaque-parent","tracestate":"vendor=value"}}`), &record); err != nil {
			t.Fatal(err)
		}
		// CreateSubmission takes a creation draft; storage assigns its ID.
		record.ID = 0
		var id ID
		_, err := h.Commit(bg, func(tx *Tx) error {
			var err error
			id, err = tx.CreateSubmission(record)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		mainTraceMutate(record)
		_, err = h.Commit(bg, func(tx *Tx) error {
			got, found, err := tx.Submission(id)
			if err != nil || !found {
				t.Fatal(got, found, err)
			}
			if !reflect.DeepEqual(mainTraceRecordCarrier(t, got), want) {
				t.Error("transaction trace lost", got)
			}
			mainTraceMutate(got)
			again, found, err := tx.Submission(id)
			if err != nil || !found || !reflect.DeepEqual(mainTraceRecordCarrier(t, again), want) {
				t.Fatal("transaction carrier alias", again, found, err)
			}
			return tx.SettleSubmission(id, SubmissionRecord{Status: "unanswered", Reason: "aborted"})
		})
		if err != nil {
			t.Fatal(err)
		}
		handle, err := h.Submission(bg, id)
		if err != nil || handle == nil {
			t.Fatal(err)
		}
		got, err := handle.Status(bg)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(mainTraceRecordCarrier(t, got), want) {
			t.Error("status trace lost or caller-owned", got)
		}
		mainTraceMutate(got)
		again, err := handle.Status(bg)
		if err != nil || !reflect.DeepEqual(mainTraceRecordCarrier(t, again), want) {
			t.Fatal("status carrier alias", again, err)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(mainTraceRecordCarrier(t, state.Submissions[id]), want) {
			t.Error("snapshot trace lost or caller-owned", state.Submissions[id])
		}
		mainTraceMutate(state.Submissions[id])
		fresh, err := h.Snapshot(bg)
		if err != nil || !reflect.DeepEqual(mainTraceRecordCarrier(t, fresh.Submissions[id]), want) {
			t.Fatal("snapshot carrier alias", fresh.Submissions[id], err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		reopened := b.reopen()
		t.Cleanup(func() { _ = reopened.Close(bg) })
		stored, err := reopened.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(mainTraceRecordCarrier(t, stored.Submissions[id]), want) {
			t.Error("stored/reopened trace differs", stored.Submissions[id])
		}
	})
}

// Source TelemetryCarrier is an optional string object: empty and absent
// differ. Both remain data-only through the real transaction projection.
func TestMainSubmissionTraceEmptyAndAbsent(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		want        map[string]string
	}{
		{"absent", "", nil}, {"empty", `,"trace":{}`, map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := taskTestHarness(t, mustMemory(t))
			var record SubmissionRecord
			if err := json.Unmarshal([]byte(`{"id":2,"conversationId":1,"type":"continue","status":"placed"`+tc.field+`}`), &record); err != nil {
				t.Fatal(err)
			}
			// Convert the public record into an ID-less creation draft.
			record.ID = 0
			_, err := h.Commit(bg, func(tx *Tx) error {
				id, err := tx.CreateSubmission(record)
				if err != nil {
					return err
				}
				got, _, err := tx.Submission(id)
				if err == nil && !reflect.DeepEqual(mainTraceRecordCarrier(t, got), tc.want) {
					t.Error("optional carrier presence lost", got)
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Source-derived DTO type fences: arbitrary backend Value and structured/non-
// string carrier entries are never silently converted into propagation data.
func TestMainSubmissionTraceRejectsNonStringCarrier(t *testing.T) {
	for _, carrier := range []string{`"opaque"`, `[]`, `{"span":1}`, `{"span":true}`, `{"span":null}`, `{"span":{"nested":"private"}}`} {
		t.Run(carrier, func(t *testing.T) {
			var record SubmissionRecord
			if err := json.Unmarshal([]byte(`{"id":2,"conversationId":1,"type":"continue","status":"placed","trace":`+carrier+`}`), &record); err == nil {
				t.Error("invalid trace carrier accepted", carrier)
			}
		})
	}
}

// Extend only this new carrier slice with the real required pure-Go backend;
// the shared legacy helper's completed coverage is not changed or repeated.

func TestMainSubmissionTracePublicCaptureDedupAndReopen(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- terminal("answer")
			close(ch)
			return ch
		})
		var sequence atomic.Int64
		var latest atomic.Pointer[goai.TelemetryCarrier]
		makeSpan := func(name string) *goai.TelemetrySpan {
			if name != "pi.durable.submit" {
				return nil
			}
			carrier := goai.TelemetryCarrier{"scope": t.Name(), "span": strconv.FormatInt(sequence.Add(1), 10)}
			latest.Store(&carrier)
			return &goai.TelemetrySpan{Context: carrier, Value: make(chan int)}
		}
		options.Telemetry = &goai.TelemetrySpan{
			StartRootSpan: func(name string, _ map[string]any, _ []*goai.TelemetrySpan) *goai.TelemetrySpan {
				return makeSpan(name)
			},
			StartSpan: func(name string, _ map[string]any) *goai.TelemetrySpan { return makeSpan(name) },
		}
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		first, err := c.Submit(bg, Input{Content: "hi", RequestID: "trace-input"})
		if err != nil {
			t.Fatal(err)
		}
		wantInput := map[string]string{"scope": t.Name(), "span": "1"}
		if got := waitSubmission(t, first); !reflect.DeepEqual(mainTraceRecordCarrier(t, got.Record), wantInput) {
			t.Fatal("input trace", got.Record)
		}
		if err := h.WaitForIdle(bg); err != nil {
			t.Fatal(err)
		}
		// The completed backend may dispose of its map; stored carriers must
		// already be owned and must not be replaced by a later dedup span.
		(*latest.Load())["backend-private"] = "must-not-persist"
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		dedup, err := c.Submit(bg, Input{Content: "ignored", RequestID: "trace-input"})
		if err != nil || dedup.ID() != first.ID() {
			t.Fatal(dedup, err)
		}
		after, err := h.Snapshot(bg)
		if err != nil || after.Seq != before.Seq {
			t.Fatal("dedup wrote a trace", before.Seq, after.Seq, err)
		}
		status, err := dedup.Status(bg)
		if err != nil || !reflect.DeepEqual(mainTraceRecordCarrier(t, status), wantInput) {
			t.Fatal("dedup replaced original trace", status, err)
		}
		queried, found, err := b.store.Request(bg, c.ID(), "trace-input")
		if err != nil || !found || !reflect.DeepEqual(mainTraceRecordCarrier(t, queried), wantInput) {
			t.Fatal("request projection", queried, found, err)
		}
		mainTraceMutate(queried)
		rows, err := b.store.Submissions(bg, Query{Conversation: c.ID(), Limit: 20})
		if err != nil || len(rows) != 1 || !reflect.DeepEqual(mainTraceRecordCarrier(t, rows[0]), wantInput) {
			t.Fatal("submission query ownership", rows, err)
		}
		mainTraceMutate(rows[0])
		write, err := c.Submit(bg, Input{Type: "write", Entry: &Entry{Kind: "note", Value: JSON{}, HasData: true, Data: JSON{"text": "memo"}}, RequestID: "trace-write"})
		if err != nil {
			t.Fatal(err)
		}
		wantWrite := map[string]string{"scope": t.Name(), "span": "3"}
		if got := waitSubmission(t, write); got.Record.Status != "done" || !reflect.DeepEqual(mainTraceRecordCarrier(t, got.Record), wantWrite) {
			t.Fatal("write trace", got.Record)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		options.Telemetry = nil
		h2 := openHarness(t, b.reopen(), options)
		for id, want := range map[ID]map[string]string{first.ID(): wantInput, write.ID(): wantWrite} {
			handle, err := h2.Submission(bg, id)
			if err != nil || handle == nil {
				t.Fatal(id, err)
			}
			got, err := handle.Status(bg)
			if err != nil || !reflect.DeepEqual(mainTraceRecordCarrier(t, got), want) {
				t.Fatal("reopened projection depends on live backend", id, got, err)
			}
		}
	})
}

func TestMainSubmissionTraceRollback(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h := taskTestHarness(t, b.store)
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		carrier := goai.TelemetryCarrier{"span": "rolled-back"}
		cause := errors.New("caller rollback")
		_, err = h.Commit(bg, func(tx *Tx) error {
			if _, err := tx.CreateSubmission(SubmissionRecord{Conversation: 1, Type: "continue", Status: "placed", RequestID: "trace-rollback", Trace: &carrier}); err != nil {
				return err
			}
			return cause
		})
		if !errors.Is(err, cause) {
			t.Fatal("rollback cause", err)
		}
		after, err := h.Snapshot(bg)
		if err != nil || after.Seq != before.Seq || len(after.Submissions) != len(before.Submissions) {
			t.Fatal("rolled-back carrier escaped", after, err)
		}
		if _, found, err := b.store.Request(bg, 1, "trace-rollback"); err != nil || found {
			t.Fatal("rolled-back request escaped", found, err)
		}
	})
}

func TestMainSubmissionTraceCustomQueuedPhaseKept(t *testing.T) {
	def, err := DefineTask(TaskDefinitionOptions{Kind: "task.custom-queued", Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "queued"}, nil }, Abort: taskAbort,
		Phases: map[string]TaskPhase{"queued": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var observed atomic.Bool
	h := taskTestHarnessOptions(t, mustMemory(t), Options{Telemetry: &goai.TelemetrySpan{StartSpan: func(name string, attrs map[string]any) *goai.TelemetrySpan {
		if name == "pi.durable.phase" && attrs["task.name"] == "task.custom-queued" {
			if attrs["phase"] != "queued" {
				t.Error("custom phase renamed", attrs)
			}
			observed.Store(true)
		}
		return &goai.TelemetrySpan{}
	}}}, def)
	id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	if got := waitPublicTask(t, h, id); got.State.Outcome == nil || got.State.Outcome.Status != "completed" {
		t.Fatal(got)
	}
	if err := h.WaitForIdle(bg); err != nil {
		t.Fatal(err)
	}
	if !observed.Load() {
		t.Fatal("custom queued phase was not observed")
	}
}

func TestMainSubmissionTracePublicEmptyAndAbsent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		carrier goai.TelemetryCarrier
	}{
		{"absent", nil}, {"empty", goai.TelemetryCarrier{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				ch <- terminal("answer")
				close(ch)
				return ch
			})
			options.Telemetry = &goai.TelemetrySpan{StartRootSpan: func(string, map[string]any, []*goai.TelemetrySpan) *goai.TelemetrySpan {
				return &goai.TelemetrySpan{Context: tc.carrier, Value: map[string]string{"span": "PRIVATE_NOT_A_CARRIER"}}
			}}
			h := openHarness(t, mustMemory(t), options)
			c := root(t, h, ref)
			handle, err := c.Submit(bg, Input{Content: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			got := waitSubmission(t, handle)
			if !reflect.DeepEqual(mainTraceRecordCarrier(t, got.Record), map[string]string(tc.carrier)) {
				t.Fatal("public carrier presence or Value authority changed", got.Record)
			}
		})
	}
}
