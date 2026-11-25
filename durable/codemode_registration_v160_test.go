package durable

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/quickjs"
)

const v160RegisteredEcho = `return await tools.echo('a')`
const v160IsEchoRegistered = `return 'echo' in tools`

// packages/codemode/test/sandbox.test.ts:412–420. The exact standalone
// register/unregister APIs exist in the published JS hosts, not the Go engine.
func TestSourceV160CodemodePublishedHostRegistration(t *testing.T) {
	root := officialWorkerPackageRootV160(t)
	for _, placement := range []string{"inline", "portable", "node-worker"} {
		t.Run(placement, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", "testdata/worker-register-v160.mjs", placement, v160RegisteredEcho, v160IsEchoRegistered)
			cmd.Env = append(os.Environ(), "GOAI_WORKER_PACKAGE_ROOT="+root)
			raw, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("published host failed: %v\n%s", err, raw)
			}
			var got struct {
				Duplicate string
				Names     []string
				First     struct {
					OK    bool
					Value string
				}
				Removed bool
				Second  struct {
					OK    bool
					Value *bool
				}
				Callbacks int
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("published host returned invalid JSON: %v\n%s", err, raw)
			}
			if !strings.Contains(got.Duplicate, "already registered") || !reflect.DeepEqual(got.Names, []string{"echo"}) { // L415–416
				t.Errorf("duplicate=%q names=%v; want already registered and only echo", got.Duplicate, got.Names)
			}
			if !got.First.OK || got.First.Value != "a" || !got.Removed || !got.Second.OK || got.Second.Value == nil || *got.Second.Value || got.Callbacks != 1 { // L417–419
				t.Errorf("results=%+v; want a, successful removal, false, and one echo", got)
			}
		})
	}
}

// The native raw engine takes a fresh explicit tool snapshot each execution.
// Reuse the exact scalar caller and absent-tool script, not JS registry calls.
func TestV160CodemodeInlineEngineFreshToolSnapshots(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	engine, err := quickjs.Default()
	if err != nil {
		t.Fatal(err)
	}
	var count atomic.Int32
	echo := quickjs.Tool{Name: "echo", Execute: func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
		count.Add(1)
		if string(raw) != `"a"` {
			t.Errorf("echo arg=%s; want a", raw)
		}
		return raw, nil
	}}
	if got := engine.Execute(ctx, v160RegisteredEcho, quickjs.Options{Tools: []quickjs.Tool{echo}}); !got.OK || string(got.Value) != `"a"` {
		t.Fatalf("first execution=%+v; want a", got)
	}
	if got := engine.Execute(ctx, v160IsEchoRegistered, quickjs.Options{}); !got.OK || string(got.Value) != "false" || count.Load() != 1 {
		t.Errorf("next execution=%+v; want false and only one callback total", got)
	}
}

// Supplemental application-level test: the Go registry removes an extension,
// not a JS tool callback. No already running or stored snapshot is modified.
func TestV160CodemodePublicToolSnapshotAfterUninstall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sandbox, err := NewQuickJSSandbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(context.Background())
	registry := NewRegistry()
	codemode, err := NewCodemodeExtension(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	var effects atomic.Int32
	echo := wrapRegistration("echo")
	echo.Execute = func(_ context.Context, args any, _ *ToolAPI) (ToolResult, error) {
		effects.Add(1)
		return ToolResult{Content: args.(JSON)["text"].(string)}, nil
	}
	tools := Extension{Name: "echo_fixture", Tools: []ToolRegistration{echo}}
	for _, ext := range []*Extension{&codemode, &tools} {
		if err := registry.Install(ext); err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		switch requests.Add(1) {
		case 1:
			ch <- toolAnswer("before", "codemode", JSON{"code": `return await tools.echo({text:'a'})`})
		case 3:
			ch <- toolAnswer("after", "codemode", JSON{"code": v160IsEchoRegistered})
		default:
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	conversation := root(t, h, ref)
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	submit := func(id, want string) {
		t.Helper()
		sub, err := conversation.Submit(ctx, Input{Content: id})
		if err != nil {
			t.Fatal(err)
		}
		if status, err := sub.Wait(ctx); err != nil || status.Submission.Status != "done" {
			t.Fatalf("submission=%q err=%v; want done", status.Submission.Status, err)
		}
		result := v160ToolResult(t, ctx, conversation, id)
		if result.IsError || len(result.Content) != 2 || result.Content[1].Text != want {
			t.Errorf("%s result=%q error=%t; want %q", id, v160ToolResultText(result), result.IsError, want)
		}
	}
	submit("before", "a")
	registry.Uninstall(&tools)
	submit("after", "false")
	if effects.Load() != 1 || requests.Load() != 4 {
		t.Errorf("tool effects=%d provider requests=%d; want 1 and 4", effects.Load(), requests.Load())
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
