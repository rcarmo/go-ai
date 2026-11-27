package durable

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCodemodeWorkerAdmissionAndExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	sandbox, err := NewQuickJSSandbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if err := sandbox.Close(closeCtx); err != nil {
			t.Error(err)
		}
	})
	run := func(t *testing.T, code string) CodemodeResult {
		t.Helper()
		runCtx, stop := context.WithTimeout(ctx, 4*time.Second)
		defer stop()
		exec, err := sandbox.Start(runCtx, code, CodemodeStart{})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := exec.Close(); err != nil {
				t.Error(err)
			}
		}()
		event, err := exec.Next(runCtx)
		if err != nil || event.Result == nil || event.Call != nil || event.Drain != nil {
			t.Fatalf("terminal Codemode event=%+v, err=%v", event, err)
		}
		return *event.Result
	}
	t.Run("admission-denied", func(t *testing.T) {
		acquired, started := 0, 0
		sandbox.AcquireWorker = func() (func(), error) {
			acquired++
			return nil, errors.New("private capacity diagnostic")
		}
		sandbox.OnWorkerStarted = func(<-chan struct{}) error { started++; return nil }
		got := run(t, `while (true) {}`)
		if got.HasValue || got.Error != "sandbox: QuickJS worker capacity unavailable" {
			t.Errorf("admission refusal=%+v", got)
		}
		if acquired != 1 || started != 0 {
			t.Errorf("admission attempts=%d, started=%d; want 1 and 0", acquired, started)
		}
	})
	t.Run("start-hook-failure", func(t *testing.T) {
		var exited <-chan struct{}
		var released int
		releaseBeforeExit := false
		sandbox.AcquireWorker = func() (func(), error) {
			return func() {
				released++
				select {
				case <-exited:
				default:
					releaseBeforeExit = true
				}
			}, nil
		}
		sandbox.OnWorkerStarted = func(done <-chan struct{}) error {
			exited = done
			return errors.New("private lifetime diagnostic")
		}
		got := run(t, `while (true) {}`)
		if got.HasValue || got.Error != "sandbox: QuickJS worker-start callback failed" {
			t.Errorf("start hook failure=%+v", got)
		}
		if exited == nil {
			t.Fatal("start hook was not handed the actual worker exit signal")
		}
		select {
		case <-exited:
		default:
			t.Error("worker did not exit")
		}
		if released != 1 || releaseBeforeExit {
			t.Errorf("capacity release count=%d, before exit=%v; want 1 after actual exit", released, releaseBeforeExit)
		}
	})
}
