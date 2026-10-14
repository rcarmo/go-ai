package durable

import "strings"

// RenderCodemodeToolSignature describes a tool without granting authority.
// Missing schemas stay unknown; catalogued text-return tools have an explicit
// string output schema supplied by CodemodeTools.
func RenderCodemodeToolSignature(tool CodemodeToolDescription) string {
	input, output := "unknown", "unknown"
	if tool.InputSchema != nil {
		input = codemodeSchemaType(tool.InputSchema, 0)
	}
	if tool.OutputSchema != nil {
		output = codemodeSchemaType(tool.OutputSchema, 0)
	}
	return CodemodeIdentifier(tool.Name) + "(args: " + input + "): Promise<" + output + ">;"
}
func RenderCodemodeToolSample(tool CodemodeToolDescription) string {
	sample := "codemode tool declaration:\n```ts\ndeclare const tools: { " + RenderCodemodeToolSignature(tool) + " };\n```"
	if description := strings.TrimSpace(tool.Description); description != "" {
		return description + "\n\n" + sample
	}
	return sample
}
