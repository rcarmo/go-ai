package google

import goai "github.com/rcarmo/go-ai"

func resolveGoogleFunctionCallingMode(tools []goai.Tool, choice goai.ToolChoice, supportsStrictMode bool) (string, error) {
	switch choice {
	case goai.ToolChoiceNone:
		return "NONE", nil
	case goai.ToolChoiceRequired:
		return "ANY", nil
	}
	for _, tool := range tools {
		strict, err := goai.ResolveJSONSchemaStrictSampling(tool, supportsStrictMode)
		if err != nil {
			return "", err
		}
		if strict != nil && *strict {
			return "VALIDATED", nil
		}
	}
	if choice != "" {
		return "AUTO", nil
	}
	return "", nil
}
