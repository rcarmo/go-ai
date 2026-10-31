package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"testing"
)

// These oracle outputs were computed by the pinned upstream catalog helpers.
// Go consumes their generated metadata; it does not fetch raw provider catalogs.
func TestPinnedReasoningMetadataGenerationMain(t *testing.T) {
	var fixture struct {
		Upstream string `json:"upstream"`
		Cases    []struct {
			Name             string             `json:"name"`
			ThinkingLevelMap map[string]*string `json:"thinkingLevelMap"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/reasoning_main.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Upstream != "4b535d57ea8863dbba44b89872146523da271786" || len(fixture.Cases) != 10 {
		t.Fatal("unexpected oracle provenance or case count")
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			record := map[string]any{"id": tc.Name, "provider": "oracle", "api": "openai-completions", "type": "chat", "reasoning": true}
			if tc.ThinkingLevelMap != nil {
				record["thinkingLevelMap"] = tc.ThinkingLevelMap
			}
			raw, err := json.Marshal(map[string]any{"openai-completions": map[string]any{"chat:" + tc.Name: record}})
			if err != nil {
				t.Fatal(err)
			}
			flat := flattenProviderDataJSON(raw, "MODELS")
			if flat == "" {
				t.Fatal("provider hydration rejected oracle fixture")
			}
			var models map[string]modelEntry
			if err = json.Unmarshal([]byte(flat), &models); err != nil {
				t.Fatal(err)
			}
			if len(models) != 1 || !reflect.DeepEqual(models[tc.Name].ThinkingLevelMap, tc.ThinkingLevelMap) {
				t.Fatalf("hydration changed metadata: %#v", models)
			}
			source := generateGoSource(map[string]map[string]modelEntry{"oracle": models}, 1, "chat")
			file, err := parser.ParseFile(token.NewFileSet(), "generated.go", source, 0)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]*string
			fields := 0
			ast.Inspect(file, func(n ast.Node) bool {
				kv, ok := n.(*ast.KeyValueExpr)
				if !ok {
					return true
				}
				field, ok := kv.Key.(*ast.Ident)
				if !ok || field.Name != "ThinkingLevelMap" {
					return true
				}
				fields++
				literal, ok := kv.Value.(*ast.CompositeLit)
				if !ok {
					t.Fatal("metadata is not a map literal")
				}
				got = make(map[string]*string)
				for _, element := range literal.Elts {
					entry, ok := element.(*ast.KeyValueExpr)
					if !ok {
						t.Fatal("missing map key")
					}
					key, ok := entry.Key.(*ast.BasicLit)
					if !ok {
						t.Fatal("map key not a string")
					}
					name, err := strconv.Unquote(key.Value)
					if err != nil {
						t.Fatal(err)
					}
					if value, ok := entry.Value.(*ast.Ident); ok && value.Name == "nil" {
						got[name] = nil
						continue
					}
					value, ok := entry.Value.(*ast.CallExpr)
					if !ok || len(value.Args) != 1 {
						t.Fatal("unexpected effort expression")
					}
					fn, ok := value.Fun.(*ast.Ident)
					if !ok || fn.Name != "strPtr" {
						t.Fatal("unexpected effort constructor")
					}
					argument, ok := value.Args[0].(*ast.BasicLit)
					if !ok {
						t.Fatal("effort is not a string")
					}
					effort, err := strconv.Unquote(argument.Value)
					if err != nil {
						t.Fatal(err)
					}
					got[name] = &effort
				}
				return false
			})
			expectedFields := 0
			if tc.ThinkingLevelMap != nil {
				expectedFields = 1
			}
			if fields != expectedFields || !reflect.DeepEqual(got, tc.ThinkingLevelMap) {
				t.Fatalf("emitted metadata changed: got %#v (%d fields), want %#v", got, fields, tc.ThinkingLevelMap)
			}
		})
	}
}
