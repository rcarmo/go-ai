package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	goai "github.com/rcarmo/go-ai"
)

func TestBedrockArgumentStateSnapshotsMain(t *testing.T) {
	text, want := bedrockStreamArguments(25000)
	reader := bedrockArgumentsReader(text)
	ch := make(chan goai.Event, len(text)/32*2+40)
	processConverseStream(lifecycleResponse(reader), &goai.Model{ID: "test", Api: goai.ApiBedrockConverseStream}, goai.NewAssistantEventSender(ch))
	close(ch)
	var prefixes []string
	var final *goai.Message
	ends := 0
	for event := range ch {
		switch e := event.(type) {
		case *goai.ToolCallStartEvent:
			if e.Partial.Content[e.ContentIndex].Arguments == nil {
				t.Fatal("start must expose empty arguments")
			}
		case *goai.ToolCallDeltaEvent:
			if prefix, ok := e.Partial.Content[e.ContentIndex].Arguments["content"].(string); ok {
				prefixes = append(prefixes, prefix)
			}
		case *goai.ToolCallEndEvent:
			ends++
			if !reflect.DeepEqual(e.ToolCall.Arguments, want) || e.ToolCall.ArgumentsError != "" {
				t.Fatal("completed tool arguments changed")
			}
		case *goai.DoneEvent:
			final = e.Message
		case *goai.ErrorEvent:
			t.Fatal(e.Err)
		}
	}
	if final == nil || ends != 1 || final.StopReason != goai.StopReasonToolUse || len(final.Content) != 2 {
		t.Fatal("missing completed tool stream", ends, final)
	}
	if final.Content[0].ThinkingSignature != "AQID" || final.Content[0].Thinking != "[Reasoning redacted]" || !final.Content[0].Redacted {
		t.Fatal("encrypted reasoning changed", final.Content[0])
	}
	previous := 0
	for _, prefix := range prefixes {
		if len(prefix) < previous || !strings.HasPrefix(want["content"].(string), prefix) {
			t.Fatal("argument preview is not a stable prefix")
		}
		previous = len(prefix)
	}
	if len(prefixes) < 2 {
		t.Fatal("missing intermediate previews")
	}
	for _, block := range final.Content {
		raw, err := json.Marshal(block)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		json.Unmarshal(raw, &fields)
		for _, key := range []string{"index", "partialJson", "partialJSON", "redactedChunks", "argumentsStream"} {
			if _, ok := fields[key]; ok {
				t.Fatal("stream state escaped", key)
			}
		}
	}
}

