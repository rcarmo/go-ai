package bedrock

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	goai "github.com/rcarmo/go-ai"
)

func bedrockStreamArguments(size int) (string, map[string]any) {
	var content strings.Builder
	for line := 0; content.Len() < size; line++ {
		fmt.Fprintf(&content, "const line%d = \"value %d\"; // \\ done\n", line, line)
	}
	value := content.String()[:size]
	args := struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}{"/src/file.ts", value}
	raw, _ := json.Marshal(args)
	return string(raw), map[string]any{"path": args.Path, "content": value}
}
func bedrockArgumentsReader(text string) *lifecycleReader {
	events := make(chan types.ConverseStreamOutput, len(text)/32+10)
	events <- &types.ConverseStreamOutputMemberMessageStart{Value: types.MessageStartEvent{Role: types.ConversationRoleAssistant}}
	for _, chunk := range [][]byte{{1, 2}, {3}} {
		events <- &types.ConverseStreamOutputMemberContentBlockDelta{Value: types.ContentBlockDeltaEvent{ContentBlockIndex: aws.Int32(0), Delta: &types.ContentBlockDeltaMemberReasoningContent{Value: &types.ReasoningContentBlockDeltaMemberRedactedContent{Value: chunk}}}}
	}
	events <- &types.ConverseStreamOutputMemberContentBlockStop{Value: types.ContentBlockStopEvent{ContentBlockIndex: aws.Int32(0)}}
	events <- &types.ConverseStreamOutputMemberContentBlockStart{Value: types.ContentBlockStartEvent{ContentBlockIndex: aws.Int32(1), Start: &types.ContentBlockStartMemberToolUse{Value: types.ToolUseBlockStart{ToolUseId: aws.String("tool_1"), Name: aws.String("write")}}}}
	for index := 0; index < len(text); index += 32 {
		end := min(index+32, len(text))
		events <- &types.ConverseStreamOutputMemberContentBlockDelta{Value: types.ContentBlockDeltaEvent{ContentBlockIndex: aws.Int32(1), Delta: &types.ContentBlockDeltaMemberToolUse{Value: types.ToolUseBlockDelta{Input: aws.String(text[index:end])}}}}
	}
	events <- &types.ConverseStreamOutputMemberContentBlockStop{Value: types.ContentBlockStopEvent{ContentBlockIndex: aws.Int32(1)}}
	events <- &types.ConverseStreamOutputMemberMessageStop{Value: types.MessageStopEvent{StopReason: types.StopReasonToolUse}}
	close(events)
	return &lifecycleReader{events: events}
}
func BenchmarkBedrockArgumentStreamMain(b *testing.B) {
	for _, size := range []int{25000, 250000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			text, _ := bedrockStreamArguments(size)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				response := lifecycleResponse(bedrockArgumentsReader(text))
				b.StartTimer()
				processConverseStream(response, &goai.Model{ID: "test", Api: goai.ApiBedrockConverseStream}, func(goai.Event) {})
			}
		})
	}
}
