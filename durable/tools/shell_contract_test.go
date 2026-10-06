//go:build unix

package tools

import (
	"context"
	"errors"
	"github.com/rcarmo/go-ai/durable"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPortableShellReferenceCwdRawSpillAndFailureOwnership(t *testing.T) {
	env := LocalEnv(t.TempDir())
	ctx := context.Background()
	var output strings.Builder
	_, err := env.Exec(ctx, "pwd", durable.ShellExecOptions{OnOutput: func(_ context.Context, text string) error { output.WriteString(text); return nil }})
	if err != nil || strings.TrimSpace(output.String()) != env.Cwd() {
		t.Fatal("shell cwd", output.String(), env.Cwd(), err)
	}
	// Invalid UTF-8 must stay byte-exact in the host spill; callbacks expose
	// decoded replacement text and must not be used as the spill source.
	raw := []byte{0xff, 0x00, 0xf0, 0x9f, 0x98, 0x80, '\n'}
	result, err := env.Exec(ctx, `printf '\377\000\360\237\230\200\n'`, durable.ShellExecOptions{Spill: &durable.ShellSpillOptions{AfterBytes: 1}})
	if err != nil || result.SpillPath == "" {
		t.Fatal(result, err)
	}
	saved, err := os.ReadFile(result.SpillPath)
	if err != nil || string(saved) != string(raw) {
		t.Fatal("raw spill changed", saved, err)
	}
	t.Cleanup(func() { _ = os.Remove(result.SpillPath) })
	for _, ending := range []string{"timeout", "callback"} {
		t.Run(ending, func(t *testing.T) {
			failure := errors.New("callback failure")
			timeout := 0.03
			options := durable.ShellExecOptions{Spill: &durable.ShellSpillOptions{AfterBytes: 1}}
			if ending == "timeout" {
				options.Timeout = &timeout
			} else {
				options.OnOutput = func(context.Context, string) error { return failure }
			}
			// Raw bytes and cwd must survive the failure path too; callbacks
			// receive decoded text only after these bytes have reached spill.
			result, err := env.Exec(ctx, `printf '\377\000%s\n' "$PWD"; sleep 5`, options)
			var execution *durable.ExecutionError
			want := "timeout"
			if ending == "callback" {
				want = "callback_error"
			}
			if !errors.As(err, &execution) || execution.Code != want || result.SpillPath == "" || execution.SpillPath != result.SpillPath {
				t.Fatal(result, err)
			}
			t.Cleanup(func() { _ = os.Remove(result.SpillPath) })
			if err := env.Cleanup(ctx); err != nil {
				t.Fatal(err)
			}
			bytes, err := os.ReadFile(result.SpillPath)
			wantBytes := string([]byte{0xff, 0}) + env.Cwd() + "\n"
			if err != nil || string(bytes) != wantBytes {
				t.Fatal("failure lost caller-owned spill", string(bytes), err)
			}
		})
	}
}

func TestPortableShellIndependentUTF8DecodersWithInterleavedStreams(t *testing.T) {
	env := LocalEnv(t.TempDir())
	ctx := context.Background()
	var mu sync.Mutex
	var chunks []string
	stderrSeen := make(chan struct{})
	var once sync.Once
	// stdout holds half an emoji while stderr contributes ASCII. The callback
	// creates the gate that releases the remaining stdout bytes, proving the
	// streams interleave without relying on sleeps or scheduler arrival order.
	done := make(chan error, 1)
	go func() {
		_, err := env.Exec(ctx, `printf '\360\237'; printf STDERR >&2; while [ ! -f gate ]; do sleep 0.001; done; printf '\230\200'`, durable.ShellExecOptions{OnOutput: func(_ context.Context, text string) error {
			mu.Lock()
			chunks = append(chunks, text)
			mu.Unlock()
			if strings.Contains(text, "STDERR") {
				once.Do(func() { close(stderrSeen) })
				return os.WriteFile(filepath.Join(env.Cwd(), "gate"), nil, 0600)
			}
			return nil
		}})
		done <- err
	}()
	select {
	case <-stderrSeen:
	case <-time.After(3 * time.Second):
		t.Fatal("stderr gate missing")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("interleaved shell did not finish")
	}
	mu.Lock()
	text := strings.Join(chunks, "")
	mu.Unlock()
	if !strings.Contains(text, "STDERR") || !strings.Contains(text, "😀") || strings.Contains(text, "�") {
		t.Fatal("streams shared UTF8 state", chunks)
	}
}
