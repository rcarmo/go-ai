package durable

import "testing"

func TestCodemode120LocalDeclarationReferencesBounded(t *testing.T) {
	schema := JSON{"type": "object", "$defs": JSON{"Name": JSON{"type": "string"}, "Self": JSON{"$ref": "#/$defs/Self"}, "a/b~c": JSON{"type": "integer"}}, "definitions": JSON{"Flag": JSON{"type": "boolean"}}, "properties": map[string]any{"name": JSON{"$ref": "#/$defs/Name"}, "flag": JSON{"$ref": "#/definitions/Flag"}, "self": JSON{"$ref": "#/$defs/Self"}, "remote": JSON{"$ref": "https://example.invalid/schema"}, "escaped": JSON{"$ref": "#/$defs/a~1b~0c"}}, "additionalProperties": false}
	want := "{\n  // @integer\n  escaped?: number;\n  flag?: boolean;\n  name?: string;\n  remote?: unknown;\n  self?: unknown;\n}"
	if got := codemodeSchemaType(schema, 0); got != want {
		t.Fatal(got, want)
	}
	for _, ref := range []string{"#/missing", "#/$defs/a~2b", "file:///etc/passwd"} {
		if got := codemodeSchemaType(JSON{"$ref": ref}, 0); got != "unknown" {
			t.Fatal(ref, got)
		}
	}
	// Repeated branching must exhaust a fixed expansion budget, never explode.
	root := JSON{"$ref": "#/$defs/A0"}
	defs := JSON{}
	for i := 0; i < 14; i++ {
		name := "A" + string(rune('a'+i))
		next := "A" + string(rune('a'+i+1))
		defs[name] = JSON{"anyOf": []any{JSON{"$ref": "#/$defs/" + next}, JSON{"$ref": "#/$defs/" + next}}}
	}
	root["$ref"] = "#/$defs/Aa"
	root["$defs"] = defs
	if got := codemodeSchemaType(root, 0); got == "" {
		t.Fatal("empty bounded result")
	}
}
