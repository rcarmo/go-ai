package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

type recoveryEnvironment string

func (e recoveryEnvironment) Cwd() string { return string(e) }

func TestToolRecoveryPinnedCurrentAgentCwdAndDeselection(t *testing.T) {
	for _, change := range []string{"cwd", "deselect", "clear"} {
		t.Run(change, func(t *testing.T) {
			registry := NewRegistry()
			var runs atomic.Int64
			cwds := []string{}
			entered, release := make(chan struct{}), make(chan struct{})
			reg := wrapRegistration("work")
			reg.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
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
			store, dir := newJournal(t)
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
			store, err = OpenJournal(dir, JournalOptions{})
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
			waitSubmission(t, resumed)
			want := int64(2)
			if change == "deselect" {
				want = 1
			}
			if runs.Load() != want {
				t.Fatal("current selection not applied", runs.Load(), want)
			}
			if cwds[0] != "/one" {
				t.Fatal(cwds)
			}
			if change == "cwd" && cwds[1] != "/two" || change == "clear" && cwds[1] != "" {
				t.Fatal("current cwd", cwds)
			}
			snapshot, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			for _, task := range snapshot.Tasks {
				if task.Kind == "pi.tool" {
					var cp toolCheckpoint
					if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
						t.Fatal(err)
					}
					if change == "deselect" && cp.ErrorCode != "interrupted" {
						t.Fatal(cp.ErrorCode)
					}
				}
			}
		})
	}
}
