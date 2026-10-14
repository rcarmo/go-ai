package durable

import goai "github.com/rcarmo/go-ai"

// Convert only known native classifier DTO fields; never run caller-provided
// MarshalJSON methods through the durable JSON admission boundary.
func classifierContextObject(input goai.ClassifierContext, l Limits) (JSON, error) {
	if input.State == nil || input.Questions == nil {
		return nil, reject("classifier state/questions required")
	}
	questions := JSON{}
	for key, question := range input.Questions {
		var criteria any
		switch value := question.Criteria.(type) {
		case goai.ClassifierBoolCriteria:
			criteria = JSON{"true": value.True, "false": value.False}
		case map[string]string:
			criteria = value
		case []string:
			criteria = value
		case []any:
			criteria = value
		case map[string]any:
			criteria = value
		default:
			return nil, reject("invalid classifier criteria")
		}
		questions[key] = JSON{"type": question.Type, "instructions": question.Instructions, "criteria": criteria}
	}
	return dtoObject(JSON{"state": input.State, "questions": questions, "images": input.Images}, l)
}
