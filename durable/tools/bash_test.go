//go:build unix

package tools

import (
	"context"
	"errors"
	"github.com/rcarmo/go-ai/durable"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestBashOutputExitAndBounds(t *testing.T) {
	env := LocalEnv(t.TempDir())
	result, err := Bash(env).Execute(context.Background(), durable.JSON{"command": "printf hello; printf err >&2"}, nil)
	if err != nil || !strings.Contains(result.Content, "hello") || !strings.Contains(result.Content, "err") {
		t.Fatal(result, err)
	}
	result, err = Bash(env).Execute(context.Background(), durable.JSON{"command": "printf failed; exit 7"}, nil)
	if err == nil || !strings.Contains(err.Error(), "7") || result.Content != "failed" {
		t.Fatal(result, err)
	}
	result, err = Bash(env).Execute(context.Background(), durable.JSON{"command": "for ((i=0;i<3000;i++)); do printf '%s\\n' $i; done"}, nil)
	if err != nil || bashLineCount([]byte(result.Content)) > MaxReadLines || !strings.Contains(result.Content, "2999") || result.Details["truncated"] != true {
		t.Fatal(result.Details, err)
	}
	path, ok := result.Details["fullOutputPath"].(string)
	if !ok {
		t.Fatal("truncated bash missing spill", result.Details)
	}
	complete, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(complete), "0\n") || !strings.HasSuffix(string(complete), "2999\n") {
		t.Fatal("spill lost full output", err)
	}
}
func TestBashTimeoutAndCancellationKillDescendants(t *testing.T) {
	for _, mode := range []string{"timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			env := LocalEnv(directory)
			caller, cancel := context.WithCancel(context.Background())
			defer cancel()
			args := durable.JSON{"command": "touch started; (sleep 0.5; touch leaked)& wait"}
			if mode == "timeout" {
				args["timeout"] = 0.05
			} else {
				go func() {
					deadline := time.Now().Add(time.Second)
					for time.Now().Before(deadline) {
						if _, err := os.Stat(filepath.Join(directory, "started")); err == nil {
							cancel()
							return
						}
						time.Sleep(time.Millisecond)
					}
				}()
			}
			_, err := Bash(env).Execute(caller, args, nil)
			want := context.DeadlineExceeded
			if mode == "cancel" {
				want = context.Canceled
			}
			if !errors.Is(err, want) {
				t.Fatal("wrong cancellation", err)
			}
			// Negative process effect assertion: wait beyond the child effect deadline.
			time.Sleep(600 * time.Millisecond)
			if _, err := os.Stat(filepath.Join(directory, "leaked")); !os.IsNotExist(err) {
				t.Fatal("descendant survived", err)
			}
		})
	}
}
func TestBashCancelledBeforeStartDoesNotExecute(t *testing.T) {
	directory := t.TempDir()
	env := LocalEnv(directory)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Bash(env).Execute(ctx, durable.JSON{"command": "touch unexpected"}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "unexpected")); !os.IsNotExist(err) {
		t.Fatal("cancelled command ran", err)
	}
}

func TestBashUsesPortableUTF8DecoderAndCharacterSafeTail(t *testing.T) {
	env := LocalEnv(t.TempDir())
	result, err := Bash(env).Execute(context.Background(), durable.JSON{"command": "printf '\\360\\237'; sleep 0.01; printf '\\230\\200'; printf '\\342\\202' >&2"}, nil)
	if err != nil || !utf8.ValidString(result.Content) || !strings.Contains(result.Content, "😀") || !strings.Contains(result.Content, "�") {
		t.Fatal(result, err)
	}
	capture := &bashCapture{}
	capture.append([]byte("€" + strings.Repeat("a", MaxReadBytes-1)))
	result = capture.result()
	if !utf8.ValidString(result.Content) {
		t.Fatal("byte tail split UTF8", result.Content[:min(4, len(result.Content))])
	}
}
