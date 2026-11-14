package durable

// Official1.5 f9a26365 harness-restart-failure.test.ts330/367/438. These
// narrowly coupled controls must pass before removing global restart parking.
import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func mainCutoffReports(p *mainAfterRecoveryProbe, options *Options) {
	options.OnReportWithInfo = func(_ context.Context, _ error, info ReportInfo) {
		p.mu.Lock()
		p.infos = append(p.infos, info)
		p.mu.Unlock()
	}
}
func mainCutoffReport(t *testing.T, p *mainAfterRecoveryProbe, task TaskRecord, restarts uint64) {
	t.Helper()
	id := task.ID
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, info := range p.infos {
		if info.Type == "task_interrupted" && info.Reason == "restart_limit" && info.Task != nil && info.Task.ID == id {
			if info.Task.Kind != task.Kind || info.Task.ConversationID != task.Conversation {
				t.Error("cutoff report task", info.Task, task)
			}
			encoded, err := json.Marshal(info)
			if err != nil {
				t.Fatal(err)
			}
			var raw map[string]any
			if err := json.Unmarshal(encoded, &raw); err != nil {
				t.Fatal(err)
			}
			if raw["restarts"] != float64(restarts) {
				t.Error("cutoff report count", string(encoded))
			}
			return
		}
	}
	t.Error("missing attributed interruption report", id, p.infos)
}
func mainCutoffTask(t *testing.T, h *Harness, id ID) TaskRecord {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	task, err := h.WaitForTask(ctx, id)
	if err != nil {
		t.Error("cutoff did not terminate", id, err)
		task, _, _ = h.Task(bg, id)
	}
	return task
}
func mainCutoffOutcome(t *testing.T, task TaskRecord, message string) {
	t.Helper()
	out := task.State.Outcome
	if out == nil || out.Status != "failed" || out.Error == nil || out.Error.Message != message || out.Error.Detail == nil || !equalJSONValue(out.Error.Detail.Value, JSON{"reason": "restart_limit"}) {
		t.Error("cutoff outcome", task)
	}
}
func mainCutoffStatus(t *testing.T, h *Harness, id ID) SubmissionRecord {
	t.Helper()
	handle, err := h.Submission(bg, id)
	if err != nil {
		t.Fatal(err)
	}
	status, err := handle.Status(bg)
	if err != nil {
		t.Fatal(err)
	}
	return status
}
func TestMainCutoffGenerationDuringPreparation(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		entered := make(chan struct{}, 4)
		var rendered, requests atomic.Int64
		registry := NewRegistry()
		if err := registry.Install(&Extension{Name: "preamble", Sections: []PromptSection{{Key: "preamble", Render: func(ctx context.Context, _ PromptInput) (*string, error) {
			rendered.Add(1)
			entered <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		}}}}); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			requests.Add(1)
			ch := make(chan goai.Event, 1)
			ch <- terminal("unexpected")
			close(ch)
			return ch
		})
		limit := uint64(1)
		options.Settings = &HarnessSettings{RestartLimit: &limit}
		options.Registry = registry
		p := &mainAfterRecoveryProbe{options: options, ref: ref}
		mainCutoffReports(p, &p.options)
		h := openHarness(t, b.store, p.options)
		c := root(t, h, ref)
		sub, err := c.Submit(bg, Input{Content: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		snap, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var id ID
		for _, candidate := range snap.Tasks {
			if candidate.Kind == "pi.generation" && candidate.Conversation == c.ID() && !terminalStatus(candidate.Status) {
				id = candidate.ID
			}
		}
		if id == 0 {
			t.Fatal("missing active generation")
		}
		store := mainAfterRecoveryCrash(t, h, b.store)
		h, c = mainAfterRecoveryOpen(t, p, store, c.ID())
		task := mainCutoffTask(t, h, id)
		// Native queued/preparation checkpoint is mapped to source phase `prepare`.
		message := "The generation was interrupted once in its prepare phase"
		mainCutoffOutcome(t, task, message)
		status := mainCutoffStatus(t, h, sub.ID())
		if status.Status != "unanswered" || status.Reason != "restart_limit" || status.Detail == nil || status.Detail.Value != message {
			t.Error("cutoff submission", status)
		}
		live := mainAfterRunLive(t, h, c.ID())
		failure := generationObject(live["runFailure"])
		if failure["reason"] != "restart_limit" || failure["detail"] != message || submissionEntryID(failure["taskId"]) != id || !equalJSONValue(failure["inputs"], []any{json.Number(strconv.FormatUint(uint64(sub.ID()), 10))}) || submissionEntryID(failure["first"]) == 0 || live["run"] != nil {
			t.Error("cutoff runFailure", live)
		}
		if rendered.Load() != 1 || requests.Load() != 0 {
			t.Error("cutoff reran preparation/provider", rendered.Load(), requests.Load())
		}
		if err := h.Close(bg); err != nil {
			t.Error(err)
		}
		mainCutoffReport(t, p, task, 1)
	})
}

