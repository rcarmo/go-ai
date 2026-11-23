//go:build unix

package tools

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rcarmo/go-ai/durable"
)

type powershellEnv104 struct {
	*Env
	calls   [][]string
	failure string
	options durable.ShellExecOptions
}

func (e *powershellEnv104) ExecArgs(ctx context.Context, args []string, options durable.ShellExecOptions) (durable.ShellExecResult, error) {
	e.calls = append(e.calls, append([]string(nil), args...))
	e.options = options
	if len(e.calls) == 1 {
		return durable.ShellExecResult{}, &durable.ExecutionError{Code: e.failure, Cause: errors.New("injected failure")}
	}
	if err := options.OnOutputInfo(ctx, "output", durable.ShellOutputInfo{Stream: "stdout"}); err != nil {
		return durable.ShellExecResult{}, err
	}
	return durable.ShellExecResult{}, nil
}
func TestPowerShell104DirectArgvPreparationFallbackAndFailures(t *testing.T) {
	for _, failure := range []string{"spawn_error", "timeout", "callback_error"} {
		env := &powershellEnv104{Env: LocalEnv(t.TempDir()), failure: failure}
		prepared := false
		tool := PowerShell(env, PowerShellOptions{CommandPrefix: "prefix", Prepare: func(_ context.Context, e *BashExecution, _ *durable.ToolAPI) error {
			prepared = true
			e.Command += "\nprepared"
			e.Env["KEY"] = "value"
			e.InheritEnv = false
			return nil
		}})
		result, err := tool.Execute(context.Background(), durable.JSON{"command": "Write-Output 'literal;argument'", "timeout": 1.0}, nil)
		if !prepared {
			t.Fatal("prepare not called")
		}
		if failure != "spawn_error" {
			if err == nil || len(env.calls) != 1 {
				t.Fatal("non-spawn failure retried", failure, err, env.calls)
			}
			continue
		}
		if err != nil || result.Content != "output" || len(env.calls) != 2 {
			t.Fatal(result, err, env.calls)
		}
		if env.calls[0][0] != "pwsh" || env.calls[1][0] != "powershell" || !reflect.DeepEqual(env.calls[0][1:6], []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command"}) || !strings.HasSuffix(env.calls[1][6], "prefix\nWrite-Output 'literal;argument'\nprepared") {
			t.Fatal("argv was shell-parsed", env.calls)
		}
		if env.options.Env["KEY"] != "value" || *env.options.InheritEnv || env.options.Spill == nil || env.options.OnOutputInfo == nil {
			t.Fatal("execution settings lost", env.options)
		}
	}
	env := &powershellEnv104{Env: LocalEnv(t.TempDir()), failure: "spawn_error"}
	prepareCalls := 0
	tool := PowerShell(env, PowerShellOptions{Programs: []string{}, Prepare: func(context.Context, *BashExecution, *durable.ToolAPI) error { prepareCalls++; return nil }})
	if _, err := tool.Execute(context.Background(), durable.JSON{"command": "x", "timeout": -1}, nil); err == nil || prepareCalls != 0 {
		t.Fatal("invalid timeout caused effect")
	}
	if _, err := tool.Execute(context.Background(), durable.JSON{"command": "x"}, nil); err == nil || len(env.calls) != 0 {
		t.Fatal("empty programs ran", err, env.calls)
	}
}

// Source: pi v1.6.0 packages/durable/test/tools.test.ts:552-615. This
// environment records argv and never starts a PowerShell executable.
func TestSourceV160MockPowerShell(t *testing.T) {
	newEnv := func(t *testing.T, installed, output string, exit int) *sourcePowerShellEnv {
		t.Helper()
		return &sourcePowerShellEnv{Env: LocalEnv(t.TempDir()), installed: installed, output: output, exit: exit}
	}
	t.Run("utf8-single-program-argument", func(t *testing.T) { // 573
		env := newEnv(t, "pwsh", "héllo\n", 0)
		got, err := PowerShell(env).Execute(t.Context(), durable.JSON{"command": "Write-Output 'héllo'"}, nil)
		want := [][]string{{"pwsh", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", "try { [Console]::OutputEncoding=[System.Text.Encoding]::UTF8 } catch {}\nWrite-Output 'héllo'"}}
		if err != nil || got.Content != "héllo\n" || !reflect.DeepEqual(env.calls, want) {
			t.Errorf("output=%q argv=%#v error=%v; want argv=%#v", got.Content, env.calls, err, want)
		}
	})
	t.Run("fallback-and-last-start-error", func(t *testing.T) { // 590
		env := newEnv(t, "powershell", "ok", 0)
		tool := PowerShell(env, PowerShellOptions{CommandPrefix: "$x = 1"})
		got, err := tool.Execute(t.Context(), durable.JSON{"command": "$x"}, nil)
		if err != nil || got.Content != "ok" || !slices.Equal(env.programs(), []string{"pwsh", "powershell"}) || !strings.HasSuffix(env.calls[1][len(env.calls[1])-1], "\n$x = 1\n$x") {
			t.Errorf("output=%q argv=%#v error=%v", got.Content, env.calls, err)
		}
		missing := newEnv(t, "", "", 0)
		_, err = PowerShell(missing).Execute(t.Context(), durable.JSON{"command": "1"}, nil)
		const want = "spawn powershell ENOENT"
		if err == nil || err.Error() != want || len(missing.failures) != 2 || !errors.Is(err, missing.failures[1]) || errors.Is(err, missing.failures[0]) {
			t.Errorf("error=%v; want %q preserving only the last start failure", err, want)
		}
	})
	t.Run("nonzero-exit", func(t *testing.T) { // 606 (native standalone envelope)
		env := newEnv(t, "pwsh", "partial", 3)
		got, err := PowerShell(env).Execute(t.Context(), durable.JSON{"command": "exit 3"}, nil)
		_, truncated := got.Details["truncated"]
		if err == nil || err.Error() != "Command exited with code 3" || got.Content != "partial" || got.Details["exitCode"] != 3 || truncated || !slices.Equal(env.programs(), []string{"pwsh"}) {
			t.Errorf("output=%q details=%v programs=%v error=%v", got.Content, got.Details, env.programs(), err)
		}
	})
}

type sourcePowerShellEnv struct {
	*Env
	installed, output string
	exit              int
	calls             [][]string
	failures          []*durable.ExecutionError
}

func (e *sourcePowerShellEnv) Exec(context.Context, string, durable.ShellExecOptions) (durable.ShellExecResult, error) {
	return durable.ShellExecResult{}, errors.New("unexpected string command in argv-only fixture")
}

func (e *sourcePowerShellEnv) ExecArgs(ctx context.Context, args []string, options durable.ShellExecOptions) (durable.ShellExecResult, error) {
	if len(args) == 0 {
		return durable.ShellExecResult{}, errors.New("missing program in argv")
	}
	e.calls = append(e.calls, slices.Clone(args))
	if args[0] != e.installed {
		cause := fmt.Errorf("spawn %s ENOENT", args[0])
		failure := &durable.ExecutionError{Code: "spawn_error", Cause: cause}
		e.failures = append(e.failures, failure)
		return durable.ShellExecResult{}, failure
	}
	if err := options.OnOutputInfo(ctx, e.output, durable.ShellOutputInfo{Stream: "stdout"}); err != nil {
		return durable.ShellExecResult{}, err
	}
	return durable.ShellExecResult{ExitCode: e.exit}, nil
}

func (e *sourcePowerShellEnv) programs() []string {
	p := make([]string, 0, len(e.calls))
	for _, call := range e.calls {
		p = append(p, call[0])
	}
	return p
}
