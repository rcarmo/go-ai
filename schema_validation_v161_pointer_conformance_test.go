package goai_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type preparedFormatFixture struct {
	format string
	count  int
}

func TestSourceV161SchemaPreparedPointerFormatConformance(t *testing.T) {
	checkV161SchemaPreparedFormatConformance(t, preparedFormatFixture{"json-pointer", 40}, preparedFormatFixture{"relative-json-pointer", 26})
}

func TestSourceV161SchemaPreparedURIFormatConformance(t *testing.T) {
	checkV161SchemaPreparedFormatConformance(t, preparedFormatFixture{"uri-reference", 31}, preparedFormatFixture{"uri-template", 48})
}

func TestSourceV161SchemaPreparedIPv6FormatConformance(t *testing.T) {
	checkV161SchemaPreparedFormatConformance(t, preparedFormatFixture{"ipv6", 51})
}

// Compare both the original validity/error agreement and the exact records
// captured from the unchanged pinned explainer.
func checkV161SchemaPreparedFormatConformance(t *testing.T, fixtures ...preparedFormatFixture) {
	t.Helper()
	for _, fixture := range fixtures {
		t.Run(fixture.format, func(t *testing.T) {
			data, err := os.ReadFile("tests/testdata/upstream-main/" + fixture.format + "-format-2020-12-v161.json")
			if err != nil {
				t.Fatal(err)
			}
			var groups []struct {
				Schema json.RawMessage
				Tests  []struct {
					Description string
					Data        json.RawMessage
					Valid       bool
				}
			}
			if err := json.Unmarshal(data, &groups); err != nil {
				t.Fatal(err)
			}
			if len(groups) != 1 || len(groups[0].Tests) != fixture.count {
				t.Fatalf("expected one group containing %d vectors", fixture.count)
			}
			group := groups[0]
			asserted, err := goai.CompileJSONSchema(group.Schema, goai.JSONSchemaCompileOptions{JSONSchemaCheckOptions: goai.JSONSchemaCheckOptions{AssertFormats: true}})
			if err != nil {
				t.Fatal(err)
			}
			annotation := compileV160LeafSchema(t, string(group.Schema))
			for i, tc := range group.Tests {
				t.Run(fmt.Sprintf("%02d-%s", i, tc.Description), func(t *testing.T) {
					valid, err := asserted.Check(tc.Data)
					if err != nil {
						t.Fatal(err)
					}
					if valid != tc.Valid {
						t.Errorf("configured check(%s) = %t; want %t", tc.Data, valid, tc.Valid)
					}
					issues, err := asserted.Explain(tc.Data)
					if err != nil {
						t.Fatal(err)
					}
					if (len(issues) == 0) != valid {
						t.Errorf("diagnostics = %+v; disagree with check %t", issues, valid)
					}
					want := `[]`
					if !tc.Valid {
						want = `[{"path":"","keyword":"format","message":"expected format ` + fixture.format + `"}]`
					}
					assertV160StructuredDiagnostics(t, issues, want)
					assertV160PreparedSchemaCheck(t, annotation, string(tc.Data), true)
				})
			}
		})
	}
}

func TestSchemaPointerFormatsPreserveToolPolicy(t *testing.T) {
	checkSchemaFormatsPreserveToolPolicy(t, "json-pointer", "relative-json-pointer")
}

func TestSchemaURIFormatsPreserveToolPolicy(t *testing.T) {
	checkSchemaFormatsPreserveToolPolicy(t, "uri-reference", "uri-template")
}

func TestSchemaIPv6FormatPreservesToolPolicy(t *testing.T) {
	checkSchemaFormatsPreserveToolPolicy(t, "ipv6")
}

func checkSchemaFormatsPreserveToolPolicy(t *testing.T, formats ...string) {
	t.Helper()
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			schema := `{"type":"object","properties":{"p":{"type":"string","format":"` + format + `"}},"required":["p"]}`
			plain := decodeV160DiagnosticInput(t, schema).(map[string]any)
			input := map[string]any{"p": "not a pointer"}
			if !goai.CheckJSONSchemaValue(plain, input) {
				t.Fatal("legacy check now asserts the new format")
			}
			tool := goai.Tool{Name: "pointer", Parameters: json.RawMessage(schema)}
			value, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: input})
			if err != nil {
				t.Fatal(err)
			}
			assertV160StandaloneJSON(t, value, `{"p":"not a pointer"}`)
		})
	}
}
