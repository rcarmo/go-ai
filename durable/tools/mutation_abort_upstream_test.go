//go:build linux

package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/durable"
)

// TestSourceV160DelayedAbortMutationQueue maps tools.test.ts:331 and :423.
func TestSourceV160DelayedAbortMutationQueue(t *testing.T) {
	for _, operation := range []string{"write", "edit"} {
		t.Run(operation, func(t *testing.T) {
			ctx, timeout := context.WithTimeout(t.Context(), 5*time.Second)
			defer timeout()
			caller, cancel := context.WithCancel(ctx)
			defer cancel()
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			const path = "file.txt"
			fs := &abortMutationProbe{
				Env: LocalEnv(root), fixtureContext: ctx, firstContent: "first\n",
				firstEntered: make(chan struct{}), firstSettled: make(chan struct{}), releaseFirst: make(chan struct{}),
				secondKeyRequested: make(chan struct{}), secondEntered: make(chan struct{}), releaseSecond: make(chan struct{}),
			}
			releaseFirst := sync.OnceFunc(func() { close(fs.releaseFirst) })
			releaseSecond := sync.OnceFunc(func() { close(fs.releaseSecond) })
			var calls sync.WaitGroup
			defer func() {
				releaseFirst()
				releaseSecond()
				calls.Wait()
			}()
			tool := Write(fs)
			firstArgs := durable.JSON{"path": path, "content": "first\n"}
			secondArgs := durable.JSON{"path": path, "content": "second\n"}
			want := "second\n"
			if operation == "edit" {
				if err := fs.Env.WriteFile(ctx, path, []byte("alpha\nbeta\n")); err != nil {
					t.Fatal(err)
				}
				fs.firstContent = "ALPHA\nbeta\n"
				fs.admittedEdit = true
				tool = Edit(fs)
				firstArgs = sourceEditArgs(path, "alpha", "ALPHA")
				secondArgs = sourceEditArgs(path, "beta", "BETA")
				want = "ALPHA\nBETA\n"
			}

			start := func(ctx context.Context, args durable.JSON) <-chan error {
				done := make(chan error, 1)
				calls.Go(func() {
					out, err := tool.Execute(ctx, args, nil)
					if err == nil && out.IsError {
						err = fmt.Errorf("tool rejected mutation: %v", out.Diagnostics)
					}
					done <- err
				})
				return done
			}
			first := start(caller, firstArgs)
			awaitPathTool(t, ctx, fs.firstEntered)
			cancel()
			canonical := filepath.Join(root, path)
			checkHeldPathToolKey(t, fs.Env.ID(), canonical)
			second := start(ctx, secondArgs)
			awaitPathTool(t, ctx, fs.secondKeyRequested)
			checkHeldPathToolKey(t, fs.Env.ID(), canonical)
			select {
			case <-fs.secondEntered:
				t.Error("second mutation reached the host before the first settled")
			default:
			}

			releaseFirst()
			if err := awaitPathTool(t, ctx, first); !errors.Is(err, context.Canceled) {
				t.Errorf("first mutation error = %v, want cancellation", err)
			} else if operation == "edit" && err.Error() != "Operation aborted" {
				t.Errorf("aborted edit error = %q, want %q", err, "Operation aborted")
			}
			select {
			case <-fs.firstSettled:
			default:
				t.Error("aborted mutation returned before its host write settled")
			}
			awaitPathTool(t, ctx, fs.secondEntered)
			if fs.secondBeforeSettle {
				t.Error("second host write entered while the first was still active")
			}
			// Pause the second write to inspect the actual effect of the first.
			if operation == "edit" {
				checkToolFile(t, ctx, fs.Env, path, "ALPHA\nbeta\n")
			} else if _, err := fs.Env.ReadTextFile(ctx, path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("aborted write created the file before the second write: %v", err)
			}
			releaseSecond()
			if err := awaitPathTool(t, ctx, second); err != nil {
				t.Fatal(err)
			}
			checkToolFile(t, ctx, fs.Env, path, want)
		})
	}
}

type abortMutationProbe struct {
	*Env
	fixtureContext                           context.Context
	firstContent                             string
	admittedEdit, secondBeforeSettle         bool
	firstEntered, firstSettled, releaseFirst chan struct{}
	secondKeyRequested                       chan struct{}
	secondEntered, releaseSecond             chan struct{}
}

func (fs *abortMutationProbe) ID() string {
	select {
	case <-fs.firstEntered:
		// The second tool has resolved the path and reached lock-key construction.
		close(fs.secondKeyRequested)
	default:
	}
	return fs.Env.ID()
}

func (fs *abortMutationProbe) WriteFile(ctx context.Context, path string, data []byte) error {
	if string(data) == fs.firstContent {
		close(fs.firstEntered)
		defer close(fs.firstSettled)
		<-fs.releaseFirst
		if fs.admittedEdit {
			// As in the original edit fixture, model a host write that commits
			// after the caller aborts. The ordinary write keeps its canceled ctx.
			ctx = fs.fixtureContext
		}
	} else {
		select {
		case <-fs.firstSettled:
		default:
			fs.secondBeforeSettle = true
		}
		close(fs.secondEntered)
		<-fs.releaseSecond
	}
	return fs.Env.WriteFile(ctx, path, data)
}
