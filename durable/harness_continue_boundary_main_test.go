package durable

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Selected upstream test-body translation: official v1.5.0
// f9a26365568127461c47a07a37dcb42c140a3ac5, source-continue.test.ts:395-422,
// "places inputs a failed run left queued first, and they join its run".
// Additional source-derived assertions: source-submissions.ts:264-274 and
// source-inbox.ts applyBoundary. A real model-contributing write W is submitted
// AFTER B but must land BEFORE B; run.first inherits A's failed history, not W.
// Native adaptations: numeric run.task, follow-up input mode, registered fake
// provider, and detached public Status/Wait/Snapshot/SubscribeCommits records.
// No alternate Continue helper, empty user input, or checkpoint seeding is used.
func TestMainContinueBoundaryKeptInputsAndWrite(t *testing.T) {
	mainContinueBoundaryCase(t, false)
}

// Source-derived legacy fixture, NOT an additional official test-body port:
// submissions.ts uses live.runFailure?.first ?? boundary.first. Only the
// optional historical first field is removed from an actually failed run;
// A/B/W, the kept inbox, and all placements still use production paths.
func TestMainContinueBoundaryLegacyFirstFallsBackToActualWrite(t *testing.T) {
	mainContinueBoundaryCase(t, true)
}

func mainContinueBoundaryDocument(t *testing.T, state Snapshot, conversation ID, kind string) JSON {
	t.Helper()
	var value JSON
	count := 0
	for _, doc := range state.Documents {
		if !doc.Retired && doc.Scope == "conversation" && doc.Owner == conversation && doc.Kind == kind {
			value, count = doc.Value, count+1
		}
	}
	if count != 1 {
		t.Fatalf("expected one actual %s document, got %d", kind, count)
	}
	return value
}

