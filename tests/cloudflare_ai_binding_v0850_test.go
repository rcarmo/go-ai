package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type fakeCloudflareAIBinding struct {
	inputs []any
	inits  []any
}

func (f *fakeCloudflareAIBinding) Fetch(input any, init any) (any, error) {
	f.inputs = append(f.inputs, input)
	f.inits = append(f.inits, init)
	return "response", nil
}

func TestV0850CloudflareAIBindingSentinelAndFetchAdapter(t *testing.T) {
	if goai.CloudflareGatewayBindingAuthSentinel != "cloudflare-gateway-binding" {
		t.Fatalf("sentinel=%q", goai.CloudflareGatewayBindingAuthSentinel)
	}
	binding := &fakeCloudflareAIBinding{}
	fetch, err := goai.CreateCloudflareAIBindingFetch(binding)
	if err != nil {
		t.Fatal(err)
	}
	got, err := fetch("https://workers-binding.ai/ai-gateway/gateways/gw/openai/chat/completions", map[string]string{
		"cf-aig-authorization": "Bearer " + goai.CloudflareGatewayBindingAuthSentinel,
	})
	if err != nil || got != "response" {
		t.Fatalf("fetch got=%#v err=%v", got, err)
	}
	if len(binding.inputs) != 1 || binding.inputs[0] != "https://workers-binding.ai/ai-gateway/gateways/gw/openai/chat/completions" {
		t.Fatalf("inputs=%#v", binding.inputs)
	}
}

func TestV0850CloudflareAIBindingRejectsMissingFetchEarly(t *testing.T) {
	_, err := goai.CreateCloudflareAIBindingFetch(nil)
	if err == nil || err.Error() != "create Cloudflare AI binding fetch: the AI binding does not expose fetch()" {
		t.Fatalf("err=%v", err)
	}
}
