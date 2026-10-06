package tools

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/durable"
)

type heldMutationFS struct {
	*memoryToolFS
	mu                                        sync.Mutex
	writesStarted                             int
	firstStarted, releaseFirst, secondStarted chan struct{}
}

func (fs *heldMutationFS) WriteFile(ctx context.Context, path string, data []byte) error {
	fs.mu.Lock()
	fs.writesStarted++
	count := fs.writesStarted
	fs.mu.Unlock()
	if count == 1 {
		close(fs.firstStarted)
		<-fs.releaseFirst
	} else {
		close(fs.secondStarted)
	}
	// Model an admitted host write that must settle despite caller cancellation.
	return fs.memoryToolFS.WriteFile(context.Background(), path, data)
}

func TestFileMutationQueueKeepsLockUntilAbortedHostWriteSettles(t *testing.T) {
	for _, operation := range []string{"write", "edit"} {
		t.Run(operation, func(t *testing.T) {
			fs := &heldMutationFS{memoryToolFS: &memoryToolFS{id: "held-" + operation, files: map[string][]byte{"/virtual/file": []byte("alpha\nbeta\n")}}, firstStarted: make(chan struct{}), releaseFirst: make(chan struct{}), secondStarted: make(chan struct{})}
			defer func() {
				select {
				case <-fs.releaseFirst:
				default:
					close(fs.releaseFirst)
				}
			}()
			caller, cancel := context.WithCancel(context.Background())
			defer cancel()
			tool := Write(fs)
			firstArgs := durable.JSON{"path": "file", "content": "first\n"}
			secondArgs := durable.JSON{"path": "file", "content": "second\n"}
			if operation == "edit" {
				tool = Edit(fs)
				firstArgs = durable.JSON{"path": "file", "edits": []any{map[string]any{"oldText": "alpha", "newText": "ALPHA"}}}
				secondArgs = durable.JSON{"path": "file", "edits": []any{map[string]any{"oldText": "beta", "newText": "BETA"}}}
			}
			first, second := make(chan error, 1), make(chan error, 1)
			go func() { _, err := tool.Execute(caller, firstArgs, nil); first <- err }()
			select {
			case <-fs.firstStarted:
			case <-time.After(3 * time.Second):
				t.Fatal("first host write absent")
			}
			cancel()
			go func() { _, err := tool.Execute(context.Background(), secondArgs, nil); second <- err }()
			// Observe the actual same-file lock claim, not a goroutine launch/timer.
			lockKey := fs.ID() + "\x00/virtual/file"
			deadline := time.After(3 * time.Second)
			for {
				mutationLocks.Lock()
				lock := mutationLocks.m[lockKey]
				queued := lock != nil && lock.refs == 2
				mutationLocks.Unlock()
				if queued {
					break
				}
				select {
				case <-deadline:
					t.Fatal("second mutation did not acquire queue claim")
				default:
				}
			}
			select {
			case <-fs.secondStarted:
				t.Fatal("second host write bypassed unsettled first")
			default:
			}
			close(fs.releaseFirst)
			select {
			case err := <-first:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("first did not settle")
			}
			select {
			case err := <-second:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("second did not settle")
			}
			want := "second\n"
			if operation == "edit" {
				want = "ALPHA\nBETA\n"
			}
			if string(fs.files["/virtual/file"]) != want {
				t.Fatal("ordered mutation lost", string(fs.files["/virtual/file"]))
			}
		})
	}
}
