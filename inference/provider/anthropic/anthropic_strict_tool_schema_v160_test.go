package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source: unchanged pi v1.7.0 anthropic-strict-tool-schema.test.ts:71–169.
// Retain the historical test name. First run each original's stop-at-hook
// design, then separately observe the production request.
func TestSourceV160AnthropicStrictToolSchemaWire(t *testing.T) {
	const defs = `{"type":"object","properties":{"settings":{"$ref":"#/$defs/Settings"}},"required":["settings"],"$defs":{"Settings":{"type":"object","properties":{"verbose":{"type":"boolean"}}}}}`
	const definitions = `{"type":"object","properties":{"settings":{"$ref":"#/definitions/Settings"}},"required":["settings"],"definitions":{"Settings":{"type":"object","properties":{"verbose":{"type":"boolean"}}}}}`
	const nestedMinItems = `{"type":"object","required":["options"],"properties":{"options":{"type":"object","required":["tags"],"properties":{"tags":{"type":"array","items":{"type":"string"},"minItems":2}}}}}`
	const regexFormat = `{"type":"object","required":["expression"],"properties":{"expression":{"type":"string","format":"regex"}}}`
	const supported = `"type":"object","required":["code","url","tags"],"properties":{"code":{"type":"string","minLength":1,"maxLength":1000,"pattern":"^[a-z]+$"},"url":{"type":"string","format":"uri"},"tags":{"type":"array","items":{"type":"string"},"minItems":1}}`
	cases := []struct {
		name, mode, parameters, schema string
		strict                         bool
	}{
		{"legacy", "", `{"type":"object","required":["value"],"properties":{"value":{"type":"string"}},"additionalProperties":false,"title":"LookupInput"}`,
			`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`, false},
		{"strict", "prefer", `{"type":"object","required":["value"],"properties":{"value":{"type":"string"},"optional":{"type":"number"}},"title":"StrictLookupInput"}`,
			`{"type":"object","required":["value","optional"],"properties":{"value":{"type":"string"},"optional":{"anyOf":[{"type":"number"},{"type":"null"}]}},"title":"StrictLookupInput","additionalProperties":false}`, true},
		{"$defs", "", defs, defs, false},
		{"definitions", "", definitions, definitions, false},
		{"minimum-maximum", "prefer", `{"type":"object","properties":{"timeoutMs":{"type":"integer","minimum":1,"maximum":300000}}}`,
			`{"type":"object","properties":{"timeoutMs":{"type":"integer","minimum":1,"maximum":300000}},"required":[]}`, false},
		{"nested-minItems", "prefer", nestedMinItems, nestedMinItems, false},
		{"regex-format", "prefer", regexFormat, regexFormat, false},
		{"supported", "prefer", `{` + supported + `}`, `{` + supported + `,"additionalProperties":false}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var schema map[string]any
			if err := json.Unmarshal([]byte(tc.schema), &schema); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{
				"name": "lookup", "description": "Look up a value", "eager_input_streaming": true, "input_schema": schema,
			}
			if tc.strict {
				want["strict"] = true
			}
			tool := strictLookupTool(tc.parameters, tc.mode)
			assertAnthropicStrictToolProjectionV160(t, tool, want, true)
			assertAnthropicStrictToolProjectionV160(t, tool, want, false)
		})
	}
}

func assertAnthropicStrictToolProjectionV160(t *testing.T, tool goai.Tool, want map[string]any, stopAtHook bool) {
	t.Helper()
	const base = "http://127.0.0.1:9"
	const responseBody = `{"type":"error","error":{"type":"permission_error","message":"scripted Anthropic rejection"}}`
	captureDone := errors.New("payload captured")
	model := strictAnthropicModel()
	model.Provider = "test-anthropic"
	model.BaseURL, model.Name, model.Reasoning, model.ContextWindow, model.MaxTokens = base, "Claude Opus 4.8", true, 200000, 32000
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("Use the tool")}, Tools: []goai.Tool{tool}}
	type payload struct {
		Tools []map[string]any `json:"tools"`
	}
	var hook, wire payload
	var order []string
	transport := anthropicThreadTestTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		order = append(order, "transport")
		if stopAtHook {
			return nil, fmt.Errorf("source hook did not stop before transport")
		}
		// SDK appends beta=true and has its own header metadata; the native
		// check only covers the real route and explicit key authority.
		if r.Method != http.MethodPost || strings.TrimSuffix(r.URL.String(), "?beta=true") != base+"/v1/messages" ||
			!slices.Equal(r.Header.Values("X-Api-Key"), []string{"test-key"}) || len(r.Header.Values("Authorization")) != 0 {
			return nil, fmt.Errorf("unexpected Anthropic fixture request %s %s or synthetic auth", r.Method, r.URL)
		}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(responseBody))}, nil
	})
	zero := 0
	_, err := goai.Complete(t.Context(), model, ctx, &goai.StreamOptions{
		APIKey: "test-key", CacheRetention: goai.CacheRetentionNone, MaxRetries: &zero, HTTPTransport: transport,
		Env: goai.ProviderEnv{"ANTHROPIC_AUTH_TOKEN": "", "PI_CACHE_RETENTION": ""},
		OnPayload: func(value any, _ *goai.Model) (any, error) {
			order = append(order, "hook")
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(encoded, &hook); err != nil {
				return nil, err
			}
			if stopAtHook {
				return nil, captureDone
			}
			return nil, nil
		},
	})
	wantTools := []map[string]any{want}
	if !reflect.DeepEqual(hook.Tools, wantTools) {
		t.Errorf("hook tools = %#v, want %#v", hook.Tools, wantTools)
	}
	if stopAtHook {
		if !slices.Equal(order, []string{"hook"}) || !errors.Is(err, captureDone) {
			t.Errorf("source hook order = %v, err = %v; want one callback, sentinel, zero HTTP", order, err)
		}
		return
	}
	if !slices.Equal(order, []string{"hook", "transport"}) || err == nil || !strings.Contains(err.Error(), "HTTP 403: "+responseBody) {
		t.Errorf("wire order = %v, err = %v; want one callback before one scripted 403", order, err)
	}
	if !reflect.DeepEqual(wire.Tools, wantTools) {
		t.Errorf("wire tools = %#v, want %#v", wire.Tools, wantTools)
	}
}

const anthropicDraft7TupleV161 = `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"pair":{"type":"array","items":[{"type":"string"},{"type":"string"}],"minItems":2,"maxItems":2}},"required":["pair"]}`

// Source: unchanged pi v1.7.0 anthropic-strict-tool-schema.test.ts:128–145.
// Retain the historical v1.6.1 control name.
func TestSourceV161AnthropicNonStrictTupleDialect(t *testing.T) {
	assertAnthropicNonStrictTupleV161(t, anthropicDraft7TupleV161, anthropicDraft7TupleV161)
}

func TestAnthropicNonStrictTupleRootFilteringV161(t *testing.T) {
	want := strings.TrimSuffix(anthropicDraft7TupleV161, "}") + `,"additionalProperties":false,"definitions":{"Other":{"type":"string"}}}`
	input := strings.TrimSuffix(want, "}") + `,"title":"keep out","allOf":[{}],"anyOf":[{}],"oneOf":[{}]}`
	assertAnthropicNonStrictTupleV161(t, input, want)
}

func assertAnthropicNonStrictTupleV161(t *testing.T, parameters, expectedSchema string) {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal([]byte(expectedSchema), &schema); err != nil {
		t.Fatal(err)
	}
	// Exact tool equality also proves the strict key is absent, not false.
	want := map[string]any{
		"name": "lookup", "description": "Look up a value", "eager_input_streaming": true, "input_schema": schema,
	}
	tool := strictLookupTool(parameters, "")
	assertAnthropicStrictToolProjectionV160(t, tool, want, true)
	assertAnthropicStrictToolProjectionV160(t, tool, want, false)
}
