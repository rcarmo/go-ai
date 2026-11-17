package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source-derived oracle, official v1.5.0 f9a26365 generation.ts:1120-1132:
// an actual postTools reset ends the old run unanswered/reset, without detail,
// and starts its immediate queued successor with keepFailure. startRun at
// 1162-1172 clears that failure when the answered successor starts more work.
// Live taskId and canonical task results identify actual generations. All IDs
// below come from placed submissions/live tasks.
func TestMainRunFailurePostToolsResetRetainsOnlyImmediateSuccessor(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		toolEntered, toolRelease := make(chan struct{}), make(chan struct{})
		entered := []chan struct{}{make(chan struct{}), make(chan struct{})}
		release := []chan struct{}{make(chan struct{}), make(chan struct{})}
		registry := NewRegistry()
		reg := wrapRegistration("hold")
		reg.Execute = func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
			close(toolEntered)
			select {
			case <-toolRelease:
				return ToolResult{Content: "old tool"}, nil
			case <-ctx.Done():
				return ToolResult{}, ctx.Err()
			}
		}
		if err := registry.Register(reg); err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			n := calls.Add(1)
			if n == 1 {
				ch <- toolAnswer("old-call", "hold", JSON{})
				close(ch)
				return ch
			}
			go func() {
				defer close(ch)
				if n != 2 && n != 3 {
					t.Errorf("unexpected provider call %d", n)
					ch <- terminal("unexpected answer")
					return
				}
				var text []string
				for _, message := range input.Messages {
					for _, block := range message.Content {
						text = append(text, block.Text)
					}
				}
				want := "handoff,B"
				answer := "B answer"
				if n == 3 {
					want, answer = "handoff,B,B answer,C", "C answer"
				}
				if got := strings.Join(text, ","); got != want {
					t.Errorf("provider %d reset context = %q; want %q", n, got, want)
				}
				close(entered[n-2])
				select {
				case <-release[n-2]:
					ch <- terminal(answer)
				case <-ctx.Done():
				}
			}()
			return ch
		})
		options.Registry = registry
		// A reset, unlike a model failure, starts its successor independently
		// of afterFailedRun. Do not mask that distinction with "continue".
		options.Settings = &HarnessSettings{AfterFailedRun: "keep", FollowUpMode: "one-at-a-time"}
		options.OnReport = func(err error) { t.Error("scheduler", err) }
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, toolRelease, release[0], release[1])
		c := root(t, h, ref)

		active := func(sub *SubmissionHandle, name string) (JSON, ID, ID) {
			t.Helper()
			status, err := sub.Status(bg)
			if err != nil || status.Status != "placed" || status.Entry == 0 {
				t.Fatalf("%s must be placed at its actual gate: %+v, %v", name, status, err)
			}
			state, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			var live JSON
			for _, doc := range state.Documents {
				if !doc.Retired && doc.Kind == "pi.live" && doc.Owner == c.ID() {
					if live != nil {
						t.Fatal("multiple live documents")
					}
					live = doc.Value
				}
			}
			if live == nil {
				t.Fatal("missing live document")
			}
			run := generationObject(live["run"])
			task, ok, err := h.Task(bg, submissionEntryID(run["taskId"]))
			if err != nil || !ok || task.Kind != "pi.generation" || task.Version != 2 || task.Owner != 0 || task.Conversation != c.ID() || task.State.Status == "terminal" {
				t.Fatalf("%s has no actual active generation: %v", name, live)
			}
			cp := taskCheckpointMap(task.State.Checkpoint)
			phase := "request"
			if name == "A tools" {
				phase = "tools"
			}
			if cp["phase"] != phase || task.Input == nil || !task.Input.Present || !equalJSONValue(task.Input.Value, JSON{}) {
				t.Fatal("canonical request/tool round", task, cp)
			}
			want, err := dtoObject(JSON{"inputs": []any{sub.ID()}}, h.session.limits)
			if err != nil {
				t.Fatal(err)
			}
			first := submissionEntryID(run["first"])
			boundaryOK := first == status.Entry
			if name == "B" {
				reset, exists := state.Entries[first]
				input := state.Entries[status.Entry]
				boundaryOK = exists && reset.Conversation == c.ID() && reset.Kind == "pi.reset" && reset.Seq == input.Seq && reset.Position < input.Position
			}
			if !boundaryOK || !equalJSONValue(run["inputs"], want["inputs"]) {
				t.Fatalf("%s must own its actual input/first: run=%v checkpoint=%+v status=%+v", name, run, cp, status)
			}
			entry, ok := state.Entries[status.Entry]
			if !ok || entry.Conversation != c.ID() || legacyEntryValue(entry)["role"] != "user" {
				t.Fatalf("%s first is not its persisted user entry: %+v", name, entry)
			}
			if held, _ := live["failedRun"].(bool); held {
				t.Fatalf("%s must not hold the queue: %v", name, live)
			}
			return live, task.ID, status.Entry
		}
		queued := func(sub *SubmissionHandle) {
			t.Helper()
			status, err := sub.Status(bg)
			if err != nil || status.Status != "queued" || status.Entry != 0 {
				t.Fatalf("followup must remain queued/unplaced: %+v, %v", status, err)
			}
		}
		a, err := c.Submit(bg, Input{Content: "A"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, toolEntered)
		_, aTask, aEntry := active(a, "A tools")
		bSub, err := c.Submit(bg, Input{Content: "B"})
		if err != nil {
			t.Fatal(err)
		}
		cSub, err := c.Submit(bg, Input{Content: "C"})
		if err != nil {
			t.Fatal(err)
		}
		queued(bSub)
		queued(cSub)
		if err := c.Reset(bg, "handoff"); err != nil {
			t.Fatal(err)
		}
		queued(bSub)
		queued(cSub)
		releaseTaskGate(toolRelease)
		old := waitSubmission(t, a)
		if old.Record.Status != "unanswered" || old.Record.Reason != "reset" || old.Record.Answer != 0 || old.Record.Entry != aEntry || old.Task.ID != aTask || old.Record.Detail != nil && old.Record.Detail.Present {
			t.Fatalf("post-tools reset must settle A unanswered/reset without detail: %+v, task=%d", old.Record, old.Task.ID)
		}
		record, found, err := h.Task(bg, aTask)
		if err != nil || !found || record.State.Checkpoint != nil || record.State.Outcome == nil || record.State.Outcome.Status != "completed" || record.State.Outcome.Result == nil || !record.State.Outcome.Result.Present {
			t.Fatal("reset must complete the actual tool-calling generation", record, err)
		}
		assistant := submissionEntryID(generationObject(record.State.Outcome.Result.Value)["entryId"])
		entry, found, err := h.Entry(bg, assistant)
		if err != nil || !found || entry.Kind != "pi.assistant" || entry.ByTask != aTask || len(entry.Model) != 1 || len(entry.Model[0].Content) != 1 || entry.Model[0].Content[0].ID != "old-call" {
			t.Fatal("reset generation result must reference its actual assistant call", entry, err)
		}
		awaitTaskSignal(t, entered[0])
		live, bTask, bEntry := active(bSub, "B")
		if bTask == aTask || bEntry == aEntry {
			t.Fatal("reset successor reused A task/first", aTask, bTask, aEntry, bEntry)
		}
		wantFailure, err := dtoObject(JSON{"taskId": aTask, "inputs": []any{a.ID()}, "first": aEntry, "reason": "reset"}, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		if !equalJSONValue(live["runFailure"], wantFailure) {
			t.Errorf("B in progress must retain exact A reset failure without detail: got %v; want %v", live["runFailure"], wantFailure)
		}
		if detail, exists := generationObject(live["runFailure"])["detail"]; exists {
			t.Errorf("reset failure detail must be ABSENT, not null/empty: %v", detail)
		}
		queued(cSub)
		releaseTaskGate(release[0])
		if answered := waitSubmission(t, bSub); answered.Record.Status != "done" || answered.Record.Answer == 0 || answered.Record.Entry != bEntry {
			t.Fatalf("B must answer before C starts: %+v", answered.Record)
		}
		awaitTaskSignal(t, entered[1])
		live, cTask, cEntry := active(cSub, "C")
		if cTask == aTask || cTask == bTask || cEntry == aEntry || cEntry == bEntry {
			t.Fatal("second successor must own a distinct task/first", aTask, bTask, cTask, aEntry, bEntry, cEntry)
		}
		if failure, exists := live["runFailure"]; exists {
			t.Errorf("C in progress must clear A reset failure after B answered: %v", failure)
		}
		releaseTaskGate(release[1])
		if result := waitSubmission(t, cSub); result.Record.Status != "done" || result.Record.Answer == 0 {
			t.Fatalf("C must answer: %+v", result.Record)
		}
		if calls.Load() != 3 {
			t.Fatal("expected exactly A, B and C provider calls", calls.Load())
		}
	})
}
