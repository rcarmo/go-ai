package tools

import (
	"context"
	"errors"
	"github.com/rcarmo/go-ai/durable"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Env configures local filesystem-backed durable tools.
type WatchOptions struct {
	PollInterval   time.Duration
	MaxDirectories int
}

type Env struct {
	watchOptions WatchOptions
	watchMu      sync.Mutex
	watchers     map[*localWatcher]bool
	cwd          string
	processMu    sync.Mutex
	processes    map[int]struct{}
}

// LocalEnv returns a local filesystem environment rooted at cwd for relative paths.
// Absolute paths are preserved.
func LocalEnv(cwd string) *Env {
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	return &Env{cwd: filepath.Clean(cwd)}
}

func LocalEnvWithWatchOptions(cwd string, options WatchOptions) *Env {
	env := LocalEnv(cwd)
	env.watchOptions = options
	return env
}
func (e *Env) Cwd() string {
	if e == nil {
		return ""
	}
	return e.cwd
}

func resolveToolEnv(ctx context.Context, fallback durable.FileSystem, api *durable.ToolAPI) (durable.FileSystem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if api == nil {
		return fallback, nil
	}
	resolved, err := api.Environment(ctx)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		return fallback, nil
	}
	env, ok := resolved.(durable.FileSystem)
	if !ok {
		return nil, errors.New("coding tools require a filesystem environment")
	}
	return env, nil
}

func (e *Env) resolve(path string) (string, error) {
	if e == nil {
		return "", errors.New("nil env")
	}
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is required")
	}
	if path == "~" || strings.HasPrefix(path, "~/") || runtime.GOOS == "windows" && strings.HasPrefix(path, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	} else if strings.HasPrefix(path, "file://") {
		parsed, err := url.Parse(path)
		// Node fileURLToPath rejects non-local hosts and encoded separators.
		if err == nil && (parsed.Host == "" || parsed.Host == "localhost") && !strings.Contains(strings.ToLower(parsed.EscapedPath()), "%2f") && !strings.Contains(strings.ToLower(parsed.EscapedPath()), "%5c") && filepath.IsAbs(parsed.Path) {
			path = filepath.FromSlash(parsed.Path)
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	if e.cwd == "" {
		return filepath.Clean(path), nil
	}
	return filepath.Clean(filepath.Join(e.cwd, path)), nil
}

type fileLock struct {
	mu   sync.Mutex
	refs int
}

var mutationLocks = struct {
	sync.Mutex
	m map[string]*fileLock
}{m: map[string]*fileLock{}}

func withSerializedMutation(path string, fn func() error) error {
	key := canonicalMutationKey(path)
	lock := acquireMutationLock(key)
	lock.mu.Lock()
	defer func() {
		lock.mu.Unlock()
		releaseMutationLock(key)
	}()
	return fn()
}

func acquireMutationLock(key string) *fileLock {
	mutationLocks.Lock()
	defer mutationLocks.Unlock()
	lock := mutationLocks.m[key]
	if lock == nil {
		lock = &fileLock{}
		mutationLocks.m[key] = lock
	}
	lock.refs++
	return lock
}

func releaseMutationLock(key string) {
	mutationLocks.Lock()
	defer mutationLocks.Unlock()
	lock := mutationLocks.m[key]
	if lock == nil {
		return
	}
	lock.refs--
	if lock.refs <= 0 {
		delete(mutationLocks.m, key)
	}
}

func canonicalMutationKey(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	name := filepath.Base(path)
	if resolved, err := filepath.EvalSymlinks(parent); err == nil {
		return filepath.Join(filepath.Clean(resolved), name)
	}
	base := canonicalMutationKey(parent)
	if base == parent {
		return filepath.Join(base, name)
	}
	return filepath.Join(base, name)
}
