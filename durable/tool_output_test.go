package durable

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestToolOutputPinnedReferenceChunkSnapshots(t *testing.T) {
	data, err := os.ReadFile("testdata/output-reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Revision string `json:"revision"`
		Cases    []struct {
			Limits ToolOutputLimits `json:"limits"`
			Chunks []struct {
				Input        string `json:"input"`
				Text         string `json:"text"`
				DroppedBytes uint64 `json:"droppedBytes"`
				DroppedLines uint64 `json:"droppedLines"`
			} `json:"chunks"`
			Bounded struct {
				Text string `json:"text"`
			} `json:"bounded"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Revision != "a13d35a742c6ef8462812a28fbe1d8c8b7431c32" || len(fixture.Cases) != 56 {
		t.Fatal("fixture provenance")
	}
	for index, c := range fixture.Cases {
		cp := toolCheckpoint{}
		whole := ""
		for step, chunk := range c.Chunks {
			whole += chunk.Input
			if err := retainToolOutput(&cp, chunk.Input, c.Limits, false); err != nil {
				t.Fatal(err)
			}
			if cp.Output != chunk.Text || cp.DroppedBytes != chunk.DroppedBytes || cp.DroppedLines != chunk.DroppedLines {
				t.Fatalf("case%d step%d %+v: got %q/%d/%d reference %q/%d/%d", index, step, c.Limits, cp.Output, cp.DroppedBytes, cp.DroppedLines, chunk.Text, chunk.DroppedBytes, chunk.DroppedLines)
			}
		}
		if got := boundToolText(whole, c.Limits); got != c.Bounded.Text {
			t.Fatalf("case%d bound %q reference %q", index, got, c.Bounded.Text)
		}
	}
}

func TestToolOutputBoundsWholeLinesUTF8AndZeroLimits(t *testing.T) {
	for _, c := range []struct {
		text, retain, want string
		bytes, lines       int
	}{
		{"a\nb\nc\n", "head", "a\nb\n", 100, 2}, {"a\nb\nc\n", "tail", "b\nc\n", 100, 2},
		{"aa\nbb\ncc\n", "head", "aa\nbb\n", 7, 10}, {"aa\nbb\ncc\n", "tail", "bb\ncc\n", 7, 10},
		{"ééé\n", "head", "éé", 5, 10}, {"x\néééé", "tail", "éé", 5, 10},
		{"a\nb\nc\n\n", "tail", "b\nc\n\n", 100, 3}, {"abc", "head", "", 0, 1}, {"abc", "tail", "", 3, 0},
	} {
		got := boundToolText(c.text, ToolOutputLimits{c.bytes, c.lines, c.retain})
		if got != c.want {
			t.Fatalf("%q %s: %q want %q", c.text, c.retain, got, c.want)
		}
	}
	for _, text := range []string{"é中🙂a", "👩‍💻", strings.Repeat("🙂é", 32)} {
		for bytes := 0; bytes <= len(text)+1; bytes++ {
			got := boundToolText(text, ToolOutputLimits{bytes, 10, "tail"})
			from := len(text) - bytes
			if from < 0 {
				from = 0
			}
			for from < len(text) && !utf8.RuneStart(text[from]) {
				from++
			}
			if got != text[from:] || !utf8.ValidString(got) || len(got) > bytes {
				t.Fatal("tail scalar boundary", bytes, got)
			}
		}
	}
	if sanitizeToolOutput("a\x00b\tc\nd\re\x07f\ufff9g\ufffbh😀") != "ab\tc\ndefgh😀" {
		t.Fatal("control sanitation")
	}
}
func TestToolOutputFinalBlocksPreserveUnchangedShapeAndTruncationPosition(t *testing.T) {
	blocks := []goai.ContentBlock{{Type: "text", Text: "first"}, {Type: "image", MimeType: "image/png", Data: "aGVsbG8="}, {Type: "text", Text: "last"}}
	for _, retain := range []string{"head", "tail"} {
		unchanged := boundToolBlocks(blocks, ToolOutputLimits{100, 10, retain})
		if len(unchanged) != 3 || unchanged[0].Text != "first" || unchanged[2].Text != "last" {
			t.Fatal("unchanged content shape", unchanged)
		}
		cut := boundToolBlocks(blocks, ToolOutputLimits{3, 10, retain})
		if len(cut) != 2 {
			t.Fatal(cut)
		}
		if retain == "head" {
			if cut[0].Text != "fir" || cut[1].Type != "image" {
				t.Fatal("head placement", cut)
			}
		} else if cut[0].Type != "image" || cut[1].Text != "ast" {
			t.Fatal("tail placement", cut)
		}
		empty := boundToolBlocks(blocks, ToolOutputLimits{0, 10, retain})
		if len(empty) != 2 {
			t.Fatal("empty retained text missing", empty)
		}
	}
}

func TestToolOutputStreamingCountsMatchWholeStream(t *testing.T) {
	for _, limits := range []ToolOutputLimits{{40, 3, "head"}, {40, 3, "tail"}, {25, 50, "head"}, {25, 50, "tail"}, {0, 0, "head"}} {
		cp := toolCheckpoint{}
		stream := ""
		for i := 0; i < 300; i++ {
			chunk := fmt.Sprintf("line %d\n", i)
			if i%7 == 0 {
				chunk = strings.Repeat("é", i%30) + "\n"
			}
			stream += chunk
			if err := retainToolOutput(&cp, chunk, limits, false); err != nil {
				t.Fatal(err)
			}
			want := boundToolText(stream, limits)
			if cp.Output != want || cp.DroppedBytes != uint64(len(stream)-len(want)) || cp.DroppedLines != outputLines(stream)-outputLines(want) || len(cp.OutputRaw) > limits.MaxBytes {
				t.Fatal(limits, i, cp.Output, want, cp.DroppedBytes, cp.DroppedLines)
			}
		}
	}
	cp := toolCheckpoint{}
	l := ToolOutputLimits{100, 1, "tail"}
	for _, chunk := range []string{"a\x07\n", "b\x1b\n"} {
		if err := retainToolOutput(&cp, chunk, l, false); err != nil {
			t.Fatal(err)
		}
	}
	if cp.Output != "b\n" || cp.DroppedBytes != 3 || cp.DroppedLines != 1 {
		t.Fatal("sanitized raw totals", cp)
	}
	if err := retainToolOutput(&cp, "reset", l, true); err != nil || cp.Output != "reset" || cp.DroppedBytes != 0 || cp.DroppedLines != 0 {
		t.Fatal("replacement reset", cp, err)
	}
	before := cp
	if err := retainToolOutput(&cp, "\xff", l, false); err == nil || cp.Output != before.Output {
		t.Fatal("invalid UTF8 changed output")
	}
}
func TestToolOutputFinalHeadBoundsCombinedTextOnce(t *testing.T) {
	registry := NewRegistry()
	limits := ToolOutputLimits{7, 10, "head"}
	if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "combined", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "combined.output", Version: 1, OutputLimits: &limits, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
		return ToolResult{Content: "aa\nbbbbb\n", Blocks: []goai.ContentBlock{{Type: "text", Text: "Z"}}}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		calls++
		ch := make(chan goai.Event, 1)
		if calls == 1 {
			ch <- toolAnswer("combined-call", "combined", JSON{})
		} else {
			for _, m := range input.Messages {
				if m.Role == goai.RoleToolResult {
					if len(m.Content) != 1 || m.Content[0].Text != "aa\n" || m.IsError {
						t.Error("later text refilled head cutoff", m)
					}
				}
			}
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	store, _ := NewMemory()
	h := openHarness(t, store, options)
	c := root(t, h, ref)
	sub, err := c.Submit(bg, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	waitSubmission(t, sub)
}

func TestToolOutputPolicyPinsCopiesFinalTextAndNonTextBlocks(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		limits := ToolOutputLimits{12, 2, "tail"}
		registry := NewRegistry()
		reg := ToolRegistration{Definition: goai.Tool{Name: "bounded", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "bounded.test", Version: 1, OutputLimits: &limits, Execute: func(_ context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			if err := api.Output("old line\nrecent\n"); err != nil {
				return ToolResult{}, err
			}
			return ToolResult{Content: strings.Repeat("discarded\n", 5000) + "kept\n", Blocks: []goai.ContentBlock{{Type: "image", MimeType: "image/png", Data: "aGVsbG8="}, {Type: "text", Text: "last\n"}}}, nil
		}}
		if err := registry.Register(reg); err != nil {
			t.Fatal(err)
		}
		limits.MaxBytes = 0 // registration snapshot is detached.
		calls := 0
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			calls++
			ch := make(chan goai.Event, 1)
			if calls == 1 {
				ch <- toolAnswer("bounded-call", "bounded", JSON{})
			} else {
				var found bool
				for _, m := range input.Messages {
					if m.Role == goai.RoleToolResult {
						found = true
						var text string
						images := 0
						for _, block := range m.Content {
							if block.Type == "text" {
								text += block.Text
							} else if block.Type == "image" {
								images++
							}
						}
						if m.IsError || text != "kept\nlast\n" || images != 1 {
							t.Error("bounded final", m)
						}
					}
				}
				if !found {
					t.Error("missing result")
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
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			if task.Kind == "pi.tool" {
				var cp toolCheckpoint
				if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
					t.Fatal(err)
				}
				if cp.Offer.OutputLimits.MaxBytes != 12 || cp.DroppedBytes == 0 {
					t.Fatal("policy or progress counts lost", cp)
				}
			}
		}
		for _, invalid := range []ToolOutputLimits{{-1, 1, "head"}, {MaxToolOutputBytes + 1, 1, "tail"}, {1, -1, "head"}, {1, 1, "invalid"}} {
			reg.OutputLimits = &invalid
			if err := registry.Register(reg); err == nil {
				t.Fatal("invalid retention accepted", invalid)
			}
		}
	})
}
