//go:build unix

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
)

// Only the ArgvShell boundary is mocked. The shell's real output callback
// updates the live ToolAPI, and each next chunk waits for the public event.
type livePowerShellV160 struct {
	*Env
	installed string
	chunks    []string
	exit      int
	accepted  chan struct{}
	calls     [][]string
}

func (*livePowerShellV160) Exec(context.Context, string, durable.ShellExecOptions) (durable.ShellExecResult, error) {
	return durable.ShellExecResult{}, errors.New("expected direct argv without a shell")
}

func (e *livePowerShellV160) ExecArgs(ctx context.Context, args []string, options durable.ShellExecOptions) (durable.ShellExecResult, error) {
	e.calls = append(e.calls, slices.Clone(args))
	if len(args) == 0 {
		return durable.ShellExecResult{}, errors.New("mock PowerShell received no program")
	}
	if args[0] != e.installed {
		return durable.ShellExecResult{}, &durable.ExecutionError{Code: "spawn_error", Cause: fmt.Errorf("spawn %s ENOENT", args[0])}
	}
	if options.OnOutputInfo == nil {
		return durable.ShellExecResult{}, errors.New("missing real shell output callback")
	}
	for _, chunk := range e.chunks {
		if err := options.OnOutputInfo(ctx, chunk, durable.ShellOutputInfo{Stream: "stdout"}); err != nil {
			return durable.ShellExecResult{}, err
		}
		select {
		case <-e.accepted:
		case <-ctx.Done():
			return durable.ShellExecResult{}, ctx.Err()
		}
	}
	return durable.ShellExecResult{ExitCode: e.exit}, nil
}

