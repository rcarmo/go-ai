//go:build unix

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/rcarmo/go-ai/durable"
)

func TestShell104ArgvAndOutputStreamMetadata(t *testing.T) {
	env := LocalEnv(t.TempDir())
	ctx := context.Background()
	var output strings.Builder
	var streams []string
	result, err := env.ExecArgs(ctx, []string{"printf", "%s", "literal ; $(false)"}, durable.ShellExecOptions{OnOutputInfo: func(_ context.Context, text string, info durable.ShellOutputInfo) error {
		output.WriteString(text)
		streams = append(streams, info.Stream)
		return nil
	}, OnOutput: func(context.Context, string) error { t.Error("legacy callback repeated metadata output"); return nil }})
	if err != nil || result.ExitCode != 0 || output.String() != "literal ; $(false)" || len(streams) == 0 || streams[0] != "stdout" {
		t.Fatal(result, err, output.String(), streams)
	}
	output.Reset()
	streams = nil
	_, err = env.Exec(ctx, "printf 'out'; printf 'err' >&2", durable.ShellExecOptions{OnOutputInfo: func(_ context.Context, text string, info durable.ShellOutputInfo) error {
		output.WriteString(text)
		streams = append(streams, info.Stream)
		return nil
	}})
	if err != nil || !strings.Contains(output.String(), "out") || !strings.Contains(output.String(), "err") {
		t.Fatal(output.String(), streams, err)
	}
	sawOut, sawErr := false, false
	for _, stream := range streams {
		if stream == "stdout" {
			sawOut = true
		}
		if stream == "stderr" {
			sawErr = true
		}
	}
	if !sawOut || !sawErr {
		t.Fatal(streams)
	}
}
