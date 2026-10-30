package durable

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestDiagnosticUTF16MainInvalidArgumentsLifecycle(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		scalar := toolUTF16Scalar(t, `"\ud800"`)
		diagnostic := "bad arguments: " + scalar.WTF8()
		var effects, requests atomic.Int64
		registry := NewRegistry()
		reg := wrapRegistration("invalid_leaf")
		reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "unexpected"}, nil
		}
		if err := registry.Register(reg); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			events := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				event := toolAnswer("invalid-unicode", "invalid_leaf", JSON{})
				event.Message.Content[0].SetStreamingArguments(map[string]any{}, diagnostic)
				events <- event
			} else {
				found := false
				for _, message := range input.Messages {
					if message.Role == goai.RoleToolResult {
						for _, block := range message.Content {
							if strings.Contains(block.Text, diagnostic) {
								found = true
								if !block.HasToolArgumentDiagnostic() {
									t.Error("diagnostic text lost ownership")
								}
							}
						}
					}
				}
				if !found {
					t.Error("full argument diagnostic lost before next model request")
				}
				events <- terminal("answer")
			}
			close(events)
			return events
		})
		options.Registry = registry
		first := openHarness(t, b.store, options)
		conversation := root(t, first, ref)
		submission, err := conversation.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		settled := waitSubmission(t, submission)
		if settled.Submission.Status != "done" || effects.Load() != 0 || requests.Load() != 2 {
			t.Fatal("invalid call executed or failed settlement", settled.Submission, effects.Load(), requests.Load())
		}
		stream, err := first.WatchEvents(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := stream.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		assertArgumentDiagnosticSnapshot(t, snapshot, diagnostic)
		stream.Stop()
		if err = first.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, utf16ReopenStore(t, b.store), options)
		stream, err = second.WatchEvents(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Stop()
		snapshot, err = stream.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		assertArgumentDiagnosticSnapshot(t, snapshot, diagnostic)
	})
}
func assertArgumentDiagnosticSnapshot(t *testing.T, snapshot ConversationSnapshot, diagnostic string) {
	t.Helper()
	found := false
	for _, message := range snapshot.Messages {
		if message.Role != goai.RoleToolResult {
			continue
		}
		for _, d := range message.Diagnostics {
			if strings.Contains(d.Message, diagnostic) {
				found = true
				if !d.argumentError {
					t.Error("diagnostic remark lost ownership")
				}
			}
		}
	}
	if !found {
		t.Error("snapshot lost diagnostic remark")
	}
}

func TestDiagnosticUTF16MainScopeAndSDKCodec(t *testing.T) {
	scalar := toolUTF16Scalar(t, `"\ud800"`)
	text := "diagnostic " + scalar.WTF8()
	block := goai.ContentBlock{}
	block.SetToolArgumentDiagnostic(text)
	raw, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	var decoded goai.ContentBlock
	decoded.SetToolArgumentDiagnostic("")
	if err = json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if err = goai.RestoreStreamingArgumentsJSON(raw, &decoded, true); err != nil || decoded.Text != text {
		t.Fatal("explicit diagnostic SDK restoration failed", err)
	}
	r := MessageReceipt{Role: goai.RoleToolResult, IsError: true, ErrorCode: "invalid_arguments", Content: []goai.ContentBlock{block}, Diagnostics: []ToolDiagnostic{{Severity: "error", Code: "invalid_arguments", Message: text, argumentError: true}}}
	object, err := dtoObject(r, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = copyObject(object, DefaultLimits()); err != nil {
		t.Fatal("diagnostic wire is not generic JSON", err)
	}
	var next MessageReceipt
	if err = fromObject(object, &next, DefaultLimits()); err != nil || next.Content[0].Text != text || next.Diagnostics[0].Message != text {
		t.Fatal("diagnostic receipt roundtrip", err)
	}
	ordinary := r
	ordinary.Content = []goai.ContentBlock{{Type: "text", Text: text}}
	ordinary.Diagnostics = nil
	if _, err = dtoObject(ordinary, DefaultLimits()); err == nil {
		t.Fatal("ordinary text widened")
	}
	if _, err = copyObject(JSON{"text": text}, DefaultLimits()); err == nil {
		t.Fatal("generic text widened")
	}
	for _, mutate := range []func(JSON){func(o JSON) { o["role"] = "assistant" }, func(o JSON) { o["errorCode"] = "other" }, func(o JSON) { o["isError"] = false }, func(o JSON) { o["utf16Arguments"].([]any)[0].(map[string]any)["kind"] = "other" }} {
		bad, err := copyObject(object, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		mutate(bad)
		var target MessageReceipt
		if err = fromObject(bad, &target, DefaultLimits()); err == nil {
			t.Fatal("diagnostic witness escaped its context")
		}
	}
}

func TestDiagnosticUTF16MainASCIICheckpoint(t *testing.T) {
	d := ToolDiagnostic{Severity: "error", Code: "invalid_arguments", Message: "ASCII diagnostic", argumentError: true}
	receipt := MessageReceipt{Role: goai.RoleToolResult, IsError: true, ErrorCode: "invalid_arguments", Diagnostics: []ToolDiagnostic{d}}
	cp := toolCheckpoint{StreamingArguments: true, Arguments: JSON{}, Result: &receipt, Diagnostics: []ToolDiagnostic{d}}
	object, err := dtoObject(cp, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var next toolCheckpoint
	if err = fromObject(object, &next, DefaultLimits()); err != nil || next.Diagnostics[0].Message != d.Message {
		t.Fatal("ASCII diagnostic checkpoint blanked", err)
	}
	block := goai.ContentBlock{}
	block.SetToolArgumentDiagnostic(toolUTF16Scalar(t, `"\ud800"`).WTF8())
	block.Type = "thinking"
	if _, err = encodeBounded(block, DefaultLimits(), DefaultLimits().MaxDocumentBytes); err == nil {
		t.Fatal("diagnostic text flag accepted on nontext block")
	}
}