// Source: tools.test.ts:573–615. Split the exact mocked source text into two
// chunks so the public progress observation proves their order, not just the
// final retained content. This never launches a PowerShell executable.
func TestSourceV160MockPowerShellLiveProgress(t *testing.T) {
	for _, tc := range []struct {
		name, installed, command, prefix string
		chunks, programs                 []string
		exit                             int
		wantErr                          string
	}{
		{"pwshUTF8", "pwsh", "Write-Output 'héllo'", "", []string{"hé", "llo\n"}, []string{"pwsh"}, 0, ""},
		{"pwshFallback", "powershell", "$x", "$x = 1", []string{"o", "k"}, []string{"pwsh", "powershell"}, 0, ""},
		{"pwshMissing", "", "1", "", nil, []string{"pwsh", "powershell"}, 0, "spawn powershell ENOENT"},
		{"pwshExit", "pwsh", "exit 3", "", []string{"par", "tial"}, []string{"pwsh"}, 3, "Command exited with code 3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			env := &livePowerShellV160{Env: LocalEnv(t.TempDir()), installed: tc.installed, chunks: tc.chunks, exit: tc.exit, accepted: make(chan struct{})}
			tool := PowerShell(env, PowerShellOptions{CommandPrefix: tc.prefix})
			type returned struct {
				result durable.ToolResult
				err    error
			}
			returnedByTool := make(chan returned, 1)
			execute := tool.Execute
			tool.Execute = func(ctx context.Context, args any, api *durable.ToolAPI) (durable.ToolResult, error) {
				if api == nil || api.TaskID() == 0 {
					return durable.ToolResult{}, errors.New("expected a live ToolAPI")
				}
				result, err := execute(ctx, args, api)
				returnedByTool <- returned{result, err}
				return result, err
			}
			registry := durable.NewRegistry()
			if err := registry.Register(tool); err != nil {
				t.Fatal(err)
			}
			api := goai.Api("powershell-progress-v160-" + tc.name)
			model := &goai.Model{ID: "mock-powershell", Provider: goai.ProviderOpenAI, Api: api, BaseURL: "https://unreachable.invalid", ContextWindow: 128000, MaxTokens: 256}
			var requests atomic.Int32
			const callID = "mock-powershell-call"
			goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{{Type: "text", Text: "done"}}}
				if requests.Add(1) == 1 {
					message.StopReason = goai.StopReasonToolUse
					message.Content = []goai.ContentBlock{{Type: "toolCall", ID: callID, Name: "powershell", Arguments: map[string]any{"command": tc.command}}}
				}
				result := make(chan goai.Event, 1)
				result <- &goai.DoneEvent{Reason: message.StopReason, Message: message}
				close(result)
				return result
			}})
			t.Cleanup(func() { goai.UnregisterApi(api) })
			store, err := newSQLiteStore(t)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close(context.Background()) })
			zero := int64(0)
			h, err := durable.Open(ctx, store, durable.Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }, Settings: &durable.HarnessSettings{Progress: &durable.ProgressSettings{OutputIntervalMs: &zero}}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = h.Close(context.Background()) })
			c, err := h.Root(ctx, durable.AgentChange{Model: durable.ModelRef{Provider: model.Provider, ID: model.ID}})
			if err != nil {
				t.Fatal(err)
			}
			stream, err := h.WatchEvents(ctx, c.ID())
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Stop()
			events := make(chan durable.AgentEvent, 16)
			if err := stream.Start(func(ctx context.Context, batch []durable.AgentEvent) error {
				for _, event := range batch {
					if event.ToolCallID == callID && (event.OutputChange != nil || event.Type == "tool_execution_end") {
						select {
						case events <- event:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			submission, err := c.Submit(ctx, durable.Input{Content: "run mock PowerShell"})
			if err != nil {
				t.Fatal(err)
			}
			var streamed []string
			var end *durable.MessageReceipt
			for end == nil {
				select {
				case event := <-events:
					if event.Type == "tool_execution_end" {
						end = event.Message
						if end == nil {
							t.Fatal("terminal event without the committed model receipt")
						}
						break
					}
					change := event.OutputChange
					if event.Type != "tool_execution_update" || change == nil || change.Set != nil || change.TrimStart != 0 || len(streamed) >= len(tc.chunks) || change.Append != tc.chunks[len(streamed)] {
						t.Fatalf("unexpected progress after %q: type=%s change=%+v output=%q", streamed, event.Type, change, event.Output)
					}
					streamed = append(streamed, change.Append)
					if event.Output != strings.Join(streamed, "") {
						t.Fatalf("preview=%q; want cumulative %q", event.Output, streamed)
					}
					select {
					case env.accepted <- struct{}{}:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				case <-stream.Closed():
					end, _ := stream.End()
					t.Fatalf("watch ended before tool result: %+v", end)
				case <-ctx.Done():
					t.Fatalf("watch did not receive required progress: streamed=%q: %v", streamed, ctx.Err())
				}
			}
			if !slices.Equal(streamed, tc.chunks) {
				t.Fatalf("public streamed chunks=%q; want %q", streamed, tc.chunks)
			}
			if result, err := submission.Wait(ctx); err != nil || result.Record.Status != "done" {
				t.Fatal("submission did not settle", result, err)
			}
			if requests.Load() != 2 {
				t.Fatal("expected one tool call and one answer", requests.Load())
			}
			var got returned
			select {
			case got = <-returnedByTool:
			case <-ctx.Done():
				t.Fatal("missing Execute result")
			}
			if (tc.wantErr == "" && got.err != nil) || (tc.wantErr != "" && (got.err == nil || got.err.Error() != tc.wantErr)) || got.result.Details["exitCode"] != tc.exit {
				t.Fatalf("live Execute exit=%v error=%v; want exit=%d, error=%q", got.result.Details["exitCode"], got.err, tc.exit, tc.wantErr)
			}
			command := "try { [Console]::OutputEncoding=[System.Text.Encoding]::UTF8 } catch {}\n"
			if tc.prefix != "" {
				command += tc.prefix + "\n"
			}
			command += tc.command
			var wantArgv [][]string
			for _, program := range tc.programs {
				wantArgv = append(wantArgv, []string{program, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", command})
			}
			if !reflect.DeepEqual(env.calls, wantArgv) {
				t.Errorf("argv=%q; want %q", env.calls, wantArgv)
			}
			// Source returns structuredOutput and an exit_code diagnostic. Go
			// returns the execution error and puts exitCode in receipt details;
			// its Harness retains output and renders the generic tool_error.
			var wantText []string
			if len(tc.chunks) != 0 {
				wantText = append(wantText, strings.Join(tc.chunks, ""))
			}
			var wantDiagnostics []durable.ToolDiagnostic
			if tc.wantErr != "" {
				wantText = append(wantText, "<harness>\n[error] "+tc.wantErr+"\n</harness>")
				wantDiagnostics = []durable.ToolDiagnostic{{Severity: "error", Code: "tool_error", Message: tc.wantErr}}
			}
			var text []string
			for _, block := range end.Content {
				if block.Type != "text" {
					t.Fatalf("unexpected result block: %+v", block)
				}
				text = append(text, block.Text)
			}
			_, truncated := end.Details["truncated"]
			if !slices.Equal(text, wantText) || end.IsError != (tc.wantErr != "") || !slices.Equal(end.Diagnostics, wantDiagnostics) || end.Details["exitCode"] != json.Number(strconv.Itoa(tc.exit)) || truncated || got.result.HasStructuredOutput {
				t.Fatalf("native receipt: content=%q isError=%v details=%v diagnostics=%+v returnedStructured=%v", text, end.IsError, end.Details, end.Diagnostics, got.result.HasStructuredOutput)
			}
		})
	}
}
