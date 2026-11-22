package durable

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type recoveryEnvironment string

func (e recoveryEnvironment) Cwd() string { return string(e) }

func TestToolRecoveryPinnedCurrentAgentCwdAndDeselection(t *testing.T) {
	for _, change := range []string{"cwd", "deselect", "clear", "caller-revoked", "model-offer-removed", "newly-deferred"} {
		t.Run(change, func(t *testing.T) {
			registry := NewRegistry()
			var runs atomic.Int64
			cwds := []string{}
			entered, release := make(chan struct{}), make(chan struct{})
			reg := wrapRegistration("work")
			reg.Execute = func(ctx context.Context, _ any, api *ToolAPI) (ToolResult, error) {
				env, err := api.Environment(ctx)
				if err != nil {
					return ToolResult{}, err
				}
				cwds = append(cwds, env.Cwd())
				if runs.Add(1) == 1 {
					close(entered)
					<-release
					<-ctx.Done()
					return ToolResult{}, ctx.Err()
				}
				return ToolResult{Content: "rerun"}, nil
			}
			if err := registry.Install(&Extension{Name: "coding", Tools: []ToolRegistration{reg}}); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int64
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if calls.Add(1) == 1 {
					ch <- toolAnswer("work-call", "work", JSON{})
				} else {
					ch <- terminal("done")
				}
				close(ch)
				return ch
			})
			options.Registry = registry
			options.Env = func(_ context.Context, target EnvTarget) (ExecutionEnvironment, error) {
				return recoveryEnvironment(target.Cwd), nil
			}
			store, dir := newSQLiteTestStore(t)
			h := openHarness(t, store, options)
			conversation := root(t, h, ref)
			cwd := "/one"
			names := []string{"coding"}
			if err := conversation.ConfigurePatch(bg, AgentPatch{Cwd: &cwd, Extensions: &names}); err != nil {
				t.Fatal(err)
			}
			sub, err := conversation.Submit(bg, Input{Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			<-entered
			patch := AgentPatch{}
			switch change {
			case "cwd":
				cwd = "/two"
				patch.Cwd = &cwd
			case "deselect":
				names = []string{}
				patch.Extensions = &names
			case "clear":
				patch.Clear = []string{"cwd", "extensions"}
			case "caller-revoked":
				reg.Callers = []string{"tools"}
				if err := registry.Install(&Extension{Name: "coding", Tools: []ToolRegistration{reg}}); err != nil {
					t.Fatal(err)
				}
			case "model-offer-removed":
				patch.ModelTools = &ExtensionFilter{Remove: []string{"work"}}
			case "newly-deferred":
				reg.Deferred = true
				if err := registry.Install(&Extension{Name: "coding", Tools: []ToolRegistration{reg}}); err != nil {
					t.Fatal(err)
				}
			}
			if err := conversation.ConfigurePatch(bg, patch); err != nil {
				t.Fatal(err)
			}
			closed := make(chan error, 1)
			go func() { closed <- h.Close(bg) }()
			<-h.life.Done()
			close(release)
			if err := <-closed; err != nil {
				t.Fatal(err)
			}
			store, err = openSQLiteTestDirectory(dir, SQLiteOptions{})
			if err != nil {
				t.Fatal(err)
			}
			h = openHarness(t, store, options)
			if runs.Load() != 1 {
				t.Fatal("open dispatched", runs.Load())
			}
			resumed, err := h.Submission(bg, sub.ID())
			if err != nil {
				t.Fatal(err)
			}
			if settled := waitSubmission(t, resumed).Record; settled.Status != "done" {
				t.Fatalf("recovered submission: %+v", settled)
			}
			want := int64(2)
			if change == "deselect" || change == "caller-revoked" || change == "model-offer-removed" || change == "newly-deferred" {
				want = 1
			}
			if runs.Load() != want {
				t.Fatal("current selection not applied", runs.Load(), want)
			}
			wantCwds := []string{"/one"}
			if change == "cwd" {
				wantCwds = append(wantCwds, "/two")
			} else if change == "clear" {
				wantCwds = append(wantCwds, "")
			}
			if !reflect.DeepEqual(cwds, wantCwds) {
				t.Fatalf("tool cwd sequence %q; want %q", cwds, wantCwds)
			}
			snapshot, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			toolTasks := 0
			for _, task := range snapshot.Tasks {
				if task.Kind != "pi.tool" {
					continue
				}
				toolTasks++
				record, found, err := h.Task(bg, task.ID)
				outcome := record.State.Outcome
				if err != nil || !found || record.State.Status != "terminal" || outcome == nil || outcome.Result == nil || !outcome.Result.Present {
					t.Fatalf("terminal tool receipt: %+v, found %t, err %v", record, found, err)
				}
				entryID := submissionEntryID(generationObject(outcome.Result.Value)["entryId"])
				entry, found, err := h.Entry(bg, entryID)
				if err != nil || !found {
					t.Fatalf("tool receipt entry %d: found %t, err %v", entryID, found, err)
				}
				var receipt MessageReceipt
				if err := decodeEntryMessage(entry, h.session.limits, &receipt); err != nil || receipt.ToolCallID != "work-call" || receipt.IsError != (want == 1) {
					t.Fatalf("tool result %+v, err %v", receipt, err)
				}
				if want == 1 {
					if outcome.Status != "failed" || outcome.Error == nil || outcome.Error.Message != "Tool work was interrupted and may have partially run" ||
						len(receipt.Content) != 1 || !strings.Contains(receipt.Content[0].Text, "was interrupted") {
						t.Fatalf("revoked tool should be interrupted: status %q error %+v", outcome.Status, outcome.Error)
					}
				} else if outcome.Status != "completed" || outcome.Error != nil {
					t.Fatalf("retained tool should complete: %+v", outcome)
				}
			}
			if toolTasks != 1 {
				t.Fatalf("tool task count = %d; want one", toolTasks)
			}
		})
	}
}
