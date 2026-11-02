package goai_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestUpstreamMainValidationResources(t *testing.T) {
	type row struct {
		Name                    string
		Schema, Input, Expected json.RawMessage
		Valid                   bool
	}
	var f struct {
		Upstream         string
		Literal, Derived []row
	}
	data, err := os.ReadFile("testdata/upstream-main/validation-resources.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f.Upstream != "c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8" || len(f.Literal) != 2 || len(f.Derived) != 44 {
		t.Fatal("unexpected pinned resource fixture")
	}
	for _, g := range []struct {
		name string
		rows []row
	}{{"literal", f.Literal}, {"source-derived", f.Derived}} {
		t.Run(g.name, func(t *testing.T) {
			for _, tc := range g.rows {
				t.Run(tc.Name, func(t *testing.T) {
					t.Parallel()
					got, args, err := mainValidateJSON(t, string(tc.Schema), string(tc.Input))
					mainAssertValidationJSON(t, args, string(tc.Input))
					if (err == nil) != tc.Valid {
						t.Fatalf("accepted=%v want %v: %v", err == nil, tc.Valid, err)
					}
					if err == nil {
						mainAssertValidationJSON(t, got, string(tc.Expected))
					}
				})
			}
		})
	}
}

func TestUpstreamMainValidationResourcesOwnership(t *testing.T) {
	parameters := json.RawMessage(`{"type":"object","$defs":{"Node":{"$id":"Node","type":"object","properties":{"name":{"type":"string"},"children":{"type":"array","items":{"$ref":"Node"}}},"required":["name","children"]}},"properties":{"tree":{"$ref":"Node"}},"required":["tree"]}`)
	before := append([]byte(nil), parameters...)
	tool := goai.Tool{Name: "tree", Parameters: parameters}
	child := map[string]interface{}{"name": 1, "children": []interface{}{}}
	args := map[string]interface{}{"tree": map[string]interface{}{"name": "root", "children": []interface{}{child}}}
	got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	got["tree"].(map[string]interface{})["children"].([]interface{})[0].(map[string]interface{})["name"] = "changed"
	if child["name"] != 1 || !bytes.Equal(parameters, before) {
		t.Fatal("caller arguments/schema changed")
	}
}

func TestUpstreamMainValidationResourcesFreshCalls(t *testing.T) {
	tool := goai.Tool{Name: "fresh"}
	for _, tc := range []struct {
		kind string
		want interface{}
	}{{"integer", 1}, {"boolean", true}} {
		tool.Parameters = json.RawMessage(`{"type":"object","$defs":{"T":{"$id":"T","type":"` + tc.kind + `"}},"properties":{"v":{"$ref":"T"}},"required":["v"]}`)
		got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]interface{}{"v": "1"}})
		if err != nil {
			t.Fatal(err)
		}
		if got["v"] != tc.want {
			t.Fatalf("cross-call resource cache: %#v", got)
		}
	}
}

func TestUpstreamMainValidationResourcesNoFetch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"type":"integer"}`))
	}))
	defer server.Close()
	parameters, _ := json.Marshal(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"v": map[string]interface{}{"$ref": server.URL + "/schema"}}, "required": []string{"v"}})
	tool := goai.Tool{Name: "no-fetch", Parameters: parameters}
	if _, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]interface{}{"v": "1"}}); err == nil {
		t.Fatal("unregistered document accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("resolver fetched an unregistered document")
	}
}
