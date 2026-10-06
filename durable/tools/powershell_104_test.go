//go:build unix

package tools

import (
	"context"
	"errors"
	"reflect"
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
