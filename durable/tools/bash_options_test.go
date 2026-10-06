//go:build unix

package tools

import (
	"context"
	"errors"
	"github.com/rcarmo/go-ai/durable"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBashReferenceNoDefaultTimeoutPrefixAndPrepareFactory(t *testing.T) {
	if duration, seconds, err := bashTimeoutArg(durable.JSON{}, "timeout", DefaultBashTimeout); err != nil || duration != 0 || seconds != 0 {
		t.Fatal(duration, seconds, err)
	}
	prepared := 0
	tool := Bash(LocalEnv(t.TempDir()), BashOptions{CommandPrefix: "export ANSWER=42", Prepare: func(_ context.Context, execution *BashExecution, _ *durable.ToolAPI) error {
		prepared++
		if execution.Command != "export ANSWER=42\noriginal" {
			t.Error("prefix/preparation order", execution)
		}
		execution.Command = "printf \"$ANSWER\""
		execution.Env["ANSWER"] = "42"
		execution.InheritEnv = false
		return nil
	}})
	args := durable.JSON{"command": "original"}
	result, err := tool.Execute(context.Background(), args, nil)
	if err != nil || result.Content != "42" || prepared != 1 {
		t.Fatal(result, err, prepared)
	}
	if _, present := result.Details["timeoutSeconds"]; present {
		t.Fatal("default timeout persisted", result)
	}
	fs := &fakeShellFS{memoryToolFS: &memoryToolFS{files: map[string][]byte{}, id: "remote"}}
	remote := Bash(fs, BashOptions{CommandPrefix: "prefix"})
	if _, err := remote.Execute(context.Background(), durable.JSON{"command": "command"}, nil); err != nil {
		t.Fatal(err)
	}
	if fs.timeout != nil || fs.command != "prefix\ncommand" {
		t.Fatal("remote default/prefix", fs.timeout, fs.command)
	}
	if _, err := remote.Execute(context.Background(), durable.JSON{"command": "command", "timeout": 1}, nil); err != nil {
		t.Fatal(err)
	}
	if fs.timeout == nil || *fs.timeout != 1 {
		t.Fatal("explicit timeout lost", fs.timeout)
	}
}

type failingBashFS struct {
	*memoryToolFS
	failure error
}

func (fs *failingBashFS) Exec(context.Context, string, durable.ShellExecOptions) (durable.ShellExecResult, error) {
	return durable.ShellExecResult{}, fs.failure
}

func TestBashReferenceValidationBeforePrepareAndPortableFailureDiagnostics(t *testing.T) {
	prepared := 0
	tool := Bash(LocalEnv(t.TempDir()), BashOptions{Prepare: func(context.Context, *BashExecution, *durable.ToolAPI) error { prepared++; return nil }})
	for _, value := range []any{0, -1, math.NaN(), math.Inf(1), maxTimeoutSeconds + 1} {
		_, err := tool.Execute(context.Background(), durable.JSON{"command": "true", "timeout": value}, nil)
		if err == nil || !strings.HasPrefix(err.Error(), "Invalid timeout:") || prepared != 0 {
			t.Fatal("invalid timeout ran preparation", value, err, prepared)
		}
	}
	for _, code := range []string{"timeout", "aborted"} {
		cause := errors.New("private execution failure")
		failure := &durable.ExecutionError{Code: code, SpillPath: "/remote/full-output", Cause: cause}
		fs := &failingBashFS{memoryToolFS: &memoryToolFS{id: "remote-failure", files: map[string][]byte{}}, failure: failure}
		result, err := Bash(fs).Execute(context.Background(), durable.JSON{"command": "ignored", "timeout": 2}, nil)
		want := "Command aborted"
		if code == "timeout" {
			want = "Command timed out after 2 seconds"
		}
		if err == nil || err.Error() != want || !errors.Is(err, cause) || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "full_output" || result.Details["fullOutputPath"] != "/remote/full-output" {
			t.Fatal(result, err)
		}
	}
}

func TestBashNativeCwdOptionPrecedesPrepareAndPreparedCwdWins(t *testing.T) {
	base := t.TempDir()
	selected := filepath.Join(base, "selected")
	prepared := filepath.Join(base, "prepared")
	for _, dir := range []string{selected, prepared} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Bash(LocalEnv(base)).Execute(context.Background(), durable.JSON{"command": "pwd", "cwd": "selected"}, nil)
	if err != nil || strings.TrimSpace(result.Content) != selected {
		t.Fatal("explicit cwd ignored", result, err)
	}
	tool := Bash(LocalEnv(base), BashOptions{Prepare: func(_ context.Context, execution *BashExecution, _ *durable.ToolAPI) error {
		if execution.Cwd != selected {
			t.Error("prepare missing native cwd", execution.Cwd)
		}
		execution.Cwd = prepared
		return nil
	}})
	result, err = tool.Execute(context.Background(), durable.JSON{"command": "pwd", "cwd": "selected"}, nil)
	if err != nil || strings.TrimSpace(result.Content) != prepared {
		t.Fatal("prepare cwd ignored", result, err)
	}
}