// Observe the exact stored admission, not a reconstruction from a later
// scheduler snapshot. The request ID identifies K before Submit returns its ID.
func mainContinueBoundaryAdmission(t *testing.T, h *Harness, conversation ID, request string) func() sessionPublication {
	t.Helper()
	_, observer, err := h.SubscribeCommits(bg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { observer.Stop() })
	frames, ready := make(chan PublicationFrame, 1), make(chan struct{}, 1)
	if err := observer.Start(func(_ context.Context, frame PublicationFrame) error {
		for _, write := range frame.Publication.Tables {
			if sub := write.Submission; sub != nil && sub.Conversation == conversation && sub.RequestID == request && sub.Type == "continue" {
				select {
				case frames <- frame:
					ready <- struct{}{}
				default:
				}
				break
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return func() sessionPublication {
		t.Helper()
		awaitTaskSignal(t, ready)
		frame := <-frames
		if frame.Snapshot != nil || frame.Publication.Seq == 0 {
			t.Fatal("expected exact stored continue admission, not resnapshot", frame)
		}
		return frame.Publication
	}
}

func mainContinueBoundaryCase(t *testing.T, legacyFirstAbsent bool) {
	t.Helper()
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		entered := []chan struct{}{make(chan struct{}), make(chan struct{})}
		release := []chan struct{}{make(chan struct{}), make(chan struct{})}
		seen := make(chan []goai.Message, 1)
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, request *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			n := calls.Add(1)
			if n > 2 {
				t.Errorf("unexpected provider call %d", n)
				close(ch)
				return ch
			}
			if n == 2 {
				seen <- request.Messages
			}
			close(entered[n-1])
			go func() {
				defer close(ch)
				select {
				case <-release[n-1]:
				case <-ctx.Done():
					return
				}
				if n == 1 {
					message := terminal("").Message
					message.Content = nil
					message.StopReason, message.ErrorMessage = goai.StopReasonError, "Invalid request"
					ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: message}
				} else {
					ch <- terminal("both handled")
				}
			}()
			return ch
		})
		options.Settings = &HarnessSettings{AfterFailedRun: "keep", FollowUpMode: "one-at-a-time"}
		options.OnReport = func(err error) { t.Error("scheduler", err) }
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release...)
		c := root(t, h, ref)
		snapshot := func() Snapshot {
			t.Helper()
			state, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			return state
		}
		requireJSON := func(label string, got any, want JSON) {
			t.Helper()
			wire, err := dtoObject(want, h.session.limits)
			if err != nil {
				t.Fatal(err)
			}
			if !equalJSONValue(got, wire) {
				t.Errorf("%s: got %v; want %v", label, got, wire)
			}
		}
		requireQueued := func(state Snapshot, subs ...*SubmissionHandle) {
			t.Helper()
			inbox := mainContinueBoundaryDocument(t, state, c.ID(), "pi.inbox")
			items, ok := inbox["items"].([]any)
			if !ok || len(items) != len(subs) {
				t.Fatalf("kept inbox must contain exactly B then W: %v", inbox)
			}
			for i, sub := range subs {
				record, exists := state.Submissions[sub.ID()]
				status := publicSubmission(record)
				mode := "followUp"
				if status.Type == "write" {
					mode = "write"
				}
				item := generationObject(items[i])
				if !exists || status.Status != "queued" || status.Entry != 0 || status.Answer != 0 || submissionEntryID(item["id"]) != sub.ID() || item["mode"] != mode {
					t.Fatalf("B/W must remain genuinely queued and unplaced in order: %+v, %v", status, item)
				}
			}
		}

		a, err := c.Submit(bg, Input{Content: "first"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered[0])
		state := snapshot()
		aStatus := publicSubmission(state.Submissions[a.ID()])
		aRun := generationObject(mainContinueBoundaryDocument(t, state, c.ID(), "pi.live")["run"])
		aOwner := submissionEntryID(aRun["task"])
		if aStatus.Type != "input" || aStatus.Status != "placed" || aStatus.Entry == 0 || aOwner == 0 || state.Tasks[aOwner].Kind != "pi.generation" {
			t.Fatal("A must own a real placed generation while gated", aStatus, aRun)
		}
		requireJSON("A run", aRun, JSON{"task": aOwner, "inputs": []any{a.ID()}, "first": aStatus.Entry})
		queued, err := c.Submit(bg, Input{Content: "second", WhenBusy: "follow-up"})
		if err != nil {
			t.Fatal(err)
		}
		write, err := c.Submit(bg, Input{Type: "write", Entry: &Entry{Kind: "app.note", Value: JSON{"text": "W"}, Model: []MessageReceipt{userReceipt("W")}}})
		if err != nil {
			t.Fatal(err)
		}
		requireQueued(snapshot(), queued, write)
		releaseTaskGate(release[0])
		failed := waitSubmission(t, a)
		if failed.Record.Status != "unanswered" || failed.Record.Reason != "model_error" || failed.Record.Entry != aStatus.Entry || failed.Record.Answer != 0 || failed.Task.ID != aOwner || failed.Record.Detail == nil || !failed.Record.Detail.Present || failed.Record.Detail.Value != "Invalid request" {
			t.Fatal("A must fail at its own input with the literal upstream error", failed)
		}
		idleFailed140(t, h)
		state = snapshot()
		requireQueued(state, queued, write)
		live := mainContinueBoundaryDocument(t, state, c.ID(), "pi.live")
		failure := JSON{"taskId": aOwner, "inputs": []any{a.ID()}, "first": aStatus.Entry, "reason": "model_error", "detail": "Invalid request"}
		requireJSON("actual kept A failure", live, JSON{"runFailure": failure, "failedRun": true})
		if calls.Load() != 1 {
			t.Fatal("kept failure must not dispatch B or W", calls.Load())
		}

		if legacyFirstAbsent {
			// The sole fixture adaptation emulates older persisted metadata.
			// Do not fabricate a run, inbox item, task, entry or settlement.
			if _, err := h.Commit(bg, func(tx *Tx) error {
				doc, err := builtin(tx, c.ID(), "pi.live")
				if err != nil {
					return err
				}
				return doc.Update(func(value JSON) error {
					delete(generationObject(value["runFailure"]), "first")
					return nil
				})
			}); err != nil {
				t.Fatal(err)
			}
			delete(failure, "first")
			state = snapshot()
			requireQueued(state, queued, write)
			requireJSON("legacy failure differs only by absent first", mainContinueBoundaryDocument(t, state, c.ID(), "pi.live"), JSON{"runFailure": failure, "failedRun": true})
		}
		before := state
		admission := mainContinueBoundaryAdmission(t, h, c.ID(), "boundary-continue")
		continued, err := c.Submit(bg, Input{Type: "continue", RequestID: "boundary-continue"})
		if err != nil {
			t.Fatalf("public Input{Type: continue} admission: %v (production may still be unsupported)", err)
		}
		publication := admission()
		// The resumed provider is gated, so B/K cannot settle before admission
		// status, boundary entries and the actual request have been inspected.
		awaitTaskSignal(t, entered[1])
		state = snapshot()
		kStatus, err := continued.Status(bg)
		if err != nil || kStatus.Type != "continue" || kStatus.Status != "placed" || kStatus.Entry != 0 || kStatus.Answer != 0 {
			t.Fatalf("K must be placed without any own entry/answer: %+v, %v", kStatus, err)
		}
		bStatus, err := queued.Status(bg)
		if err != nil || bStatus.Type != "input" || bStatus.Status != "placed" || bStatus.Entry == 0 || bStatus.Answer != 0 {
			t.Fatalf("B must be placed at its own entry before resumed answer: %+v, %v", bStatus, err)
		}
		wStatus, err := write.Status(bg)
		if err != nil || wStatus.Type != "write" || wStatus.Status != "done" || wStatus.Entry == 0 || wStatus.Answer != 0 {
			t.Fatalf("W must settle done at its actual entry: %+v, %v", wStatus, err)
		}
		wEntry, wExists := state.Entries[wStatus.Entry]
		bEntry, bExists := state.Entries[bStatus.Entry]
		if !wExists || wEntry.Conversation != c.ID() || wEntry.Kind != "app.note" || legacyEntryValue(wEntry)["text"] != "W" || len(wEntry.Model) != 1 || wEntry.Model[0].Role != goai.RoleUser || len(wEntry.Model[0].Content) != 1 || wEntry.Model[0].Content[0].Type != "text" || wEntry.Model[0].Content[0].Text != "W" {
			t.Fatal("W must retain its real model contribution and application entry", wEntry)
		}
		if !bExists || bEntry.Conversation != c.ID() || !isMessageEntry(bEntry) || bEntry.ID == wEntry.ID || bEntry.ID == aStatus.Entry || wEntry.ID == aStatus.Entry {
			t.Fatal("B must own a distinct persisted user entry, not W or A", bEntry, wEntry)
		}
		var bMessage MessageReceipt
		if err := decodeEntryMessage(bEntry, h.session.limits, &bMessage); err != nil {
			t.Fatal(err)
		}
		if bMessage.Role != goai.RoleUser || len(bMessage.Content) != 1 || bMessage.Content[0].Type != "text" || bMessage.Content[0].Text != "second" {
			t.Fatal("B entry must retain the actual queued input", bMessage)
		}
		if wEntry.Seq != publication.Seq || bEntry.Seq != publication.Seq || wEntry.Position >= bEntry.Position {
			t.Errorf("W then B must append in K's admission: seq=%d W=(%d,%d), B=(%d,%d)", publication.Seq, wEntry.Seq, wEntry.Position, bEntry.Seq, bEntry.Position)
		}
		messages := <-seen
		if len(messages) != 3 {
			t.Errorf("resumed provider context must be exactly A,W,B, without a K user: %+v", messages)
		} else {
			for i, text := range []string{"first", "W", "second"} {
				message := messages[i]
				if message.Role != goai.RoleUser || len(message.Content) != 1 || message.Content[0].Type != "text" || message.Content[0].Text != text {
					t.Errorf("resumed provider message %d = %+v; want user %q", i, message, text)
				}
			}
		}

		// Assert the original admission publication independently. A later
		// request-preparation commit cannot repair missing same-batch placement.
		subs, tasks := map[ID]Submission{}, map[ID]Task{}
		var appended []ID
		var admittedLive, admittedInbox JSON
		for _, change := range publication.Tables {
			if change.Submission != nil {
				subs[change.Submission.ID] = *change.Submission
			}
			if change.Task != nil {
				tasks[change.Task.ID] = *change.Task
			}
			if change.Op == "append-entry" && change.Entry != nil && change.Entry.Conversation == c.ID() {
				appended = append(appended, change.Entry.ID)
			}
		}
		for _, change := range publication.Documents {
			if change.Record.Owner == c.ID() && !change.Record.Retired {
				switch change.Record.Kind {
				case "pi.live":
					admittedLive = change.Value
				case "pi.inbox":
					admittedInbox = change.Value
				}
			}
		}
		if !reflect.DeepEqual(appended, []ID{wEntry.ID, bEntry.ID}) {
			t.Errorf("admission must append exactly W then B, never a K entry: %v", appended)
		}
		for _, id := range appended {
			if _, existed := before.Entries[id]; existed {
				t.Error("admission reused an old entry", id)
			}
		}
		for _, expected := range []SubmissionRecord{kStatus, bStatus, wStatus} {
			got := publicSubmission(subs[expected.ID])
			if got.ID != expected.ID || got.Type != expected.Type || got.Status != expected.Status || got.Entry != expected.Entry || got.Answer != expected.Answer || got.Conversation != c.ID() {
				t.Errorf("admission must publish K/B placement and W settlement together: got %+v; want %+v", got, expected)
			}
		}
		if _, touched := subs[a.ID()]; touched {
			t.Error("continue admission must not resettle failed A")
		}
		wantFirst := aStatus.Entry
		if legacyFirstAbsent {
			wantFirst = wEntry.ID // Actual append, not a seeded/fixed entry ID.
		}
		run := generationObject(admittedLive["run"])
		owner := submissionEntryID(run["task"])
		if owner == 0 || owner == aOwner {
			t.Fatal("continue must start a real new generation in its admission", admittedLive)
		}
		requireJSON("admission run inherits failure first, or actual W only if absent", run, JSON{"task": owner, "inputs": []any{queued.ID(), continued.ID()}, "first": wantFirst})
		task, exists := tasks[owner]
		if !exists || task.Kind != "pi.generation" || task.Conversation != c.ID() || terminalStatus(task.Status) {
			t.Error("same admission must create/start the actual B/K generation", task)
		}
		for label, value := range map[string]JSON{"admission": admittedLive, "gated snapshot": mainContinueBoundaryDocument(t, state, c.ID(), "pi.live")} {
			if _, exists := value["runFailure"]; exists {
				t.Errorf("%s retained old A failure: %v", label, value)
			}
			if _, exists := value["failedRun"]; exists {
				t.Errorf("%s retained native kept-queue gate: %v", label, value)
			}
			requireJSON(label+" run", value["run"], JSON{"task": owner, "inputs": []any{queued.ID(), continued.ID()}, "first": wantFirst})
		}
		requireJSON("admission empties selected inbox", admittedInbox, JSON{"items": []any{}})
		requireJSON("gated snapshot inbox", mainContinueBoundaryDocument(t, state, c.ID(), "pi.inbox"), JSON{"items": []any{}})

		releaseTaskGate(release[1])
		byQueued, byContinue := waitSubmission(t, queued), waitSubmission(t, continued)
		for _, result := range []Settlement{byQueued, byContinue} {
			if result.Record.Status != "done" || result.Record.Answer == 0 || result.Record.Answer != byContinue.Record.Answer || result.Task.ID != owner {
				t.Errorf("B/K must share the same actual answer and generation: %+v", result)
			}
		}
		if byContinue.Record.Type != "continue" || byContinue.Record.Entry != 0 || byQueued.Record.Type != "input" || byQueued.Record.Entry != bEntry.ID {
			t.Error("settlement must retain entryless K and B's own placement", byQueued.Record, byContinue.Record)
		}
		idleFailed140(t, h)
		state = snapshot()
		answer, exists := state.Entries[byContinue.Record.Answer]
		var receipt MessageReceipt // Native message receipts persist in Value.
		decodeErr := decodeEntryMessage(answer, h.session.limits, &receipt)
		if !exists || answer.Conversation != c.ID() || decodeErr != nil || receipt.Role != goai.RoleAssistant || len(receipt.Content) != 1 || receipt.Content[0].Type != "text" || receipt.Content[0].Text != "both handled" {
			t.Error("shared persisted answer must contain upstream 'both handled'", answer, decodeErr)
		}
		for _, expected := range []SubmissionRecord{byQueued.Record, byContinue.Record, wStatus, failed.Record} {
			handle, err := h.Submission(bg, expected.ID)
			if err != nil || handle == nil {
				t.Fatal("missing final submission", expected.ID, err)
			}
			got, err := handle.Status(bg)
			if err != nil || got.Type != expected.Type || got.Status != expected.Status || got.Entry != expected.Entry || got.Answer != expected.Answer || got.Reason != expected.Reason {
				t.Errorf("final status must preserve B/K answer, W entry, and A failure: got %+v, %v; want %+v", got, err, expected)
			}
		}
		var users []ID
		for _, entry := range state.Entries {
			if entry.Conversation == c.ID() && isMessageEntry(entry) && legacyEntryValue(entry)["role"] == "user" {
				users = append(users, entry.ID)
				if entry.ID != aStatus.Entry && entry.ID != bEntry.ID {
					t.Error("continue fabricated a user entry", entry)
				}
			}
		}
		if len(users) != 2 || calls.Load() != 2 {
			t.Error("only A/B user entries and two provider calls are allowed", users, calls.Load())
		}
	})
}
