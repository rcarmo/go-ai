package durable

import (
	"context"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestNestedTool120SchemalessOutputShapeReplayAndIsolation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		blocks []goai.ContentBlock
		want   any
	}{
		{"empty", []goai.ContentBlock{}, ""},
		{"text", []goai.ContentBlock{{Type: "text", Text: "hello"}}, "hello"},
		{"image", []goai.ContentBlock{{Type: "image", MimeType: "image/png", Data: "aGVsbG8="}}, JSON{"type": "image", "mimeType": "image/png", "data": "aGVsbG8="}},
		{"texts", []goai.ContentBlock{{Type: "text", Text: "one"}, {Type: "text", Text: "two"}}, []any{JSON{"type": "text", "text": "one"}, JSON{"type": "text", "text": "two"}}},
		{"mixed", []goai.ContentBlock{{Type: "text", Text: "one"}, {Type: "image", MimeType: "image/png", Data: "aGVsbG8="}}, []any{JSON{"type": "text", "text": "one"}, JSON{"type": "image", "mimeType": "image/png", "data": "aGVsbG8="}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := NewRegistry()
			calls := 0
			reg := wrapRegistration("shape")
			reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
				calls++
				return ToolResult{Blocks: tc.blocks, Diagnostics: []ToolDiagnostic{{Severity: "info", Message: "not output"}}}, nil
			}
			if err := registry.Register(reg); err != nil {
				t.Fatal(err)
			}
			def := taskDefinition(t, "shape-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				for i := 0; i < 2; i++ {
					result, err := r.CallTool(ctx, "key", "shape", JSON{})
					if err != nil {
						return err
					}
					want, _ := ownJSONValue(tc.want, DefaultLimits())
					if !result.HasStructuredOutput || !reflect.DeepEqual(result.StructuredOutput, want) {
						t.Errorf("shape=%#v want=%#v", result.StructuredOutput, tc.want)
					}
					if value, ok := result.StructuredOutput.(map[string]any); ok {
						value["type"] = "changed"
					}
					if value, ok := result.StructuredOutput.([]any); ok && len(value) > 0 {
						value[0] = nil
					}
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			})
			if _, err := registry.RegisterTask(def); err != nil {
				t.Fatal(err)
			}
			store, _ := NewMemory()
			h, err := Open(context.Background(), store, Options{Registry: registry})
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close(context.Background())
			record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
			if record.State.Outcome.Status != "completed" {
				t.Fatal(record)
			}
			if calls != 1 {
				t.Fatal("replay executed tool", calls)
			}
		})
	}
}
