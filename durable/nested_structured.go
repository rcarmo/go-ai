package durable

// nestedOutputValue preserves the reference schema-less text/image shape after
// bounding. Multiple blocks remain a list rather than flattened text.
func nestedOutputValue(result ToolResult) any {
	if len(result.Blocks) == 0 {
		return result.Content
	}
	values := make([]any, len(result.Blocks))
	for i, block := range result.Blocks {
		if block.Type == "text" {
			values[i] = JSON{"type": "text", "text": block.Text}
		} else {
			values[i] = JSON{"type": "image", "mimeType": block.MimeType, "data": block.Data}
		}
	}
	if len(values) == 1 {
		if result.Blocks[0].Type == "text" {
			return result.Blocks[0].Text
		}
		return values[0]
	}
	return values
}

// Structured output is validated after hooks and bounding. The registration
// accepts only the native schema subset; unsupported schemas fail registration
// rather than bypassing result validation. Explicit JSON null retains presence.
func validateNestedStructuredResult(result ToolResult, offer toolOffer, limits Limits) (ToolResult, bool) {
	present := result.HasStructuredOutput || result.StructuredOutput != nil
	invalid := func(message string) (ToolResult, bool) {
		result.StructuredOutput = nil
		result.HasStructuredOutput = false
		result.IsError = true
		result.Commit = nil
		result.Diagnostics = append(result.Diagnostics, ToolDiagnostic{Severity: "error", Code: "invalid_structured_output", Message: message})
		return result, true
	}
	if offer.StructuredOutputSchema == nil {
		if present {
			if !result.IsError {
				return invalid("Structured output returned without a declared schema")
			}
			value, err := ownJSONValue(result.StructuredOutput, limits)
			if err != nil {
				return invalid("Structured output is not bounded JSON")
			}
			result.StructuredOutput = value
			result.HasStructuredOutput = true
			return result, false
		}
		// Schema-less failures have no structured value: codemode rejects them.
		if result.IsError {
			return result, false
		}
		result.StructuredOutput = nestedOutputValue(result)
		result.HasStructuredOutput = true
		return result, false
	}
	if !present {
		if result.IsError {
			return result, false
		}
		return invalid("Structured output missing for a schema tool")
	}
	value, err := ownJSONValue(result.StructuredOutput, limits)
	if err != nil {
		return invalid("Structured output is not bounded JSON")
	}
	if err = checkSchema(offer.StructuredOutputSchema, value, 0); err != nil {
		return invalid("Structured output does not match the declared schema")
	}
	result.StructuredOutput = value
	result.HasStructuredOutput = true
	return result, false
}
