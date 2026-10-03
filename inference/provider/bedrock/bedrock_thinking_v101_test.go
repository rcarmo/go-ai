package bedrock

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type capturedBedrockRequest struct {
	Header http.Header
	Body   map[string]any
	Path   string
}

func mustBedrockModel(t *testing.T, id string) *goai.Model {
	t.Helper()
	goai.RegisterBuiltinModels()
	model := goai.GetModel(goai.ProviderAmazonBedrock, id)
	if model == nil {
		t.Fatalf("missing amazon-bedrock/%s", id)
	}
	clone := *model
	return &clone
}

func requireBedrockMap(t *testing.T, value any, label string) map[string]any {
	t.Helper()
	got, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v (%T), want map[string]any", label, value, value)
	}
	return got
}

func requireBedrockStrings(t *testing.T, value any, label string) []string {
	t.Helper()
	raw, ok := value.([]any)
	if !ok {
		t.Fatalf("%s = %#v (%T), want []any", label, value, value)
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		str, ok := item.(string)
		if !ok {
			t.Fatalf("%s item = %#v (%T), want string", label, item, item)
		}
		out = append(out, str)
	}
	return out
}

func captureBedrockRequest(t *testing.T, model *goai.Model, reasoning goai.ThinkingLevel) capturedBedrockRequest {
	t.Helper()
	var captured capturedBedrockRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		captured.Header = r.Header.Clone()
		captured.Path = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &captured.Body); err != nil {
			t.Fatalf("unmarshal request body: %v; body=%s", err, body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Amzn-Errortype", "ValidationException")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"validation failed"}`))
	}))
	t.Cleanup(server.Close)

	requestModel := *model
	requestModel.BaseURL = server.URL
	requestModel.Provider = goai.ProviderAmazonBedrock
	requestModel.Api = goai.ApiBedrockConverseStream
	if !requestModel.Reasoning {
		requestModel.Reasoning = true
	}

	opts := &goai.StreamOptions{
		Reasoning: &reasoning,
		Env: goai.ProviderEnv{
			"AWS_REGION":            "us-east-1",
			"AWS_ACCESS_KEY_ID":     "test-access-key",
			"AWS_SECRET_ACCESS_KEY": "test-secret-key",
		},
	}
	for range streamBedrock(context.Background(), &requestModel, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts) {
	}
	if captured.Body == nil {
		t.Fatal("expected captured bedrock request body")
	}
	return captured
}

func TestV101BedrockAdaptiveThinkingBindingGatesByModelNameAndGovCloud(t *testing.T) {
	high := goai.ThinkingHigh
	xhigh := goai.ThinkingXHigh

	t.Run("opus 4.8 adds binding beta and block binding", func(t *testing.T) {
		model := mustBedrockModel(t, "global.anthropic.claude-opus-4-6-v1")
		model.ID = "global.anthropic.claude-opus-4-8-v1"
		model.Name = "Claude Opus 4.8 (Global)"
		fields := bedrockAdditionalFields(t, model, high)
		thinking := requireBedrockMap(t, fields["thinking"], "thinking")
		if thinking["type"] != "adaptive" {
			t.Fatalf("thinking.type=%#v fields=%#v", thinking["type"], fields)
		}
		if thinking["display"] != "summarized" {
			t.Fatalf("thinking.display=%#v fields=%#v", thinking["display"], fields)
		}
		block := requireBedrockMap(t, thinking["block_binding"], "thinking.block_binding")
		if block["prefix_mismatch_behavior"] != "drop_block" {
			t.Fatalf("thinking.block_binding=%#v", block)
		}
		if got := requireBedrockMap(t, fields["output_config"], "output_config")["effort"]; got != "high" {
			t.Fatalf("output_config.effort=%#v fields=%#v", got, fields)
		}
		if got := requireBedrockStrings(t, fields["anthropic_beta"], "anthropic_beta"); !reflect.DeepEqual(got, []string{thinkingBindingControlsBeta}) {
			t.Fatalf("anthropic_beta=%#v", got)
		}
	})

	t.Run("sonnet 5.5 adds native xhigh and binding controls", func(t *testing.T) {
		model := mustBedrockModel(t, "global.anthropic.claude-sonnet-5-5")
		fields := bedrockAdditionalFields(t, model, xhigh)
		thinking := requireBedrockMap(t, fields["thinking"], "thinking")
		if _, ok := thinking["block_binding"]; !ok {
			t.Fatalf("missing block_binding in %#v", thinking)
		}
		if got := requireBedrockMap(t, fields["output_config"], "output_config")["effort"]; got != "xhigh" {
			t.Fatalf("output_config.effort=%#v fields=%#v", got, fields)
		}
	})

	t.Run("application profile name fallback adds binding controls", func(t *testing.T) {
		model := mustBedrockModel(t, "global.anthropic.claude-opus-4-6-v1")
		model.ID = "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/profile"
		model.Name = "Claude Opus 5.5"
		fields := bedrockAdditionalFields(t, model, high)
		thinking := requireBedrockMap(t, fields["thinking"], "thinking")
		if _, ok := thinking["block_binding"]; !ok {
			t.Fatalf("missing block_binding in %#v", thinking)
		}
		if got := requireBedrockStrings(t, fields["anthropic_beta"], "anthropic_beta"); !reflect.DeepEqual(got, []string{thinkingBindingControlsBeta}) {
			t.Fatalf("anthropic_beta=%#v", got)
		}
	})

	t.Run("opus 4.6 stays adaptive without binding controls", func(t *testing.T) {
		model := mustBedrockModel(t, "global.anthropic.claude-opus-4-6-v1")
		fields := bedrockAdditionalFields(t, model, high)
		thinking := requireBedrockMap(t, fields["thinking"], "thinking")
		if thinking["type"] != "adaptive" {
			t.Fatalf("thinking.type=%#v fields=%#v", thinking["type"], fields)
		}
		if _, ok := thinking["block_binding"]; ok {
			t.Fatalf("unexpected block_binding in %#v", thinking)
		}
		if _, ok := fields["anthropic_beta"]; ok {
			t.Fatalf("unexpected anthropic_beta in %#v", fields)
		}
	})

	t.Run("sonnet 4.6 stays adaptive without binding controls", func(t *testing.T) {
		model := mustBedrockModel(t, "global.anthropic.claude-sonnet-4-6")
		fields := bedrockAdditionalFields(t, model, high)
		thinking := requireBedrockMap(t, fields["thinking"], "thinking")
		if thinking["type"] != "adaptive" {
			t.Fatalf("thinking.type=%#v fields=%#v", thinking["type"], fields)
		}
		if _, ok := thinking["block_binding"]; ok {
			t.Fatalf("unexpected block_binding in %#v", thinking)
		}
		if _, ok := fields["anthropic_beta"]; ok {
			t.Fatalf("unexpected anthropic_beta in %#v", fields)
		}
	})

	t.Run("govcloud region drops display and binding controls", func(t *testing.T) {
		model := mustBedrockModel(t, "global.anthropic.claude-sonnet-5-5")
		fields := bedrockAdditionalFields(t, model, high, func(o *goai.StreamOptions) { o.Region = "us-gov-west-1" })
		thinking := requireBedrockMap(t, fields["thinking"], "thinking")
		if _, ok := thinking["display"]; ok {
			t.Fatalf("unexpected display in %#v", thinking)
		}
		if _, ok := thinking["block_binding"]; ok {
			t.Fatalf("unexpected block_binding in %#v", thinking)
		}
		if _, ok := fields["anthropic_beta"]; ok {
			t.Fatalf("unexpected anthropic_beta in %#v", fields)
		}
	})

	t.Run("govcloud model id drops display and binding controls", func(t *testing.T) {
		model := mustBedrockModel(t, "global.anthropic.claude-sonnet-5-5")
		model.ID = "us-gov.anthropic.claude-sonnet-5-5"
		fields := bedrockAdditionalFields(t, model, high)
		thinking := requireBedrockMap(t, fields["thinking"], "thinking")
		if _, ok := thinking["display"]; ok {
			t.Fatalf("unexpected display in %#v", thinking)
		}
		if _, ok := thinking["block_binding"]; ok {
			t.Fatalf("unexpected block_binding in %#v", thinking)
		}
		if _, ok := fields["anthropic_beta"]; ok {
			t.Fatalf("unexpected anthropic_beta in %#v", fields)
		}
	})
}

