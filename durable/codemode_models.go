package durable

func codemodeGlobalAllowed(start CodemodeStart, name string) bool {
	return codemodeCustomGlobal(start, name) != nil || codemodeDiscoveryName(name) || start.PaidModels && (name == "models.classify" || name == "models.generateImages" || codemodeModelCatalogName(name))
}
func codemodePaidArguments(call CodemodeCall) (string, JSON, error) {
	args, _ := call.Arguments["args"].([]any)
	var model, context any
	if len(args) > 0 {
		model = args[0]
	}
	if len(args) > 1 {
		context = args[1]
	}
	name := classifierToolName
	if call.Name == "models.generateImages" {
		name = imagesToolName
	} else if call.Name != "models.classify" {
		return "", nil, reject("unknown model global")
	}
	return name, JSON{"model": model, "context": context}, nil
}
