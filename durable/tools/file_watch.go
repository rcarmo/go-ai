package tools

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rcarmo/go-ai/durable"
)

// The portable watcher explicitly reports polling mode. Snapshot coverage is
// established synchronously before Watch returns; callbacks are serial and no
// callback starts after Close. Changes undone between polls may be missed, as
// permitted by the reference polling contract. Symlink children are not followed.
type localWatcher struct {
	mu       sync.Mutex
	closed   bool
	stop     chan struct{}
	done     chan struct{}
	targets  []durable.WatchTarget
	callback func(durable.WatchChange)
	last     map[string]watchFingerprint
	env      *Env
}
type watchFingerprint struct {
	Mode           os.FileMode
	Size, Modified int64
	Content        [32]byte
	Identity       os.FileInfo
}

func (w *localWatcher) Mode() string { return "polling" }
func (w *localWatcher) Close(ctx context.Context) error {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.stop)
	}
	w.mu.Unlock()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (e *Env) Watch(ctx context.Context, targets []durable.WatchTarget, onChange func(durable.WatchChange)) (durable.FileWatcher, error) {
	if ctx == nil || onChange == nil {
		return nil, fileError("", os.ErrInvalid)
	}
	owned := make([]durable.WatchTarget, len(targets))
	for i, target := range targets {
		absolute, err := e.AbsolutePath(ctx, target.Path)
		if err != nil {
			return nil, err
		}
		target.Path = absolute
		target.ExcludeNames = append([]string(nil), target.ExcludeNames...)
		owned[i] = target
	}
	w := &localWatcher{env: e, targets: owned, callback: onChange, stop: make(chan struct{}), done: make(chan struct{})}
	baseline, err := w.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	w.last = baseline
	e.watchMu.Lock()
	if e.watchers == nil {
		e.watchers = map[*localWatcher]bool{}
	}
	e.watchers[w] = true
	e.watchMu.Unlock()
	go w.run(ctx)
	return w, nil
}
func sameWatchFingerprint(a, b watchFingerprint) bool {
	return a.Mode == b.Mode && a.Size == b.Size && a.Modified == b.Modified && a.Content == b.Content && os.SameFile(a.Identity, b.Identity)
}
func (w *localWatcher) reported(path string) string {
	for _, target := range w.targets {
		relative, err := filepath.Rel(target.Path, path)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return path
		}
	}
	for _, target := range w.targets {
		relative, err := filepath.Rel(path, target.Path)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return target.Path
		}
	}
	return path
}
func (w *localWatcher) snapshot(ctx context.Context) (map[string]watchFingerprint, error) {
	result := map[string]watchFingerprint{}
	directories := map[string]bool{}
	maximum := w.env.watchOptions.MaxDirectories
	if maximum <= 0 {
		maximum = 10000
	}
	for _, target := range w.targets {
		excluded := map[string]bool{}
		for _, name := range target.ExcludeNames {
			excluded[name] = true
		}
		// Ancestor identity detects moves without reporting unrelated siblings.
		for ancestor := filepath.Dir(target.Path); ; ancestor = filepath.Dir(ancestor) {
			if _, recorded := result[ancestor]; !recorded {
				if info, err := os.Lstat(ancestor); err == nil {
					result[ancestor] = watchFingerprint{Mode: info.Mode(), Identity: info}
				}
			}
			if filepath.Dir(ancestor) == ancestor {
				break
			}
		}
		var walk func(string, bool) error
		walk = func(path string, root bool) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			var info os.FileInfo
			var err error
			if root {
				info, err = os.Stat(path)
			} else {
				info, err = os.Lstat(path)
			}
			if errors.Is(err, os.ErrNotExist) || !root && errors.Is(err, os.ErrPermission) {
				return nil
			}
			if err != nil {
				return fileError(path, err)
			}
			fingerprint := watchFingerprint{Mode: info.Mode(), Size: info.Size(), Modified: info.ModTime().UnixNano(), Identity: info}
			// Membership maps witness visible children; metadata also changes for excluded children.
			if info.IsDir() {
				fingerprint.Size, fingerprint.Modified = 0, 0
			}
			// Like the pinned polling implementation, hash only recent small files.
			if info.Mode().IsRegular() && info.Size() <= 256<<10 && time.Since(info.ModTime()) < 5*time.Second {
				file, err := openRegularBinary(path, !root)
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				if err != nil {
					return fileError(path, err)
				}
				opened, statErr := file.Stat()
				if statErr != nil || !opened.Mode().IsRegular() {
					file.Close()
					return fileError(path, errors.Join(statErr, os.ErrInvalid))
				}
				hash := sha256.New()
				_, err = io.Copy(hash, io.LimitReader(file, (256<<10)+1))
				closeErr := file.Close()
				if err != nil || closeErr != nil {
					return fileError(path, errors.Join(err, closeErr))
				}
				copy(fingerprint.Content[:], hash.Sum(nil))
			}
			// An explicit target follows its own link and wins over a listing
			// by any overlapping target, independent of target order.
			if _, recorded := result[path]; root || !recorded {
				result[path] = fingerprint
			}
			if !info.IsDir() || !root && !target.Recursive {
				return nil
			}
			directories[path] = true
			if len(directories) > maximum {
				return fileError(path, os.ErrInvalid)
			}
			entries, err := os.ReadDir(path)
			if errors.Is(err, os.ErrNotExist) || !root && errors.Is(err, os.ErrPermission) {
				return nil
			}
			if err != nil {
				return fileError(path, err)
			}
			for _, entry := range entries {
				if excluded[entry.Name()] || target.ExcludeHidden && strings.HasPrefix(entry.Name(), ".") {
					continue
				}
				if err := walk(filepath.Join(path, entry.Name()), false); err != nil {
					return err
				}
			}
			return nil
		}
		if err := walk(target.Path, true); err != nil {
			return nil, err
		}
	}
	return result, nil
}
func (w *localWatcher) run(ctx context.Context) {
	interval := w.env.watchOptions.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer func() {
		w.mu.Lock()
		if !w.closed {
			w.closed = true
			close(w.stop)
		}
		w.mu.Unlock()
		w.env.watchMu.Lock()
		delete(w.env.watchers, w)
		w.env.watchMu.Unlock()
		close(w.done)
	}()
	for {
		select {
		case <-w.stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		next, err := w.snapshot(ctx)
		change := durable.WatchChange{}
		if err != nil {
			var failure *durable.FileError
			if !errors.As(err, &failure) {
				failure = &durable.FileError{Code: "unknown", Cause: err}
			}
			change.Error = failure
		} else {
			paths := map[string]bool{}
			for path, old := range w.last {
				if value, ok := next[path]; !ok || !sameWatchFingerprint(value, old) {
					paths[w.reported(path)] = true
				}
			}
			for path := range next {
				if _, ok := w.last[path]; !ok {
					paths[w.reported(path)] = true
				}
			}
			for path := range paths {
				change.Paths = append(change.Paths, path)
			}
			sort.Strings(change.Paths)
			w.last = next
		}
		if len(change.Paths) == 0 && change.Error == nil {
			continue
		}
		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			return
		}
		callback := w.callback
		w.mu.Unlock()
		// A host callback panic must not terminate watching or the process.
		func() { defer func() { _ = recover() }(); callback(change) }()
		if change.Error != nil {
			return
		}
	}
}
