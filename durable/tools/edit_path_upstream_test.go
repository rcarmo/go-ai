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

// Source: pi v1.6.0 packages/durable/test/tools.test.ts:446-541.
func TestSourceV160EditPathSerialization(t *testing.T) {
	for _, tc := range []struct {
		name, first, second, target string
		initial, final              string
		edit, directoryLink         bool
		holdAfterCreate, otherFS    bool
	}{
		{name: "canonical-and-symlink-edits", first: "target.txt", second: "link.txt", target: "target.txt", initial: "alpha\nbeta\ngamma\n", final: "ALPHA\nBETA\ngamma\n", edit: true}, // 446
		{name: "same-filesystem-across-environments", first: "file.txt", second: "file.txt", target: "file.txt", initial: "alpha\nbeta\n", final: "ALPHA\nBETA\n", edit: true},           // 458
		{name: "missing-nested-file-through-directory-link", first: "link/new.txt", second: "real/new.txt", target: "real/new.txt", final: "second\n", directoryLink: true},              // 471
		{name: "missing-to-existing-literal-backslash", first: `a\b.txt`, second: `a\b.txt`, target: `a\b.txt`, final: "second\n", holdAfterCreate: true},                                // 486
		{name: "different-filesystem-identities", first: "file.txt", second: "file.txt", target: "file.txt", final: "first\n", otherFS: true},                                            // 520
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The deadline only bounds a defective run. All ordering uses barriers.
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			first := &editPathProbe{Env: LocalEnv(root)}
			second := &editPathProbe{Env: LocalEnv(root)}
			if tc.name == "canonical-and-symlink-edits" {
				second.Env = first.Env
				if err := os.Symlink("target.txt", filepath.Join(root, "link.txt")); err != nil {
					t.Fatal(err)
				}
			}
			if tc.directoryLink {
				if err := os.Mkdir(filepath.Join(root, "real"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("real", filepath.Join(root, "link")); err != nil {
					t.Fatal(err)
				}
			}
			if tc.otherFS {
				second.id = "test:other-file-system"
			}
			if tc.initial != "" {
				if err := first.Env.WriteFile(ctx, tc.target, []byte(tc.initial)); err != nil {
					t.Fatal(err)
				}
			}
			started, release, written, resolved := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce, resolvedOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			hold := func(ctx context.Context) error {
				close(started)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			first.afterWrite = func(ctx context.Context) error {
				if tc.holdAfterCreate {
					if err := hold(ctx); err != nil {
						return err
					}
				}
				close(written)
				return nil
			}
			if tc.edit {
				first.afterRead = hold
			} else if !tc.holdAfterCreate {
				first.beforeWrite = hold
			}
			second.afterCanonical = func(path string) {
				if path == filepath.Join(root, tc.second) {
					resolvedOnce.Do(func() { close(resolved) })
				}
			}
			checkOrdered := func(context.Context) error {
				select {
				case <-written:
					return nil
				default:
					return errors.New("second mutation entered before the first write settled")
				}
			}
			if !tc.otherFS {
				second.beforeWrite = checkOrdered
				second.afterRead = checkOrdered
			}
			firstTool, secondTool := Write(first), Write(second)
			firstArgs := durable.JSON{"path": tc.first, "content": "first\n"}
			secondArgs := durable.JSON{"path": tc.second, "content": "second\n"}
			if tc.edit {
				firstTool, secondTool = Edit(first), Edit(second)
				firstArgs, secondArgs = sourceEditArgs(tc.first, "alpha", "ALPHA"), sourceEditArgs(tc.second, "beta", "BETA")
			}
			firstDone := startPathTool(ctx, firstTool, firstArgs)
			awaitPathTool(t, ctx, started)
			// Inspect the admitted lock while the real host read/write is held.
			// This catches a wrong key or an unheld mutex without relying on a
			// blocked goroutine being scheduled within a time window.
			checkHeldPathToolKey(t, first.ID(), filepath.Join(root, tc.target))
			secondDone := startPathTool(ctx, secondTool, secondArgs)
			awaitPathTool(t, ctx, resolved)
			if tc.otherFS {
				if err := awaitPathTool(t, ctx, secondDone); err != nil {
					t.Fatal(err)
				}
				checkToolFile(t, ctx, second.Env, tc.target, "second\n")
			}
			unblock()
			if err := awaitPathTool(t, ctx, firstDone); err != nil {
				t.Fatal(err)
			}
			if !tc.otherFS {
				if err := awaitPathTool(t, ctx, secondDone); err != nil {
					t.Fatal(err)
				}
			}
			checkToolFile(t, ctx, first.Env, tc.target, tc.final)
		})
	}
	t.Run("edit-regular-symlink-target", func(t *testing.T) { // 536
		root := t.TempDir()
		env := LocalEnv(root)
		ctx := t.Context()
		if err := env.WriteFile(ctx, "target.txt", []byte("before\n")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("target.txt", filepath.Join(root, "link.txt")); err != nil {
			t.Fatal(err)
		}
		if out, err := Edit(env).Execute(ctx, sourceEditArgs("link.txt", "before", "after"), nil); err != nil || out.IsError {
			t.Fatal(out, err)
		}
		checkToolFile(t, ctx, env, "target.txt", "after\n")
	})
}

func sourceEditArgs(path, oldText, newText string) durable.JSON {
	return durable.JSON{"path": path, "edits": []any{map[string]any{"oldText": oldText, "newText": newText}}}
}

func startPathTool(ctx context.Context, reg durable.ToolRegistration, args durable.JSON) <-chan error {
	done := make(chan error, 1)
	go func() {
		out, err := reg.Execute(ctx, args, nil)
		if err == nil && out.IsError {
			err = fmt.Errorf("tool rejected mutation: %v", out.Diagnostics)
		}
		done <- err
	}()
	return done
}

func awaitPathTool[T any](t *testing.T, ctx context.Context, ch <-chan T) (value T) {
	t.Helper()
	select {
	case value = <-ch:
		return value
	case <-ctx.Done():
		t.Fatalf("file-mutation barrier did not complete: %v", ctx.Err())
		return value
	}
}

func checkHeldPathToolKey(t *testing.T, id, canonical string) {
	t.Helper()
	mutationLocks.Lock()
	lock := mutationLocks.m[id+"\x00"+canonical]
	mutationLocks.Unlock()
	if lock == nil {
		t.Errorf("admitted mutation did not hold the canonical file key")
	} else if lock.mu.TryLock() {
		lock.mu.Unlock()
		t.Error("admitted mutation had an unheld canonical file mutex")
	}
}

func checkToolFile(t *testing.T, ctx context.Context, env *Env, path, want string) {
	t.Helper()
	if got, err := env.ReadTextFile(ctx, path); err != nil || got != want {
		t.Errorf("file=%q expected=%q error=%v", got, want, err)
	}
}

type editPathProbe struct {
	*Env
	id                                 string
	afterRead, beforeWrite, afterWrite func(context.Context) error
	afterCanonical                     func(string)
}

func (p *editPathProbe) ID() string {
	if p.id != "" {
		return p.id
	}
	return p.Env.ID()
}

func (p *editPathProbe) CanonicalPath(ctx context.Context, path string) (string, error) {
	name, err := p.Env.CanonicalPath(ctx, path)
	if p.afterCanonical != nil {
		p.afterCanonical(path)
	}
	return name, err
}

func (p *editPathProbe) ReadTextFile(ctx context.Context, path string) (string, error) {
	text, err := p.Env.ReadTextFile(ctx, path)
	if err == nil && p.afterRead != nil {
		err = p.afterRead(ctx)
	}
	return text, err
}

func (p *editPathProbe) WriteFile(ctx context.Context, path string, data []byte) error {
	if p.beforeWrite != nil {
		if err := p.beforeWrite(ctx); err != nil {
			return err
		}
	}
	if err := p.Env.WriteFile(ctx, path, data); err != nil {
		return err
	}
	if p.afterWrite != nil {
		return p.afterWrite(ctx)
	}
	return nil
}