func TestMainCutoffSafeToolKeepsStoredOutput(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		setup := mainToolRecoverySetup(t, true)
		limit := uint64(2)
		zero := int64(0)
		window := float64(0)
		options := setup.options
		options.Now = setup.clock.Load
		options.Settings = &HarnessSettings{RestartLimit: &limit, Progress: &ProgressSettings{OutputIntervalMs: &zero, PersistWithinMs: &window}}
		p := &mainAfterRecoveryProbe{options: options, ref: setup.ref}
		mainCutoffReports(p, &p.options)
		h := openHarness(t, b.store, p.options)
		c := root(t, h, setup.ref)
		sub, err := c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		store := b.store
		for crash := 0; crash < 2; crash++ {
			var run *mainToolRun
			select {
			case run = <-setup.runs:
			case <-time.After(3 * time.Second):
				t.Fatal("tool did not start", crash)
			}
			_, frames := recordUpstreamWindow(t, c)
			run.output <- "partial\n"
			for {
				frame := watchReceive(t, frames)
				if output, ok := mainToolOutput(frame.value.Docs["pi.live"]); ok && output == "partial\n" {
					break
				}
			}
			// Real dependent-read barrier persists output without changing task state.
			_, err = c.Commit(bg, func(tx *Tx) error {
				doc, err := builtin(tx, c.ID(), "pi.live")
				if err != nil {
					return err
				}
				_, err = doc.Get()
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			stored, err := store.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, doc := range stored.Documents {
				if doc.Kind == "pi.live" && doc.Owner == c.ID() {
					value, ok := mainToolOutput(doc.Value)
					found = ok && value == "partial\n"
				}
			}
			if !found {
				t.Fatal("stored tool prefix absent")
			}
			store = mainAfterRecoveryCrash(t, h, store)
			h, c = mainAfterRecoveryOpen(t, p, store, c.ID())
			if err := h.Resume(bg); err != nil {
				t.Fatal(err)
			}
		}
		mainAfterRecoveryIdle(t, c)
		if setup.count.Load() != 2 {
			t.Error("tool replayed at limit", setup.count.Load())
		}
		snap, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var id ID
		var result MessageReceipt
		for _, task := range snap.Tasks {
			if task.Kind == "pi.tool" {
				id = task.ID
			}
		}
		for _, entry := range snap.Entries {
			if entry.ByTask == id && legacyEntryValue(entry)["role"] == "toolResult" {
				if err := decodeEntryMessage(entry, h.session.limits, &result); err != nil {
					t.Fatal(err)
				}
			}
		}
		message := "Tool work was interrupted 2 times in a row; it is not run again"
		task := mainCutoffTask(t, h, id)
		mainCutoffOutcome(t, task, message)
		if !result.IsError || result.ErrorCode != "restart_limit" || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "restart_limit" || result.Diagnostics[0].Message != message {
			t.Error("cutoff receipt", result)
		}
		var text string
		for _, block := range result.Content {
			text += block.Text
		}
		if !strings.Contains(text, "partial") {
			t.Error("cutoff discarded stored output", text)
		}
		status := mainCutoffStatus(t, h, sub.ID())
		if status.Status != "done" {
			t.Error("parent did not answer", status)
		}
		if err := h.Close(bg); err != nil {
			t.Error(err)
		}
		mainCutoffReport(t, p, task, 2)
	})
}

func TestMainCutoffCompactionPublishesFailure(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		started := make(chan struct{}, 4)
		var requests atomic.Int64
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if requests.Add(1) <= 2 {
				ch <- terminal("answer")
				close(ch)
			} else {
				started <- struct{}{}
				go func() { <-ctx.Done(); close(ch) }()
			}
			return ch
		})
		limit := uint64(1)
		disabled := false
		keep := 10
		options.Settings = &HarnessSettings{RestartLimit: &limit, Compaction: &CompactionSettings{Enabled: &disabled, KeepRecentTokens: &keep}}
		p := &mainAfterRecoveryProbe{options: options, ref: ref}
		mainCutoffReports(p, &p.options)
		h := openHarness(t, b.store, p.options)
		c := root(t, h, ref)
		for _, s := range []string{"u1 ", "u2 "} {
			sub, err := c.Submit(bg, Input{Content: strings.Repeat(s, 200)})
			if err != nil {
				t.Fatal(err)
			}
			waitSubmission(t, sub)
		}
		id, err := c.Compact(bg, nil)
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, started)
		store := mainAfterRecoveryCrash(t, h, b.store)
		h, c = mainAfterRecoveryOpen(t, p, store, c.ID())
		task := mainCutoffTask(t, h, id)
		message := "Compaction was interrupted once"
		mainCutoffOutcome(t, task, message)
		live := mainAfterRunLive(t, h, c.ID())
		failure := generationObject(live["compactionFailure"])
		// Native compaction status uses `reason` for source `trigger`.
		if submissionEntryID(failure["taskId"]) != id || failure["reason"] != "manual" || failure["error"] != message || live["compactions"] != nil {
			t.Error("compaction cutoff status", live)
		}
		if requests.Load() != 3 {
			t.Error("compaction redispatched", requests.Load())
		}
		if err := h.Close(bg); err != nil {
			t.Error(err)
		}
		mainCutoffReport(t, p, task, 1)
	})
}
