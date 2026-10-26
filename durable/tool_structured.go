package durable

// Model transcript receipts contain output/diagnostics/details, not structured
// output (upstream appendToolResult contract). Validate the latter after hooks,
// report and discard invalid values without changing a model call's outcome.
func (h *Harness) validateModelStructuredResult(result ToolResult, offer toolOffer, report func(error)) ToolResult {
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
