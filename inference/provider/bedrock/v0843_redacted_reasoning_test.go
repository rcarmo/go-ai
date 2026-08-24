package bedrock

import (
	"encoding/base64"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	goai "github.com/rcarmo/go-ai"
)

func TestV0843BedrockPreservesRedactedReasoning(t *testing.T) {
	redacted := []byte("encrypted reasoning")
	events := make(chan types.ConverseStreamOutput, 5)
	events <- &types.ConverseStreamOutputMemberMessageStart{Value: types.MessageStartEvent{Role: types.ConversationRoleAssistant}}
	events <- &types.ConverseStreamOutputMemberContentBlockDelta{Value: types.ContentBlockDeltaEvent{ContentBlockIndex: aws.Int32(0), Delta: &types.ContentBlockDeltaMemberReasoningContent{Value: &types.ReasoningContentBlockDeltaMemberRedactedContent{Value: redacted}}}}
	events <- &types.ConverseStreamOutputMemberContentBlockDelta{Value: types.ContentBlockDeltaEvent{ContentBlockIndex: aws.Int32(0), Delta: &types.ContentBlockDeltaMemberReasoningContent{Value: &types.ReasoningContentBlockDeltaMemberRedactedContent{Value: []byte(" tail")}}}}
	events <- &types.ConverseStreamOutputMemberContentBlockStop{Value: types.ContentBlockStopEvent{ContentBlockIndex: aws.Int32(0)}}
	events <- &types.ConverseStreamOutputMemberMessageStop{Value: types.MessageStopEvent{StopReason: types.StopReasonEndTurn}}
	close(events)
	resp := &bedrockruntime.ConverseStreamOutput{}
	stream := bedrockruntime.NewConverseStreamEventStream(func(es *bedrockruntime.ConverseStreamEventStream) { es.Reader = &fakeConverseReader{events: events} })
	setConverseEventStream(resp, stream)

	ch := make(chan goai.Event, 16)
	processConverseStream(resp, &goai.Model{ID: "global.openai.gpt-5.6-terra", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream}, ch)
	close(ch)
	for ev := range ch {
		if done, ok := ev.(*goai.DoneEvent); ok {
			if len(done.Message.Content) != 1 || !done.Message.Content[0].Redacted {
				t.Fatalf("expected one redacted thinking block, got %#v", done.Message.Content)
			}
			want := base64.StdEncoding.EncodeToString(append(redacted, []byte(" tail")...))
			if done.Message.Content[0].ThinkingSignature != want || done.Message.Content[0].Thinking != "[Reasoning redacted]" {
				t.Fatalf("unexpected redacted block: %#v want sig %q", done.Message.Content[0], want)
			}
			return
		}
	}
	t.Fatal("missing done")
}

func TestV0843BedrockReplaysRedactedReasoning(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("opaque"))
	ctx := &goai.Context{Messages: []goai.Message{{Role: goai.RoleAssistant, Api: goai.ApiBedrockConverseStream, Provider: goai.ProviderAmazonBedrock, Model: "m", StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{{Type: "thinking", Redacted: true, ThinkingSignature: payload}, {Type: "toolCall", ID: "tool-1", Name: "read", Arguments: map[string]any{"path": "a"}}}}}}
	messages := convertMessages(ctx, &goai.Model{ID: "m", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream}, "none", nil)
	if len(messages) < 1 || len(messages[0].Content) != 2 {
		t.Fatalf("unexpected messages: %#v", messages)
	}
	redacted, ok := messages[0].Content[0].(*types.ContentBlockMemberReasoningContent)
	if !ok {
		t.Fatalf("first block should be reasoningContent, got %#v", messages[0].Content[0])
	}
	if _, ok := redacted.Value.(*types.ReasoningContentBlockMemberRedactedContent); !ok {
		t.Fatalf("reasoningContent should replay redacted content, got %#v", redacted.Value)
	}
	if _, ok := messages[0].Content[1].(*types.ContentBlockMemberToolUse); !ok {
		t.Fatalf("second block should remain toolUse, got %#v", messages[0].Content[1])
	}
}
