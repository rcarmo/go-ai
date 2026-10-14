package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestTool120ModelStructuredValidationReportsWithoutTranscriptField(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var commits atomic.Int64
		reports := make(chan error, 8)
		registry := NewRegistry()
		reg := wrapRegistration("model_structured")
		reg.StructuredOutputSchema = JSON{"type": "integer"}
		reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			return ToolResult{Content: "model-visible", StructuredOutput: "invalid", Commit: func(*Tx) error { commits.Add(1); return nil }}, nil
		}
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if calls.Add(1) == 1 {
				ch <- toolAnswer("call", "model_structured", JSON{})
			} else {
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		options.OnReport = func(e error) { reports <- e }
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		sub, e := conv.Submit(bg, Input{Content: "go"})
		if e != nil {
			t.Fatal(e)
		}
		result := waitSubmission(t, sub)
		if result.Submission.Status != "done" || commits.Load() != 1 {
			t.Fatal(result, commits.Load())
		}
		select {
		case e := <-reports:
			if !strings.Contains(e.Error(), "Structured output") {
				t.Fatal(e)
			}
		default:
			t.Fatal("invalid model structured value not reported")
		}
		state, e := h.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, entry := range state.Entries {
			if entry.Kind == "message" && entry.ByTask != 0 {
				value := entry.Value
				if _, ok := value["structuredOutput"]; ok {
					t.Fatal("structured value added to transcript")
				}
				if strings.Contains(string(mustJSONTool120(t, value)), "model-visible") {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("model output missing")
		}
	})
}
func mustJSONTool120(t *testing.T, value JSON) []byte {
	t.Helper()
	data, e := encodeBounded(value, DefaultLimits(), DefaultLimits().MaxRecordBytes)
	if e != nil {
		t.Fatal(e)
	}
	return data
}
