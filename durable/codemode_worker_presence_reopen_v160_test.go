package durable

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type workerPresencePinProbeV160 struct {
	*CodemodeWorkerRemote
	seen      chan bool
	observed  chan struct{}
	supported atomic.Bool
}

func (p *workerPresencePinProbeV160) SupportsWorkerArgumentPresence() bool {
	return p.supported.Load()
}

func (p *workerPresencePinProbeV160) Execute(ctx context.Context, source string, start CodemodeStart, exchange func(context.Context, CodemodeEvent) (CodemodeRemoteReply, error), receive func(context.Context) (CodemodeDelivery, error)) error {
	p.seen <- start.WorkerArgumentPresence
	return p.CodemodeWorkerRemote.Execute(ctx, source, start, func(ctx context.Context, event CodemodeEvent) (CodemodeRemoteReply, error) {
		reply, err := exchange(ctx, event)
		if err == nil && event.Call != nil {
			select {
			case p.observed <- struct{}{}:
			case <-ctx.Done():
				return CodemodeRemoteReply{}, ctx.Err()
			}
		}
		return reply, err
	}, receive)
}

// Supplemental: a worker run pins its argument projection before admission.
// Reopen with the opposite capability after the first global reply commits.
// The first reply must replay exactly, and the new null call uses the old mode.
func TestV160CodemodeWorkerArgumentModePinnedAcrossReopen(t *testing.T) {
	for _, first := range []struct {
		name, want string
		modern     bool
	}{{"legacy", `["empty","empty"]`, false}, {"modern", `["undefined","null"]`, true}} {
		t.Run(first.name, func(t *testing.T) {
			const identity = "worker-argument-mode-pin-v160"
			const code = `return [await readArg(), await readArg(null)]`
			var blocked atomic.Bool
			blocked.Store(true)
			committed := make(chan struct{})
			observed := make(chan struct{})
			transport := workerTransport120(func(ctx context.Context, req CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
				if req.Data.Code != code {
					return fmt.Errorf("unexpected worker code %q", req.Data.Code)
				}
				if _, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "started", Fingerprint: identity}}); err != nil {
					return err
				}
				var running sync.WaitGroup
				defer running.Wait()
				var results []string
				for index, arg := range []*string{nil, workerString120(`null`)} {
					id := index + 1
					type receipt struct {
						reply *CodemodeWorkerReply
						err   error
					}
					delivered := make(chan receipt, 1)
					running.Go(func() {
						reply, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "call", ID: id, Target: "global", Name: "readArg", Args: arg}})
						delivered <- receipt{reply, err}
					})
					select {
					case <-observed:
					case premature := <-delivered:
						return fmt.Errorf("worker call before publication: %+v", premature)
					case <-ctx.Done():
						return ctx.Err()
					}
					if _, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "idle", Seq: index, Issued: id}}); err != nil {
						return err
					}
					select {
					case got := <-delivered:
						if got.err != nil || got.reply == nil || !got.reply.OK || got.reply.Payload == nil {
							return fmt.Errorf("worker callback result: %+v", got)
						}
						results = append(results, *got.reply.Payload)
					case <-ctx.Done():
						return ctx.Err()
					}
					if id == 1 && blocked.Load() {
						close(committed)
						<-ctx.Done()
						return ctx.Err()
					}
				}
				value := "[" + results[0] + "," + results[1] + "]"
				_, err := exchange(ctx, []CodemodeWorkerMessage{{Type: "done", OK: true, Value: &value, Writes: workerString120(`[]`), During: &CodemodeWorkerDuring{Seq: 2, Issued: 2}}})
				return err
			})
			probe := &workerPresencePinProbeV160{CodemodeWorkerRemote: &CodemodeWorkerRemote{Transport: transport, Fingerprint: identity}, seen: make(chan bool, 2), observed: observed}
			probe.supported.Store(first.modern)
			sandbox := &RemoteCodemodeSandbox{Remote: probe, Identity: identity}
			registry := NewRegistry()
			def := taskDefinition(t, "worker-argument-mode-pin", func(ctx context.Context, _ TaskRecord, runtime *TaskRuntime) error {
				result, err := runtime.RunCodemode(ctx, "script", code, sandbox)
				if err != nil {
					return err
				}
				if !result.HasValue || result.Output != first.want || result.Error != "" {
					return fmt.Errorf("worker result %+v; want %s", result, first.want)
				}
				return runtime.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			})
			if _, err := registry.RegisterTask(def); err != nil {
				t.Fatal(err)
			}
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			global := CodemodeGlobal{Name: "readArg", Version: 1, Execute: func(_ context.Context, arg any) (any, error) {
				calls.Add(1)
				switch arg := arg.(type) {
				case codemodeUndefined:
					return "undefined", nil
				case nil:
					return "null", nil
				case []any:
					if len(arg) == 0 {
						return "empty", nil
					}
				}
				return nil, fmt.Errorf("unexpected worker argument %#v", arg)
			}}
			h := openHarness(t, store, Options{Registry: registry, CodemodeGlobals: []CodemodeGlobal{global}})
			id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			if err := h.Resume(bg); err != nil {
				t.Fatal(err)
			}
			if got := awaitV160WorkerPresence(t, probe.seen); got != first.modern {
				t.Fatalf("initial worker argument presence = %t; want %t", got, first.modern)
			}
			awaitTaskSignal(t, committed)
			if n := calls.Load(); n != 1 {
				t.Fatalf("before close: callbacks = %d; want 1", n)
			}
			if err := h.Close(bg); err != nil {
				t.Fatal(err)
			}
			blocked.Store(false)
			probe.supported.Store(!first.modern)
			next := openHarness(t, reopenStoreAfterHarnessClose(t, store), h.options)
			record := waitPublicTask(t, next, id)
			if got := awaitV160WorkerPresence(t, probe.seen); got != first.modern {
				t.Fatalf("replayed worker argument presence = %t; want persisted %t", got, first.modern)
			}
			if record.State.Outcome.Status != "completed" || calls.Load() != 2 {
				t.Fatalf("replayed worker outcome = %+v, callbacks = %d", record.State.Outcome, calls.Load())
			}
		})
	}
}

func awaitV160WorkerPresence(t *testing.T, seen <-chan bool) bool {
	t.Helper()
	select {
	case mode := <-seen:
		return mode
	case <-time.After(5 * time.Second):
		t.Fatal("worker start not reached")
		return false
	}
}
