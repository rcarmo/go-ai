package goai_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Locator: packages/schema/test/coerce-typebox.test.ts, 200 source cases.
// The pinned oracle records TypeBox's symbol-free JSON projection and the
// convertOnly result. Public Go Coerce is TypeBox pass 2 plus the schema
// primitive pass 3 described by the upstream coerce implementation.
func TestSchemaV170TypeBoxCoercionProjection(t *testing.T) {
	f, err := os.Open("testdata/schema_v170_typebox_coercion.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	mapped, jsOnly, passOnly := 0, 0, 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var row struct {
			Case       string          `json:"case"`
			Occurrence int             `json:"occurrence"`
			Status     string          `json:"status"`
			Reason     string          `json:"reason"`
			Schema     json.RawMessage `json:"schema"`
			Input      json.RawMessage `json:"input"`
			Result     json.RawMessage `json:"sourceResult"`
			Expected   json.RawMessage `json:"expected"`
			Public     json.RawMessage `json:"publicResult"`
			Check      *bool           `json:"check"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("%s/#%d", row.Case, row.Occurrence)
		if row.Status == "partial" {
			if row.Reason == "" {
				t.Fatal(name + ": partial row has no reason")
			}
			if len(row.Public) != 0 {
				passOnly++
				t.Run(name+"/public-pass", func(t *testing.T) {
					checkProjectedCoercion(t, row.Schema, row.Input, row.Public, row.Check)
				})
			} else {
				jsOnly++
				if len(row.Result) == 0 || len(row.Expected) == 0 {
					t.Fatal(name + ": partial JS row lost its source result")
				}
				if !bytes.Contains(append(append([]byte{}, row.Schema...), row.Input...), []byte(`$js`)) &&
					!bytes.Contains(row.Result, []byte(`$js`)) {
					t.Fatal(name + ": unexplained partial row")
				}
			}
			continue
		}
		if row.Status != "mapped" {
			t.Fatal(name + ": invalid disposition")
		}
		mapped++
		t.Run(name, func(t *testing.T) {
			checkProjectedCoercion(t, row.Schema, row.Input, row.Result, row.Check)
			if len(row.Expected) != 0 {
				if !bytes.Equal(canonicalJSON(t, row.Result), canonicalJSON(t, row.Expected)) {
					t.Fatalf("oracle result %s differs from assertion %s", row.Result, row.Expected)
				}
			}
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if mapped != 191 || passOnly != 3 || jsOnly != 35 {
		t.Fatalf("projection disposition = mapped %d, pass-only %d, JS-only %d", mapped, passOnly, jsOnly)
	}
}

func checkProjectedCoercion(t *testing.T, schema, input, want json.RawMessage, check *bool) {
	t.Helper()
	compiled, err := goai.CompileJSONSchema(schema, goai.JSONSchemaCompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(input, &value); err != nil {
		t.Fatal(err)
	}
	got, err := compiled.Coerce(value)
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonicalJSON(t, gotJSON), canonicalJSON(t, want)) {
		t.Fatalf("Coerce(%s, %s) = %s; want %s", schema, input, gotJSON, want)
	}
	if check != nil {
		valid, err := compiled.Check(got)
		if err != nil || valid != *check {
			t.Fatalf("Check(Coerce(...)) = %t, %v; want %t", valid, err, *check)
		}
	}
}

func canonicalJSON(t *testing.T, raw []byte) []byte {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	text, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return text
}
