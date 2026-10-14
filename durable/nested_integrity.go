package durable

// Validate the final candidate, including same-batch task/document admissions.
// Caller-owned key documents cannot point to another task, and results cannot
// appear before the child's decided outcome or disagree with that disposition.
func validateNestedReferences(s Snapshot, task Task, l Limits) error {
	record, err := CanonicalTask(task, l)
	if err != nil {
		return err
	}
	input, err := nestedInput(record, l)
	if err != nil {
		return err
	}
	if input.Parent != task.Owner || (!input.Sequential && !validNestedKey(input.Key) || input.Sequential && !validSequentialKey(input.Key)) || len(input.Key) > l.MaxRequestIDBytes || input.Name != input.Offer.Name || !toolCallerAllowed(input.Offer, "tools") {
		return reject("invalid nested task authority")
	}
	count := 0
	for _, doc := range s.Documents {
		if doc.Kind != nestedCallDocument || doc.Scope != "task" || doc.Owner != task.Owner || doc.Key != input.Key {
			continue
		}
		count++
		if !doc.Family || doc.Version != 1 {
			return reject("invalid nested call document")
		}
		var call nestedCallState
		if err := fromObject(doc.Value, &call, l); err != nil {
			return err
		}
		if call.Task != task.ID || call.Name != input.Name || !equalJSONValue(call.Arguments, input.Arguments) {
			return reject("nested call identity mismatch")
		}
		decided := taskHasDecidedOutcome(task)
		if (call.Result != nil) != decided {
			return reject("nested result/outcome mismatch")
		}
		if doc.Retired && !terminalStatus(s.Tasks[task.Owner].Status) {
			return reject("nested call retired before owner")
		}
		if call.Result != nil {
			result := call.Result
			outcome := task.Execution.Native.State.Outcome
			if outcome == nil || outcome.Result == nil {
				return reject("nested outcome result missing")
			}
			value, ok := outcome.Result.Value.(map[string]any)
			if !ok || value["key"] != input.Key {
				return reject("nested outcome key mismatch")
			}
			if result.Code != "" && (!result.IsError || result.HasStructuredOutput) {
				return reject("nested harness error result union")
			}
			if !validUsage(result.Usage) {
				return reject("invalid nested usage")
			}
			if result.Code == "" {
				if input.Offer.StructuredOutputSchema == nil {
					if result.IsError {
						if result.HasStructuredOutput || result.StructuredOutput != nil {
							return reject("invalid nested text error union")
						}
					} else if !result.HasStructuredOutput || result.StructuredOutput != result.Content {
						return reject("invalid nested text structured result")
					}
				} else if result.HasStructuredOutput {
					if err := checkSchema(input.Offer.StructuredOutputSchema, result.StructuredOutput, 0); err != nil {
						return reject("invalid settled nested structured output")
					}
				} else if !result.IsError {
					return reject("missing nested structured result")
				}
			}
			if call.Result.DurationMs < 0 {
				return reject("negative nested duration")
			}
			if err := validateToolDiagnostics(call.Result.Diagnostics, l); err != nil {
				return err
			}
		}
	}
	if count != 1 {
		return reject("nested call document missing/duplicated")
	}
	return nil
}
func validateNestedDocumentReplacement(old, next Document, l Limits) error {
	var before, after nestedCallState
	if err := fromObject(old.Value, &before, l); err != nil {
		return err
	}
	if err := fromObject(next.Value, &after, l); err != nil {
		return err
	}
	if before.Task != after.Task || before.Name != after.Name || !equalJSONValue(before.Arguments, after.Arguments) {
		return reject("nested call key identity changed")
	}
	if before.Result != nil {
		a, err := dtoObject(before.Result, l)
		if err != nil {
			return err
		}
		b, err := dtoObject(after.Result, l)
		if err != nil {
			return err
		}
		if !equalJSONValue(a, b) {
			return reject("nested result changed after settlement")
		}
	}
	return nil
}
