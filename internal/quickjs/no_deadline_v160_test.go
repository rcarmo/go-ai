package quickjs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:569–591.
func TestSourceV160QuickJSNoDeadline(t *testing.T) {
	engine, err := NewEngine(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	t.Run("wait", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		entered, release := make(chan struct{}), make(chan struct{})
		result := make(chan Result, 1)
		go func() {
			result <- engine.Execute(ctx, `return await tools.wait()`, Options{
				NoDeadline: true, Timeout: time.Nanosecond,
				Tools: []Tool{{Name: "wait", Execute: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
					close(entered)
					select {
					case <-release:
						return json.RawMessage(`"late"`), nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}}},
			})
		}()
		select {
		case <-entered:
			close(release)
		case got := <-result:
			t.Fatalf("execution ended before calling wait: ok=%v error=%+v", got.OK, got.Error)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		select {
		case got := <-result:
			if !got.OK || string(got.Value) != `"late"` {
				t.Fatalf("ok=%v value=%s error=%+v; want late", got.OK, got.Value, got.Error)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	})
	t.Run("stall-versus-return", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		seen := make(chan string, 2)
		opts := Options{NoDeadline: true, Tools: []Tool{{Name: "echo", Execute: func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
			seen <- string(args)
			return args, nil
		}}}}
		got := engine.Execute(ctx, `await tools.echo(1); await new Promise(() => {}); return 'never'`, opts)
		if got.OK || got.Error == nil || got.Error.Kind != ErrorScript || !strings.Contains(got.Error.Message, "can never settle") {
			t.Fatalf("ok=%v error=%+v; want a stall diagnosed by the engine", got.OK, got.Error)
		}
		select {
		case args := <-seen:
			if args != "1" {
				t.Fatalf("echo saw %s; want 1", args)
			}
		default:
			t.Fatal("echo did not settle before the stall")
		}
		got = engine.Execute(ctx, `tools.echo(2); return 'early'`, opts)
		if !got.OK || string(got.Value) != `"early"` {
			t.Fatalf("ok=%v value=%s error=%+v; want early", got.OK, got.Value, got.Error)
		}
	})
}
