package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// The process bridge needs a receipt after call publication to admit its next
// wire frame. It must not wait for the host callback: replay can supply the
// recorded delivery without invoking that callback again.
type journalWorkerRemoteV160 struct {
	*CodemodeWorkerRemote
	observed     chan<- int
	observeStart func(CodemodeStart)
	guestCode    func(int32, string) string
	guestStore   func(int32, JSON) JSON
	starts       atomic.Int32
	deliveries   atomic.Int32
}

func (r *journalWorkerRemoteV160) Execute(ctx context.Context, code string, start CodemodeStart, exchange func(context.Context, CodemodeEvent) (CodemodeRemoteReply, error), receive func(context.Context) (CodemodeDelivery, error)) error {
	run := r.starts.Add(1)
	if r.guestCode != nil {
		code = r.guestCode(run, code)
	}
	if r.observeStart != nil {
		r.observeStart(start)
	}
	if r.guestStore != nil {
		start.Store = r.guestStore(run, start.Store)
	}
	return r.CodemodeWorkerRemote.Execute(ctx, code, start, func(ctx context.Context, event CodemodeEvent) (CodemodeRemoteReply, error) {
		reply, err := exchange(ctx, event)
		if event.Call == nil || err != nil || reply.Stop {
			return reply, err
		}
		select {
		case r.observed <- event.Call.ID:
			return reply, nil
		case <-ctx.Done():
			return CodemodeRemoteReply{Stop: true}, ctx.Err()
		}
	}, func(ctx context.Context) (CodemodeDelivery, error) {
		delivery, err := receive(ctx)
		if err == nil {
			r.deliveries.Add(1)
		}
		return delivery, err
	})
}

func newJournalWorkerSandboxV160(t *testing.T) (*RemoteCodemodeSandbox, *journalWorkerRemoteV160) {
	t.Helper()
	root := officialWorkerPackageRootV160(t)
	observed := make(chan int)
	remote := &journalWorkerRemoteV160{
		CodemodeWorkerRemote: &CodemodeWorkerRemote{
			Transport:   officialWorkerTransport120{root: root, observed: observed},
			Fingerprint: officialWorkerFingerprintV160,
		},
		observed: observed,
	}
	return &RemoteCodemodeSandbox{Remote: remote, Identity: officialWorkerFingerprintV160}, remote
}

// codemode.test.ts:663–671. The exact original script runs in the published
// portable QuickJS guest both before the SQLite close and after reopen. The
// adapter forwards real guest frames, never scripted or recorded test replies.
func TestSourceV160CodemodePublishedWorkerUndefinedJournalReadback(t *testing.T) {
	sandbox, remote := newJournalWorkerSandboxV160(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	testV160CodemodeJournalWithSandbox(t, ctx, sandbox, v160UndefinedJournalScript, "nothing", "[true,true]", CodemodeUndefined)
	if got := remote.starts.Load(); got != 2 {
		t.Fatalf("published guest executions = %d; want 2", got)
	}
}
