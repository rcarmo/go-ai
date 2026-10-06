//go:build unix

package tools

import (
	"context"
	"errors"
	"fmt"
	"github.com/rcarmo/go-ai/durable"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

func shellFailure(code, spill string, err error) error {
	return &durable.ExecutionError{Code: code, SpillPath: spill, Cause: err}
}
func callShellOutput(ctx context.Context, callback func(context.Context, string) error, text string) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("shell callback panic")
		}
	}()
	return callback(ctx, text)
}

type shellWriter struct {
	mu              sync.Mutex
	ctx             context.Context
	callback        func(context.Context, string) error
	spillOptions    *durable.ShellSpillOptions
	env             *Env
	prefix          []byte
	file            *os.File
	path            string
	total, newlines int
	last            byte
	failure         error
	failureCode     string
	cancel          context.CancelFunc
}

func (w *shellWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failure != nil {
		return 0, w.failure
	}
	w.total += len(data)
	w.newlines += strings.Count(string(data), "\n")
	if len(data) > 0 {
		w.last = data[len(data)-1]
	}
	lines := w.newlines
	if w.total > 0 && w.last != '\n' {
		lines++
	}
	if w.file != nil {
		if _, err := w.file.Write(data); err != nil {
			w.failure = err
			w.failureCode = "unknown"
			w.cancel()
			return 0, err
		}
	} else if w.spillOptions != nil {
		w.prefix = append(w.prefix, data...)
		if w.total > w.spillOptions.AfterBytes || lines > w.spillOptions.AfterLines {
			path, err := w.env.CreateTempFile(w.ctx, "shell-", ".txt")
			if err != nil {
				w.failure = err
				w.failureCode = "unknown"
				w.cancel()
				return 0, err
			}
			file, err := os.OpenFile(path, os.O_WRONLY, 0600)
			if err != nil {
				os.Remove(path)
				w.failure = err
				w.failureCode = "unknown"
				w.cancel()
				return 0, err
			}
			w.file, w.path = file, path
			if _, err = file.Write(w.prefix); err != nil {
				w.failure = err
				w.failureCode = "unknown"
				w.cancel()
				return 0, err
			}
			w.prefix = nil
		}
	}
	return len(data), nil
}

type shellCallbackWriter struct{ writer *shellWriter }

func (w shellCallbackWriter) Write(data []byte) (int, error) {
	w.writer.mu.Lock()
	defer w.writer.mu.Unlock()
	if w.writer.failure != nil {
		return 0, w.writer.failure
	}
	if w.writer.callback != nil {
		if err := callShellOutput(w.writer.ctx, w.writer.callback, string(data)); err != nil {
			w.writer.failure = err
			w.writer.failureCode = "callback_error"
			w.writer.cancel()
			return 0, err
		}
	}
	return len(data), nil
}

type shellPipe struct {
	raw     *shellWriter
	decoded *transform.Writer
}

func (p *shellPipe) Write(data []byte) (int, error) {
	if _, err := p.raw.Write(data); err != nil {
		return 0, err
	}
	return p.decoded.Write(data)
}
func (e *Env) Exec(ctx context.Context, command string, options durable.ShellExecOptions) (durable.ShellExecResult, error) {
	if ctx == nil {
		return durable.ShellExecResult{}, shellFailure("unknown", "", os.ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return durable.ShellExecResult{}, shellFailure("aborted", "", err)
	}
	var duration time.Duration
	if options.Timeout != nil {
		args := durable.JSON{"timeout": *options.Timeout}
		value, _, err := bashTimeoutArg(args, "timeout", 0)
		if err != nil {
			return durable.ShellExecResult{}, shellFailure("timeout", "", err)
		}
		duration = value
	}
	if options.Spill != nil && (options.Spill.AfterBytes < 0 || options.Spill.AfterLines < 0) {
		return durable.ShellExecResult{}, shellFailure("unknown", "", os.ErrInvalid)
	}
	cwd := options.Cwd
	if cwd == "" {
		cwd = e.cwd
	}
	absolute, err := e.AbsolutePath(ctx, cwd)
	if err != nil {
		return durable.ShellExecResult{}, shellFailure("spawn_error", "", err)
	}
	shell, err := bashShellPath()
	if err != nil {
		return durable.ShellExecResult{}, shellFailure("shell_unavailable", "", err)
	}
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	if duration > 0 {
		var stop context.CancelFunc
		run, stop = context.WithTimeout(run, duration)
		defer stop()
	}
	cmd := exec.Command(shell, "-c", command)
	cmd.Dir = absolute
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	values := map[string]string{}
	if options.InheritEnv == nil || *options.InheritEnv {
		for _, pair := range os.Environ() {
			key, value, ok := strings.Cut(pair, "=")
			if ok {
				values[key] = value
			}
		}
	}
	for key, value := range options.Env {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		cmd.Env = append(cmd.Env, key+"="+values[key])
	}
	if cmd.Env == nil {
		cmd.Env = []string{}
	}
	writer := &shellWriter{ctx: ctx, callback: options.OnOutput, spillOptions: options.Spill, env: e, cancel: cancel}
	stdout := &shellPipe{writer, transform.NewWriter(shellCallbackWriter{writer}, unicode.UTF8BOM.NewDecoder())}
	stderr := &shellPipe{writer, transform.NewWriter(shellCallbackWriter{writer}, unicode.UTF8BOM.NewDecoder())}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		return durable.ShellExecResult{}, shellFailure("spawn_error", "", err)
	}
	e.processMu.Lock()
	if e.processes == nil {
		e.processes = map[int]struct{}{}
	}
	e.processes[cmd.Process.Pid] = struct{}{}
	e.processMu.Unlock()
	done, joined := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case <-done:
		case <-run.Done():
			killProcessGroup(cmd.Process.Pid)
		}
	}()
	waitErr := cmd.Wait()
	_ = stdout.decoded.Close()
	_ = stderr.decoded.Close()
	close(done)
	<-joined
	killProcessGroup(cmd.Process.Pid)
	e.processMu.Lock()
	delete(e.processes, cmd.Process.Pid)
	e.processMu.Unlock()
	if writer.file != nil {
		syncErr := writer.file.Sync()
		closeErr := writer.file.Close()
		if err := errors.Join(syncErr, closeErr); err != nil && writer.failure == nil {
			writer.failure = err
			writer.failureCode = "unknown"
		}
	}
	result := durable.ShellExecResult{SpillPath: writer.path}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
		if result.ExitCode < 0 {
			if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				result.ExitCode = 128 + int(status.Signal())
			}
		}
	}
	if writer.failure != nil {
		return result, shellFailure(writer.failureCode, writer.path, writer.failure)
	}
	if run.Err() != nil {
		code := "aborted"
		if errors.Is(run.Err(), context.DeadlineExceeded) {
			code = "timeout"
		}
		return result, shellFailure(code, writer.path, run.Err())
	}
	if waitErr != nil {
		var exited *exec.ExitError
		if !errors.As(waitErr, &exited) {
			return result, shellFailure("unknown", writer.path, fmt.Errorf("shell wait: %w", waitErr))
		}
	}
	return result, nil
}
func (e *Env) cleanupProcesses() {
	e.processMu.Lock()
	defer e.processMu.Unlock()
	for pid := range e.processes {
		killProcessGroup(pid)
	}
	clear(e.processes)
}

var _ durable.Shell = (*Env)(nil)
var _ io.Writer = (*shellWriter)(nil)
