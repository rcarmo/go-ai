//go:build unix

package tools

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPortableLocalShellExitEnvStreamingSpillAndErrors(t *testing.T) {
	env := LocalEnv(t.TempDir())
	ctx := context.Background()
	inherit := false
	var output strings.Builder
	result, err := env.Exec(ctx, "printf '%s' \"$NATIVE_TEST\"; printf stderr >&2; exit 7", durable.ShellExecOptions{Env: map[string]string{"NATIVE_TEST": "value"}, InheritEnv: &inherit, OnOutput: func(_ context.Context, text string) error { output.WriteString(text); return nil }})
	if err != nil || result.ExitCode != 7 || !strings.Contains(output.String(), "value") || !strings.Contains(output.String(), "stderr") {
		t.Fatal(result, output.String(), err)
	}
	output.Reset()
	result, err = env.Exec(ctx, "printf abcdef", durable.ShellExecOptions{Spill: &durable.ShellSpillOptions{AfterBytes: 3, AfterLines: 10}, OnOutput: func(_ context.Context, text string) error { output.WriteString(text); return nil }})
	if err != nil || result.SpillPath == "" || output.String() != "abcdef" {
		t.Fatal(result, output.String(), err)
	}
	defer os.Remove(result.SpillPath)
	data, err := os.ReadFile(result.SpillPath)
	if err != nil || string(data) != "abcdef" {
		t.Fatal(string(data), err)
	}
	failure := errors.New("callback rejected")
	_, err = env.Exec(ctx, "printf output; sleep 5", durable.ShellExecOptions{OnOutput: func(context.Context, string) error { return failure }})
	var execution *durable.ExecutionError
	if !errors.As(err, &execution) || execution.Code != "callback_error" || !errors.Is(err, failure) {
		t.Fatal("callback failure", err)
	}
	timeout := 0.02
	_, err = env.Exec(ctx, "sleep 5", durable.ShellExecOptions{Timeout: &timeout})
	if !errors.As(err, &execution) || execution.Code != "timeout" || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("timeout", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = env.Exec(cancelled, "touch must-not-exist", durable.ShellExecOptions{})
	if !errors.As(err, &execution) || execution.Code != "aborted" {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(env.Cwd(), "must-not-exist")); !os.IsNotExist(err) {
		t.Fatal("cancelled effect", err)
	}
}
func TestPortableLocalShellSeparateUTF8DecodersAndCleanupProcessGroup(t *testing.T) {
	env := LocalEnv(t.TempDir())
	ctx := context.Background()
	var mu sync.Mutex
	output := ""
	_, err := env.Exec(ctx, "printf '\\360\\237'; sleep 0.01; printf '\\230\\200'; printf '\\342\\202' >&2", durable.ShellExecOptions{OnOutput: func(_ context.Context, text string) error { mu.Lock(); defer mu.Unlock(); output += text; return nil }})
	if err != nil || !strings.Contains(output, "😀") || !strings.Contains(output, "�") {
		t.Fatal(output, err)
	}
	entered := make(chan struct{})
	var once sync.Once
	done := make(chan error, 1)
	go func() {
		_, err := env.Exec(ctx, "printf running; (sleep 0.3; touch orphan-effect) & wait", durable.ShellExecOptions{OnOutput: func(context.Context, string) error { once.Do(func() { close(entered) }); return nil }})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("process not started")
	}
	if err := env.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cleanup did not stop process")
	}
	time.Sleep(350 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(env.Cwd(), "orphan-effect")); !os.IsNotExist(err) {
		t.Fatal("cleanup descendant escaped", err)
	}
}

type fakeShellFS struct {
	*memoryToolFS
	command string
	timeout *float64
}

func (fs *fakeShellFS) Exec(ctx context.Context, command string, options durable.ShellExecOptions) (durable.ShellExecResult, error) {
	fs.command = command
	fs.timeout = options.Timeout
	if err := options.OnOutput(ctx, "remote output"); err != nil {
		return durable.ShellExecResult{}, err
	}
	return durable.ShellExecResult{ExitCode: 0, SpillPath: "/remote/output.txt"}, nil
}
func TestLocalShellAbortPreservesSpillAndCallerOwnedFile(t *testing.T) {
	env := LocalEnv(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, err := env.Exec(ctx, "printf complete-output; sleep 5", durable.ShellExecOptions{Spill: &durable.ShellSpillOptions{AfterBytes: 1, AfterLines: 10}, OnOutput: func(context.Context, string) error { cancel(); return nil }})
	var failure *durable.ExecutionError
	if !errors.As(err, &failure) || failure.Code != "aborted" || failure.SpillPath == "" || failure.SpillPath != result.SpillPath {
		t.Fatal(result, err)
	}
	defer os.Remove(result.SpillPath)
	data, err := os.ReadFile(result.SpillPath)
	if err != nil || string(data) != "complete-output" {
		t.Fatal(string(data), err)
	}
	if err := env.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(result.SpillPath); err != nil {
		t.Fatal("cleanup removed caller spill", err)
	}
}
func TestCustomShellFactoryProductionUsesRemoteCapability(t *testing.T) {
	fs := &fakeShellFS{memoryToolFS: &memoryToolFS{files: map[string][]byte{}, id: "remote-factory"}}
	registry := durable.NewRegistry()
	if err := registry.Register(Bash(LocalEnv(t.TempDir()))); err != nil {
		t.Fatal(err)
	}
	api := goai.Api("remote-shell-factory")
	calls := 0
	goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		calls++
		ch := make(chan goai.Event, 1)
		message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{{Type: "text", Text: "done"}}}
		if calls == 1 {
			message.StopReason = goai.StopReasonToolUse
			message.Content = []goai.ContentBlock{{Type: "toolCall", ID: "remote-call", Name: "bash", Arguments: map[string]any{"command": "remote-only-command"}}}
		} else {
			last := input.Messages[len(input.Messages)-1]
			if last.Role != goai.RoleToolResult || last.Content[0].Text != "remote output" || last.IsError {
				t.Error(last)
			}
		}
		ch <- &goai.DoneEvent{Reason: message.StopReason, Message: message}
		close(ch)
		return ch
	}})
	defer goai.UnregisterApi(api)
	store, _ := durable.NewMemory()
	h, err := durable.Open(context.Background(), store, durable.Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model {
		return &goai.Model{ID: "test", Provider: goai.ProviderOpenAI, Api: api, ContextWindow: 128000, MaxTokens: 100}
	}, Env: func(context.Context, durable.EnvTarget) (durable.ExecutionEnvironment, error) { return fs, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close(context.Background())
	c, err := h.Root(context.Background(), durable.AgentChange{Model: durable.ModelRef{Provider: goai.ProviderOpenAI, ID: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := c.Submit(context.Background(), durable.Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	settled, err := sub.Wait(context.Background())
	if err != nil || settled.Submission.Status != "done" || fs.command != "remote-only-command" {
		t.Fatal(settled, fs.command, err)
	}
}
func TestBashCustomShellDoesNotExecuteHostCommand(t *testing.T) {
	fs := &fakeShellFS{memoryToolFS: &memoryToolFS{files: map[string][]byte{}, id: "remote"}}
	result, err := Bash(fs).Execute(context.Background(), durable.JSON{"command": "not-a-host-command"}, nil)
	if err != nil || fs.command != "not-a-host-command" || result.Content != "remote output" || result.Details["fullOutputPath"] != "/remote/output.txt" {
		t.Fatal(result, fs.command, err)
	}
}
