package durable

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

const v160StoreAndGlobal = `store("kept", "builtin"); return [load("kept"), await store1()];`

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:481–485.
func TestSourceV160CodemodePublishedReservedStoreNames(t *testing.T) {
	root := officialWorkerPackageRootV160(t)
	for _, placement := range []string{"inline", "portable", "node-worker"} {
		t.Run(placement, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", "testdata/worker-reserved-names-v160.mjs", placement, v160StoreAndGlobal)
			cmd.Env = append(os.Environ(), "GOAI_WORKER_PACKAGE_ROOT="+root)
			raw, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("published constructor probe failed: %v\n%s", err, raw)
			}
			var got struct {
				Rejected map[string]string
				Result   struct {
					OK    bool
					Value []string
				}
				Callbacks int
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("published constructor returned invalid JSON: %v\n%s", err, raw)
			}
			for _, name := range []string{"store", "load", "store.member", "load.member"} {
				if want := `Invalid global name "` + name + `"`; got.Rejected[name] != want {
					t.Errorf("constructor with %s: %q, want %q", name, got.Rejected[name], want)
				}
			}
			if !got.Result.OK || !reflect.DeepEqual(got.Result.Value, []string{"builtin", "callback"}) || got.Callbacks != 1 {
				t.Errorf("nonreserved global and builtins: %+v; want builtin, callback, one effect", got)
			}
		})
	}
}

// The native engine accepts globals at Execute rather than construction. It
// should reject the names during setup, before acquiring capacity for a VM.
func TestV160CodemodeNativeReservedStoreNames(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	engine, err := quickjs.NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	var callbacks, acquired atomic.Int32
	execute := func(context.Context, json.RawMessage) (json.RawMessage, error) {
		callbacks.Add(1)
		return json.RawMessage(`"callback"`), nil
	}
	run := func(name, script string) quickjs.Result {
		return engine.Execute(ctx, script, quickjs.Options{
			Globals: []quickjs.Global{{Name: name, Execute: execute}},
			AcquireWorker: func() (func(), error) {
				acquired.Add(1)
				return func() {}, nil
			},
		})
	}
	for _, name := range []string{"store", "load", "store.member", "load.member"} {
		t.Run(name, func(t *testing.T) {
			before := acquired.Load()
			root, _, _ := strings.Cut(name, ".")
			got := run(name, `return "unreachable";`)
			if got.OK || got.Error == nil || got.Error.Kind != quickjs.ErrorSandbox || got.Error.Message != `Invalid global name "`+root+`"` || acquired.Load() != before || callbacks.Load() != 0 {
				t.Errorf("%s result: ok=%v error=%v worker reservations=%d callbacks=%d; want setup rejection before capacity", name, got.OK, got.Error, acquired.Load()-before, callbacks.Load())
			}
		})
	}
	before := acquired.Load()
	if got := run("store1", v160StoreAndGlobal); !got.OK || string(got.Value) != `["builtin","callback"]` || acquired.Load()-before != 1 || callbacks.Load() != 1 {
		t.Fatalf("allowed global result: ok=%v value=%s error=%v reservations=%d callbacks=%d", got.OK, got.Value, got.Error, acquired.Load()-before, callbacks.Load())
	}
}

func TestV160CodemodeHarnessReservedStoreNames(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	callback := func(context.Context, any) (any, error) { return nil, nil }
	open := func(name string) (*Harness, error) {
		return Open(t.Context(), store, Options{CodemodeGlobals: []CodemodeGlobal{{Name: name, Version: 1, Execute: callback}}})
	}
	for _, name := range []string{"store", "load"} {
		h, err := open(name)
		if h != nil {
			_ = h.Close(context.Background())
		}
		var rejected *StorageRejected
		if h != nil || !errors.As(err, &rejected) || rejected.Reason != "invalid codemode global" {
			t.Fatalf("open with %s: harness=%t error=%v; want invalid global rejection", name, h != nil, err)
		}
	}
	h, err := open("store1")
	if err != nil {
		t.Fatalf("nonreserved global could not open the same storage: %v", err)
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