func TestBedrockFlushUnstoppedBlocksMain(t *testing.T) {
	for _, mode := range []string{"eof", "stop", "stream-error", "hook-error", "abort"} {
		t.Run(mode, func(t *testing.T) {
			reader := bedrockArgumentsReader(`{"text":"part`)
			var all []types.ConverseStreamOutput
			for event := range reader.events {
				switch event.(type) {
				case *types.ConverseStreamOutputMemberContentBlockStop:
					continue
				case *types.ConverseStreamOutputMemberMessageStop:
					if mode == "eof" || mode == "stream-error" {
						continue
					}
				}
				all = append(all, event)
			}
			reader.events = make(chan types.ConverseStreamOutput, len(all))
			for _, event := range all {
				reader.events <- event
			}
			close(reader.events)
			sentinel := errors.New("stream failed")
			if mode == "stream-error" {
				reader.err = sentinel
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := &goai.StreamOptions{OnProviderStreamEvent: func(raw any, _ *goai.Model) error {
				if _, ok := raw.(*types.ConverseStreamOutputMemberMessageStop); ok {
					if mode == "hook-error" {
						return sentinel
					}
					if mode == "abort" {
						cancel()
					}
				}
				return nil
			}}
			var final *goai.Message
			ends := 0
			processBedrockStream(ctx, lifecycleResponse(reader), &goai.Model{ID: "test"}, opts, func(event goai.Event) {
				switch e := event.(type) {
				case *goai.ThinkingEndEvent:
					ends++
				case *goai.ToolCallEndEvent:
					ends++
				case *goai.DoneEvent:
					if mode != "stop" {
						t.Error("unexpected success")
					}
					final = e.Message
				case *goai.ErrorEvent:
					if mode == "stop" {
						t.Error("unexpected error", e.Err)
					}
					if mode == "abort" && e.Reason != goai.StopReasonAborted {
						t.Error("abort changed")
					}
					final = e.Error
				}
			})
			if final == nil || ends != 0 || len(final.Content) != 2 {
				t.Fatal("unstopped block lifecycle", ends, final)
			}
			if final.Content[0].ThinkingSignature != "AQID" {
				t.Fatal("unstopped encrypted reasoning lost", final.Content[0])
			}
			if final.Content[1].Arguments["text"] != "part" || final.Content[1].ArgumentsError != "" {
				t.Fatal("unfinished preview was finalized", final.Content[1])
			}
		})
	}
}

func TestBedrockStoppedIndexRetirementMain(t *testing.T) {
	reader := bedrockArgumentsReader("{}")
	var all []types.ConverseStreamOutput
	for event := range reader.events {
		if _, ok := event.(*types.ConverseStreamOutputMemberMessageStop); ok {
			continue
		}
		all = append(all, event)
		if e, ok := event.(*types.ConverseStreamOutputMemberContentBlockStop); ok && aws.ToInt32(e.Value.ContentBlockIndex) == 1 {
			all = append(all, event, event)
		}
	}
	toolDelta := func() types.ConverseStreamOutput {
		return &types.ConverseStreamOutputMemberContentBlockDelta{Value: types.ContentBlockDeltaEvent{ContentBlockIndex: aws.Int32(1), Delta: &types.ContentBlockDeltaMemberToolUse{Value: types.ToolUseBlockDelta{Input: aws.String("ignored")}}}}
	}
	all = append(all, toolDelta(), &types.ConverseStreamOutputMemberContentBlockDelta{Value: types.ContentBlockDeltaEvent{ContentBlockIndex: aws.Int32(1), Delta: &types.ContentBlockDeltaMemberText{Value: "new"}}}, toolDelta(), &types.ConverseStreamOutputMemberMessageStop{Value: types.MessageStopEvent{StopReason: types.StopReasonEndTurn}})
	reader.events = make(chan types.ConverseStreamOutput, len(all))
	for _, event := range all {
		reader.events <- event
	}
	close(reader.events)
	toolEnds := 0
	var final *goai.Message
	processConverseStream(lifecycleResponse(reader), &goai.Model{ID: "test"}, func(event goai.Event) {
		switch e := event.(type) {
		case *goai.ToolCallEndEvent:
			toolEnds++
		case *goai.DoneEvent:
			final = e.Message
		case *goai.ErrorEvent:
			t.Error(e.Err)
		}
	})
	if toolEnds != 1 || final == nil || len(final.Content) != 3 || final.Content[2].Type != "text" || final.Content[2].Text != "new" || len(final.Content[1].Arguments) != 0 {
		t.Fatal("closed index state reused", toolEnds, final)
	}
}

func TestBedrockArgumentAllocationGrowthMain(t *testing.T) {
	allocated := func(size int) int64 {
		text, _ := bedrockStreamArguments(size)
		result := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				response := lifecycleResponse(bedrockArgumentsReader(text))
				b.StartTimer()
				processConverseStream(response, &goai.Model{ID: "test", Api: goai.ApiBedrockConverseStream}, func(goai.Event) {})
			}
		})
		return result.AllocedBytesPerOp()
	}
	small, large := allocated(25000), allocated(250000)
	if small <= 0 || large >= 25*small || large >= 5000*250000 {
		t.Fatalf("allocation growth: 25k=%d, 250k=%d bytes/op", small, large)
	}
}
