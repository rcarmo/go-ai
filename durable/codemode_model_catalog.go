package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
)

func codemodeModelCatalogName(name string) bool {
	return name == "models.getModelsOfType" || name == "models.getAvailableOfType" || name == "models.getModelOfType"
}
func (r *TaskRuntime) codemodeModelCatalog(ctx context.Context, call CodemodeCall) (any, error) {
	if !r.harness.options.PaidModels {
		return nil, reject("model catalogue globals revoked")
	}
	args, ok := call.Arguments["args"].([]any)
	if !ok || len(args) < 1 {
		return nil, reject("model catalogue requires type")
	}
	kind, ok := args[0].(string)
	if !ok || (kind != "chat" && kind != "image" && kind != "classifier") {
		return nil, reject("invalid model catalogue type")
	}
	access, err := r.Models()
	if err != nil {
		return nil, err
	}
	provider := goai.Provider("")
	if len(args) > 1 {
		value, ok := args[1].(string)
		if !ok {
			return nil, reject("model catalogue provider must be string")
		}
		provider = goai.Provider(value)
	}
	switch call.Name {
	case "models.getModelOfType":
		if len(args) != 3 {
			return nil, reject("model lookup requires type/provider/id")
		}
		id, ok := args[2].(string)
		if !ok || id == "" || provider == "" {
			return nil, reject("invalid model lookup reference")
		}
		return access.GetModelOfType(goai.ModelType(kind), provider, id)
	case "models.getModelsOfType":
		if len(args) > 2 {
			return nil, reject("invalid model list arguments")
		}
		return access.GetModelsOfType(goai.ModelType(kind), provider)
	case "models.getAvailableOfType":
		if len(args) > 2 {
			return nil, reject("invalid model availability arguments")
		}
		return access.GetAvailableOfType(ctx, goai.ModelType(kind), provider)
	}
	return nil, reject("unknown model catalogue global")
}
