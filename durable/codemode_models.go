package durable

func codemodeGlobalAllowed(start CodemodeStart, name string) bool {
	return codemodeCustomGlobal(start, name) != nil || codemodeDiscoveryName(name) || start.PaidModels && (name == "models.classify" || name == "models.generateImages" || codemodeModelCatalogName(name))
}
func codemodePaidArguments(call CodemodeCall) (string, JSON, error) {
	args, ok := call.Arguments["args"].([]any)
	if !ok || len(args) != 2 {
		return "", nil, reject("model global requires model and context")
	}
	model, ok := args[0].(map[string]any)
	if !ok {
		return "", nil, reject("model global requires model reference")
	}
	provider, ok := model["provider"].(string)
	if !ok || provider == "" {
		return "", nil, reject("model global requires provider")
	}
	id, ok := model["id"].(string)
	if !ok || id == "" {
		return "", nil, reject("model global requires id")
	}
	name := classifierToolName
	if call.Name == "models.generateImages" {
		name = imagesToolName
	} else if call.Name != "models.classify" {
		return "", nil, reject("unknown model global")
	}
	return name, JSON{"provider": provider, "id": id, "context": args[1]}, nil
}
