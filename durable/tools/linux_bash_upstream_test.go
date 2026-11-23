//go:build linux

package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rcarmo/go-ai/durable"
)

// Source: pi v1.6.0 packages/durable/test/tools.test.ts:654-751. Standalone
// Go tool results expose Content/Details/errors instead of JS structuredOutput.
func TestSourceV160LinuxBash(t *testing.T) {
	newEnv := func(t *testing.T) *observedBashEnv {
		t.Helper()
		return &observedBashEnv{Env: LocalEnv(t.TempDir())}
	}
	t.Run("stdout-and-stderr", func(t *testing.T) { // 654
		env := newEnv(t)
		got, err := Bash(env).Execute(t.Context(), durable.JSON{"command": "printf out; printf err >&2"}, nil)
		streamed := env.outputText()
		_, truncated := got.Details["truncated"]
		if err != nil || !strings.Contains(streamed, "out") || !strings.Contains(streamed, "err") || got.Content != streamed || got.Details["exitCode"] != 0 || truncated {
			t.Errorf("streamed=%q result=%+v error=%v", streamed, got, err)
		}
	})
	t.Run("nonzero-and-timeout", func(t *testing.T) { // 662
		env := newEnv(t)
		tool := Bash(env)
		got, err := tool.Execute(t.Context(), durable.JSON{"command": "printf failed; exit 7"}, nil)
		_, truncated := got.Details["truncated"]
		if err == nil || err.Error() != "Command exited with code 7" || env.outputText() != "failed" || got.Content != "failed" || got.Details["exitCode"] != 7 || truncated {
			t.Errorf("streamed=%q result=%+v error=%v", env.outputText(), got, err)
		}
		_, err = tool.Execute(t.Context(), durable.JSON{"command": "sleep 2", "timeout": 0.01}, nil)
		if err == nil || err.Error() != "Command timed out after 0.01 seconds" || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("timeout error=%v", err)
		}
	})
	t.Run("timeout-spill", func(t *testing.T) { // 676
		env := newEnv(t)
		var expected strings.Builder
		for n := 1; n <= MaxReadLines+1; n++ {
			fmt.Fprintf(&expected, "line-%d\n", n)
		}
		path := filepath.Join(env.Cwd(), "timeout-fixture.log")
		env.exec = func(ctx context.Context, _ string, options durable.ShellExecOptions) (durable.ShellExecResult, error) {
			if err := env.WriteFile(ctx, path, []byte(expected.String())); err != nil {
				return durable.ShellExecResult{}, err
			}
			if err := options.OnOutputInfo(ctx, expected.String(), durable.ShellOutputInfo{Stream: "stdout"}); err != nil {
				return durable.ShellExecResult{}, err
			}
			return durable.ShellExecResult{}, &durable.ExecutionError{Code: "timeout", SpillPath: path}
		}
		got, err := Bash(env).Execute(t.Context(), durable.JSON{"command": "emit-output-then-time-out", "timeout": 0.05}, nil)
		if err == nil || err.Error() != "Command timed out after 0.05 seconds" {
			t.Errorf("timeout error=%v", err)
		}
		checkSourceBashSpill(t, env, got, expected.String())
	})
	t.Run("prepare-and-explicit-environment", func(t *testing.T) { // 691, POSIX branch
		t.Setenv("PI_BASH_PREPARE_INHERITED", "inherited")
		env := newEnv(t)
		workspace := filepath.Join(env.Cwd(), "workspace")
		if err := os.Mkdir(workspace, 0700); err != nil {
			t.Fatal(err)
		}
		physical, err := filepath.EvalSymlinks(workspace)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		prepared := 0
		tool := Bash(env, BashOptions{CommandPrefix: "prefix=ready", Prepare: func(callCtx context.Context, execution *BashExecution, _ *durable.ToolAPI) error {
			prepared++
			if callCtx != ctx || execution.Command != "prefix=ready\n:" {
				t.Errorf("unexpected prepare input: context=%v command=%q", callCtx, execution.Command)
			}
			execution.Cwd = workspace
			execution.Env = map[string]string{"PI_BASH_PREPARE_EXPLICIT": "explicit"}
			execution.InheritEnv = false
			execution.Command += "\n: > prepared-cwd\nprintf '%s:%s:%s:%s' \"$prefix\" \"${PI_BASH_PREPARE_INHERITED-}\" \"$PI_BASH_PREPARE_EXPLICIT\" \"$PWD\""
			return nil
		}})
		_, err = tool.Execute(ctx, durable.JSON{"command": ":"}, nil)
		marker, markerErr := os.Stat(filepath.Join(workspace, "prepared-cwd"))
		if err != nil || prepared != 1 || env.outputText() != "ready::explicit:"+physical || markerErr != nil || !marker.Mode().IsRegular() {
			t.Errorf("prepare calls=%d output=%q marker=%v error=%v", prepared, env.outputText(), markerErr, err)
		}
	})
	t.Run("command-prefix", func(t *testing.T) { // 725
		env := newEnv(t)
		_, err := Bash(env, BashOptions{CommandPrefix: "value=hello"}).Execute(t.Context(), durable.JSON{"command": "printf $value"}, nil)
		if err != nil || env.outputText() != "hello" {
			t.Errorf("output=%q error=%v", env.outputText(), err)
		}
	})
	t.Run("full-spill", func(t *testing.T) { // 734
		env := newEnv(t)
		got, err := Bash(env).Execute(t.Context(), durable.JSON{"command": "i=1; while [ $i -le 3000 ]; do echo line-$i; i=$((i + 1)); done"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		var expected strings.Builder
		for i := 1; i <= 3000; i++ {
			fmt.Fprintf(&expected, "line-%d\n", i)
		}
		if streamed := env.outputText(); streamed != expected.String() {
			t.Errorf("stream bytes=%d; want %d complete bytes", len(streamed), expected.Len())
		}
		checkSourceBashSpill(t, env, got, expected.String())
	})
	t.Run("no-small-spill", func(t *testing.T) { // 748
		env := newEnv(t)
		got, err := Bash(env).Execute(t.Context(), durable.JSON{"command": "printf small"}, nil)
		if err != nil || len(got.Diagnostics) != 0 || got.Details["fullOutputPath"] != nil {
			t.Errorf("result=%+v error=%v", got, err)
		}
	})
}

func checkSourceBashSpill(t *testing.T, env *observedBashEnv, got durable.ToolResult, expected string) {
	t.Helper()
	if len(got.Diagnostics) != 1 || !strings.HasPrefix(got.Diagnostics[0].Message, "Full output: ") {
		t.Fatalf("no spill notice: %+v", got.Diagnostics)
	}
	path := strings.TrimPrefix(got.Diagnostics[0].Message, "Full output: ")
	data, err := env.ReadTextFile(t.Context(), path)
	if err != nil || data != expected {
		t.Errorf("spill bytes=%d; want %d complete bytes; error=%v", len(data), len(expected), err)
	}
}

type observedBashEnv struct {
	*Env
	mu     sync.Mutex
	output strings.Builder
	exec   func(context.Context, string, durable.ShellExecOptions) (durable.ShellExecResult, error)
}

func (e *observedBashEnv) outputText() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.output.String()
}

func (e *observedBashEnv) Exec(ctx context.Context, command string, options durable.ShellExecOptions) (durable.ShellExecResult, error) {
	next := options.OnOutputInfo
	options.OnOutputInfo = func(ctx context.Context, text string, info durable.ShellOutputInfo) error {
		e.mu.Lock()
		e.output.WriteString(text)
		e.mu.Unlock()
		return next(ctx, text, info)
	}
	if e.exec != nil {
		return e.exec(ctx, command, options)
	}
	return e.Env.Exec(ctx, command, options)
}
