package durable

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

// Source: pi v1.6.0 codemode.test.ts:448–472.
func TestSourceV160CodemodePublishedWorkerCallDetails(t *testing.T) {
	checkPublishedWorkerCallDetails(t, officialWorkerPackageRootV160(t))
}

// Source: pi v1.6.1 codemode.test.ts:448–472.
func TestSourceV161CodemodePublishedWorkerCallDetails(t *testing.T) {
	checkPublishedWorkerCallDetails(t, officialWorkerPackageRoot(t, "GOAI_CODEMODE_V161_PACKAGE_ROOT", "1.6.1"))
}

func checkPublishedWorkerCallDetails(t *testing.T, packageRoot string) {
	t.Helper()
	echo := wrapRegistration("echo")
	echo.Execute = func(_ context.Context, args any, _ *ToolAPI) (ToolResult, error) {
		return ToolResult{Content: args.(JSON)["text"].(string)}, nil
	}
	fails := wrapRegistration("fails")
	fails.Execute = func(context.Context, any, *ToolAPI) (ToolResult, error) {
		return ToolResult{Content: "permission denied", IsError: true}, nil
	}
	hold := wrapRegistration("hold")
	hold.Execute = func(ctx context.Context, _ any, _ *ToolAPI) (ToolResult, error) {
		<-ctx.Done()
		return ToolResult{}, ctx.Err()
	}
	const script = `await tools.echo({ text: "a" }); try { await tools.fails({}); } catch {} tools.hold({ id: 9 }); return 1;`
	_, _, result := runPublishedWorkerOutput(t, packageRoot, script, nil, 2, echo, fails, hold)
	data, err := json.Marshal(result.Details["calls"])
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		ID     int    `json:"id"`
		Name   string `json:"name"`
		Args   string `json:"args"`
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	var actual []struct {
		row
		DurationMs *int64 `json:"durationMs"`
	}
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatalf("decode call details %s: %v", data, err)
	}
	want := []row{
		{1, "echo", `{"text":"a"}`, "ok", ""},
		{2, "fails", `{}`, "error", "permission denied"},
		{3, "hold", `{"id":9}`, "cancelled", ""},
	}
	if len(actual) != len(want) {
		t.Fatalf("call details = %s; want three ordered rows", data)
	}
	for i, call := range actual {
		if !reflect.DeepEqual(call.row, want[i]) {
			t.Errorf("call %d = %+v; want %+v", i, call.row, want[i])
		}
	}
	if actual[0].DurationMs == nil || *actual[0].DurationMs < 0 {
		t.Errorf("successful call has no nonnegative duration: %s", data)
	}
}

// Supplemental: a schema value can reach the guest even though the nested
// tool failed. Call details record the actual nested outcome.
func TestCodemodeCallDetailsRecordsStructuredFailureV160(t *testing.T) {
	command := wrapRegistration("command")
	command.StructuredOutputSchema = JSON{
		"type": "object", "properties": JSON{"exitCode": JSON{"type": "integer"}}, "required": []any{"exitCode"},
	}
	command.Execute = func(context.Context, any, *ToolAPI) (ToolResult, error) {
		return ToolResult{
			IsError: true, StructuredOutput: JSON{"exitCode": 1}, HasStructuredOutput: true,
			Diagnostics: []ToolDiagnostic{{Severity: "error", Message: "command exited with status 1"}},
		}, nil
	}
	_, result := CheckV160PublishedWorkerOutput(t, `return (await tools.command({})).exitCode;`, nil, 1, command)
	if len(result.Content) < 2 || result.Content[1].Text != "1" {
		t.Fatalf("guest did not receive the schema value: %+v", result.Content)
	}
	rows, _ := result.Details["calls"].([]any)
	if len(rows) != 1 {
		t.Fatalf("call details = %+v, want one row", result.Details)
	}
	row := generationObject(rows[0])
	if row["status"] != "error" || row["error"] != "command exited with status 1" {
		t.Fatalf("call details = %+v, want the actual nested failure", row)
	}
}
