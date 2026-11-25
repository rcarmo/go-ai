package durable

// Model transcript receipts contain output/diagnostics/details, not structured
// output (upstream appendToolResult contract). Validate the latter after hooks,
// report and discard invalid values without changing a model call's outcome.
func (h *Harness) validateModelStructuredResult(result ToolResult, offer toolOffer, report func(error)) ToolResult {
	if offer.StructuredOutputSchema != nil && result.wantsStructuredContent() {
		// Validate the projection of already-detached content without encoding
		// an image attachment a second time as bounded structured JSON. The
		// model receipt stores only the content, not this projected value.
		if err := checkSchema(offer.StructuredOutputSchema, nestedOutputValue(result), 0); err != nil {
			reportTaskError(report, reject("Structured output does not match the declared schema"))
		}
		return result
	}
	present := result.HasStructuredOutput || result.StructuredOutput != nil
	if offer.StructuredOutputSchema == nil && !present {
		return result
	}
	checked, invalid := validateNestedStructuredResult(result, offer, h.session.limits)
	if invalid {
		diagnostic := checked.Diagnostics[len(checked.Diagnostics)-1]
		reportTaskError(report, reject(diagnostic.Message))
		result.StructuredOutput = nil
		result.HasStructuredOutput = false
		return result
	}
	result.StructuredOutput = checked.StructuredOutput
	result.HasStructuredOutput = checked.HasStructuredOutput
	return result
}
