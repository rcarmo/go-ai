package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

func TestCodemode120MixedToolTextProjectionAndReplay(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	cases := []struct {
		name   string
		blocks []goai.ContentBlock
		limits *ToolOutputLimits
		want   string
	}{
		{"mixed", []goai.ContentBlock{{Type: "text", Text: ""}, {Type: "image", MimeType: "image/png", Data: "aGVsbG8="}, {Type: "text", Text: "cat"}}, nil, "\ncat"},
		{"image-only", []goai.ContentBlock{{Type: "image", MimeType: "image/png", Data: "aGVsbG8="}}, nil, ""},
		{"bounded", []goai.ContentBlock{{Type: "text", Text: "a"}, {Type: "image", MimeType: "image/png", Data: "aGVsbG8="}, {Type: "text", Text: "b"}}, &ToolOutputLimits{MaxBytes: 2, MaxLines: 10, Retain: "head"}, "a\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				registry := NewRegistry()
				var effects atomic.Int64
				tool := wrapRegistration("mixed")
				tool.OutputLimits = tc.limits
				tool.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
					effects.Add(1)
					return ToolResult{Blocks: tc.blocks}, nil
				}
				if err := registry.Register(tool); err != nil {
					t.Fatal(err)
				}
				def := taskDefinition(t, "mixed-tool", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					for i := 0; i < 2; i++ {
						result, err := r.RunCodemode(ctx, "script", `return await tools.mixed({});`, engine)
						if err != nil {
							return err
						}
						if result.Error != "" || !result.HasValue || result.Value != tc.want || len(result.Images) != 0 {
							t.Errorf("text projection %+v want %q", result, tc.want)
						}
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				if _, err := registry.RegisterTask(def); err != nil {
					t.Fatal(err)
				}
				h := openHarness(t, b.store, Options{Registry: registry})
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				record := waitPublicTask(t, h, id)
				if record.State.Outcome.Status != "completed" || effects.Load() != 1 {
					t.Fatalf("%+v error=%+v effects=%d", record.State.Outcome, record.State.Outcome.Error, effects.Load())
				}
			})
		})
	}
}
