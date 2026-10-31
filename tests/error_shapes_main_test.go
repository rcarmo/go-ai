package goai_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type errorBodyHostProbe struct{ calls *int }

func (p errorBodyHostProbe) MarshalJSON() ([]byte, error) {
	*p.calls++
	return []byte(`{"wrapper":true}`), nil
}

type namedErrorBodyMap map[string]any

func (namedErrorBodyMap) MarshalJSON() ([]byte, error) { panic("named host body marshaler invoked") }

func TestProviderErrorBodyFilteringMain(t *testing.T) {
	calls := 0
	for _, tc := range []struct {
		name    string
		body    any
		want    string
		present bool
	}{
		{name: "string-object-text", body: " {} ", want: "{}", present: true},
		{name: "plain-map", body: map[string]any{"error": "blocked"}, want: `{"error":"blocked"}`, present: true},
		{name: "string-map", body: map[string]string{"error": "blocked"}, want: `{"error":"blocked"}`, present: true},
		{name: "empty-map", body: map[string]any{}},
		{name: "nil-map", body: map[string]any(nil)},
		{name: "array", body: []any{"wrapper"}},
		{name: "bytes", body: []byte("metadata")},
		{name: "raw-json-wrapper", body: json.RawMessage(`{"error":"wrapped"}`)},
		{name: "number", body: 3},
		{name: "boolean", body: true},
		{name: "stream", body: strings.NewReader("body stream")},
		{name: "host-marshaler", body: errorBodyHostProbe{calls: &calls}},
		{name: "named-map", body: namedErrorBodyMap{"wrapper": true}},
		{name: "pointer-map", body: &map[string]any{"wrapper": true}},
		{name: "bom-whitespace", body: "\ufeff \t\n"},
		{name: "js-nel", body: "\u0085", want: "\u0085", present: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			norm := goai.NormalizeProviderError(providerErr{msg: "useful original error", status: 403, hasStatus: true, body: tc.body, hasBody: true})
			if norm.HasBody != tc.present || norm.Body != tc.want {
				t.Fatal("wrong body filtering", norm, tc.want)
			}
			if !tc.present && (norm.Message != "useful original error" || !norm.MessageCarriesBody || goai.FormatProviderError(norm) != "useful original error") {
				t.Fatal("wrapper replaced useful error", norm)
			}
		})
	}
	if calls != 0 {
		t.Fatal("top-level host authority invoked", calls)
	}
}

func TestSafeErrorJSONCyclesMain(t *testing.T) {
	object := map[string]any{}
	object["self"] = object
	array := make([]any, 4)
	array[0] = "before"
	array[1] = array
	array[2] = nil
	array[3] = map[string]any{"key": 1}
	mixed := make([]any, 6)
	mixed[0] = mixed
	mixed[1] = math.Inf(1)
	mixed[2] = math.Copysign(0, -1)
	mixed[3] = float64(1000000)
	mixed[4] = map[string]any(nil)
	mixed[5] = math.NaN()
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{{"cycle-map", object, "[object Object]"},
		{"cycle-map-pointer", &object, "[object Object]"},
		{"cycle-struct", struct{ Body any }{object}, "[object Object]"}, {"cycle-array", array, "before,,,[object Object]"},
		{"cycle-array-scalars", mixed, ",Infinity,0,1000000,,NaN"},
		{"nonfinite-array", []any{math.Inf(1)}, "[null]"}, {"nonfinite", math.Inf(1), "null"}, {"nonfinite-member", map[string]any{"n": math.NaN()}, `{"n":null}`}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := goai.SafeJSONStringify(tc.value); got != tc.want {
				t.Fatal("unsafe or wrong fallback", got, tc.want)
			}
		})
	}
	norm := goai.NormalizeProviderError(providerErr{msg: "original", status: 500, hasStatus: true, body: object, hasBody: true})
	if !norm.HasBody || norm.Body != "[object Object]" {
		t.Fatal("cycle normalization", norm)
	}
}

type throwingErrorJSON struct{}

func (throwingErrorJSON) MarshalJSON() ([]byte, error) { panic("JSON hook failed") }
func (throwingErrorJSON) String() string               { return "host fallback" }
func TestSafeErrorJSONHookFailureMain(t *testing.T) {
	if got := goai.SafeJSONStringify(throwingErrorJSON{}); got != "host fallback" {
		t.Fatal("failed JSON hook escaped", got)
	}
}