func TestV101BedrockBudgetThinkingPreservesInterleavedBetaAndBudget(t *testing.T) {
	high := goai.ThinkingHigh
	budget := 7777
	model := mustBedrockModel(t, "us.anthropic.claude-sonnet-4-5-20250929-v1:0")
	fields := bedrockAdditionalFields(t, model, high, func(o *goai.StreamOptions) {
		o.ThinkingBudgets = &goai.ThinkingBudgets{High: &budget}
	})
	thinking := requireBedrockMap(t, fields["thinking"], "thinking")
	if thinking["type"] != "enabled" {
		t.Fatalf("thinking.type=%#v fields=%#v", thinking["type"], fields)
	}
	if thinking["budget_tokens"] != float64(budget) {
		t.Fatalf("thinking.budget_tokens=%#v fields=%#v", thinking["budget_tokens"], fields)
	}
	if thinking["display"] != "summarized" {
		t.Fatalf("thinking.display=%#v fields=%#v", thinking["display"], fields)
	}
	if got := requireBedrockStrings(t, fields["anthropic_beta"], "anthropic_beta"); !reflect.DeepEqual(got, []string{interleavedThinkingBeta}) {
		t.Fatalf("anthropic_beta=%#v", got)
	}
}

func TestV101BedrockProductionPayloadIncludesAdaptiveBindingControls(t *testing.T) {
	reasoning := goai.ThinkingXHigh
	captured := captureBedrockRequest(t, mustBedrockModel(t, "global.anthropic.claude-sonnet-5-5"), reasoning)
	if got := captured.Header.Get("Authorization"); !strings.HasPrefix(got, "AWS4-HMAC-SHA256") {
		t.Fatalf("authorization=%q", got)
	}
	if got := captured.Header.Get("X-Amz-Date"); got == "" {
		t.Fatal("missing X-Amz-Date header")
	}
	if !strings.Contains(captured.Path, "/converse-stream") {
		t.Fatalf("path=%q", captured.Path)
	}

	fields := requireBedrockMap(t, captured.Body["additionalModelRequestFields"], "additionalModelRequestFields")
	thinking := requireBedrockMap(t, fields["thinking"], "thinking")
	block := requireBedrockMap(t, thinking["block_binding"], "thinking.block_binding")
	if block["prefix_mismatch_behavior"] != "drop_block" {
		t.Fatalf("thinking.block_binding=%#v", block)
	}
	if got := requireBedrockMap(t, fields["output_config"], "output_config")["effort"]; got != "xhigh" {
		t.Fatalf("output_config.effort=%#v fields=%#v", got, fields)
	}
	if got := requireBedrockStrings(t, fields["anthropic_beta"], "anthropic_beta"); !reflect.DeepEqual(got, []string{thinkingBindingControlsBeta}) {
		t.Fatalf("anthropic_beta=%#v", got)
	}
}
