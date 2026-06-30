package bedrock

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	goai "github.com/rcarmo/go-ai"
)

func TestBuildBedrockConverseInputClampsMaxTokensToContext(t *testing.T) {
	maxTokens := 2000
	model := &goai.Model{ID: "bedrock-test", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, ContextWindow: 5000, MaxTokens: 4000}
	input := buildConverseInput(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{MaxTokens: &maxTokens})
	if input.InferenceConfig == nil || aws.ToInt32(input.InferenceConfig.MaxTokens) != 902 {
		t.Fatalf("maxTokens=%v, want 902", input.InferenceConfig)
	}
}
