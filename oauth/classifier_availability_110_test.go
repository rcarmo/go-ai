package oauth

import (
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestOpenAI110ClassifierAvailabilityRequiresAPIKey(t *testing.T) {
	runtime, err := RuntimeForAPIKey(goai.ProviderOpenAI, "api-key")
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.Models) == 0 || len(runtime.ClassifierModels) != 1 || runtime.ClassifierModels[0].ID != "gpt-6-luna" || runtime.ClassifierModels[0].Api != goai.ClassifierApiOpenAIDecisions {
		t.Fatal("API-key catalog", runtime)
	}
	if _, err := RuntimeForAPIKey(goai.ProviderOpenAI, ""); err == nil {
		t.Fatal("empty API key admitted")
	}
	oauthRuntime, err := RuntimeForProvider("openai-chatgpt", &Credentials{Access: "chatgpt-token", Expires: time.Now().Add(time.Hour).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	if len(oauthRuntime.Models) == 0 || len(oauthRuntime.ClassifierModels) != 0 {
		t.Fatal("OAuth exposes Decisions", oauthRuntime)
	}
}
