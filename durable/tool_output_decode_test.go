package durable

import (
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"os"
	"testing"
)

func TestToolOutputBytesProductionFlushAndSealedReceipt(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		limits := ToolOutputLimits{100, 10, "head"}
		registry := NewRegistry()
		var escaped *ToolAPI
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "bytes", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "bytes.test", Version: 1, OutputLimits: &limits, Execute: func(_ context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			escaped = api
			for _, chunk := range [][]byte{{0xf0, 0x9f}, {0x98, 0x80, 10}, {0xe2}} {
				if err := api.OutputBytes(chunk); err != nil {
					return ToolResult{}, err
				}
			}
			if err := api.Output("x"); err != nil {
				return ToolResult{}, err
			}
			if err := api.OutputBytes([]byte{0xe2, 0x82}); err != nil {
				return ToolResult{}, err
			}
			return ToolResult{}, nil
		}}); err != nil {
			t.Fatal(err)
		}
		calls := 0
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			calls++
			ch := make(chan goai.Event, 1)
			if calls == 1 {
				ch <- toolAnswer("bytes-call", "bytes", JSON{})
			} else {
				for _, m := range input.Messages {
					if m.Role == goai.RoleToolResult && (m.IsError || len(m.Content) != 1 || m.Content[0].Text != "😀\n�x�") {
						t.Error("decoded production receipt", m)
					}
				}
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		sub, err := c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		if err := escaped.OutputBytes([]byte("late")); !errors.Is(err, ErrSealed) {
			t.Fatal("escaped byte output", err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		snapshot, err := second.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range snapshot.Tasks {
			if task.Kind == "pi.tool" {
				var cp toolCheckpoint
				if err := fromObject(task.Checkpoint, &cp, second.session.limits); err != nil {
					t.Fatal(err)
				}
				if cp.Result.Content[0].Text != "😀\n�x�" {
					t.Fatal("flush receipt lost on reopen", cp)
				}
			}
		}
	})
}
func TestOutputDecoderPinnedByteChunksStringsAndEnd(t *testing.T) {
	data, err := os.ReadFile("testdata/output-reference.json")
	if err != nil {
		t.Fatal(err)
	}
	type snapshot struct {
		Text         string `json:"text"`
		DroppedBytes uint64 `json:"droppedBytes"`
		DroppedLines uint64 `json:"droppedLines"`
	}
	var fixture struct {
		ByteCases []struct {
			Limits ToolOutputLimits `json:"limits"`
			Chunks []struct {
				Input json.RawMessage `json:"input"`
				snapshot
			} `json:"chunks"`
			Ended snapshot `json:"ended"`
		} `json:"byteCases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.ByteCases) != 12 {
		t.Fatal("byte fixture cases", len(fixture.ByteCases))
	}
	for index, c := range fixture.ByteCases {
		decoder := outputDecoder{}
		cp := toolCheckpoint{}
		check := func(want snapshot) {
			t.Helper()
			if cp.Output != want.Text || cp.DroppedBytes != want.DroppedBytes || cp.DroppedLines != want.DroppedLines {
				t.Fatalf("case%d: got %q/%d/%d reference%q/%d/%d", index, cp.Output, cp.DroppedBytes, cp.DroppedLines, want.Text, want.DroppedBytes, want.DroppedLines)
			}
		}
		for _, chunk := range c.Chunks {
			var text string
			if chunk.Input[0] == '"' {
				if err := json.Unmarshal(chunk.Input, &text); err != nil {
					t.Fatal(err)
				}
				text = decoder.end() + text
			} else {
				var bytes []uint8
				if err := json.Unmarshal(chunk.Input, &bytes); err != nil {
					t.Fatal(err)
				}
				text = decoder.decode(bytes, false)
			}
			if err := retainToolOutput(&cp, text, c.Limits, false); err != nil {
				t.Fatal(err)
			}
			check(chunk.snapshot)
		}
		if err := retainToolOutput(&cp, decoder.end(), c.Limits, false); err != nil {
			t.Fatal(err)
		}
		check(c.Ended)
	}
}
