package durable

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
	_ "github.com/rcarmo/go-ai/images/openrouter"
)

const classifierToolName = "pi.codemode.models.classify"
const imagesToolName = "pi.codemode.models.generate-images"

func paidModelRegistration(name string, kind goai.ModelType) ToolRegistration {
	return ToolRegistration{paidModel: true, Definition: goai.Tool{Name: name, Description: "Internal durable typed model call", Parameters: json.RawMessage(`{"type":"object","properties":{"provider":{"type":"string"},"id":{"type":"string"},"context":{"type":"object"}},"required":["provider","id","context"],"additionalProperties":false}`)}, Implementation: name, Version: 1, Callers: []string{"tools"}, ReplaySafe: false, StructuredOutputSchema: JSON{"type": "object", "additionalProperties": true}, Execute: func(ctx context.Context, args JSON, api *ToolAPI) (ToolResult, error) {
		provider, _ := args["provider"].(string)
		id, _ := args["id"].(string)
		ref := goai.TypedModelRef{Type: kind, Provider: provider, ID: id}
		access, err := api.Models()
		if err != nil {
			return ToolResult{}, err
		}
		value, err := access.GetModelOfType(kind, goai.Provider(provider), id)
		if err != nil {
			return ToolResult{}, err
		}
		if value == nil {
			return ToolResult{}, reject("unknown scoped model")
		}
		h := api.h
		var output any
		var usage *goai.Usage
		var stop string
		switch kind {
		case goai.ModelTypeClassifier:
			var input goai.ClassifierContext
			object, ok := args["context"].(map[string]any)
			if !ok {
				return ToolResult{}, reject("invalid classifier context")
			}
			data, err := encodeBounded(object, h.session.limits, h.session.limits.MaxRecordBytes)
			if err != nil {
				return ToolResult{}, err
			}
			if err = json.Unmarshal(data, &input); err != nil {
				return ToolResult{}, reject("invalid classifier context")
			}
			if err = validatePaidClassifierContext(input); err != nil {
				return ToolResult{}, err
			}
			options := &goai.ClassifierOptions{}
			if h.options.ClassifierRequestOptions != nil {
				resolved, e := h.options.ClassifierRequestOptions(ctx, ref)
				if e != nil {
					return ToolResult{}, e
				}
				if resolved != nil {
					copy := *resolved
					copy.Headers = cloneStringMap(resolved.Headers)
					copy.SuppressHeaders = append([]string(nil), resolved.SuppressHeaders...)
					options = &copy
				}
			}
			if options.OnPayload != nil {
				return ToolResult{}, reject("paid classifier payload rewrite hook unsupported")
			}
			options.Context = ctx
			result, err := goai.Classify(value.(*goai.ClassifierModel), input, options)
			if err != nil {
				return ToolResult{}, err
			}
			if result == nil {
				return ToolResult{}, reject("classifier returned nil")
			}
			answers := JSON{}
			invalidAnswers := false
			for key, answer := range result.Answers {
				switch answer.Type {
				case "choice":
					answers[key] = JSON{"type": answer.Type, "choice": answer.Choice, "probabilities": answer.Probabilities, "confidence": answer.Confidence}
				case "score":
					answers[key] = JSON{"type": answer.Type, "score": answer.Score}
				case "bool":
					answers[key] = JSON{"type": answer.Type, "probability": answer.Probability}
				default:
					invalidAnswers = true
				}
			}
			if !invalidAnswers {
				candidate, e := dtoObject(JSON{"answers": answers}, h.session.limits)
				if e != nil {
					invalidAnswers = true
				} else {
					var checked goai.ClassifierResult
					if e = fromObject(candidate, &checked, h.session.limits); e != nil {
						invalidAnswers = true
					}
				}
			}
			stopReason, errorMessage := result.StopReason, result.ErrorMessage
			if invalidAnswers {
				answers = JSON{}
				stopReason = goai.StopReasonError
				errorMessage = "Invalid classifier answers"
			}
			output = JSON{"api": result.Api, "provider": result.Provider, "model": result.Model, "answers": answers, "stopReason": stopReason, "timestamp": result.Timestamp, "usage": result.Usage, "errorMessage": errorMessage}
			usage = result.Usage
			stop = string(stopReason)
		case goai.ModelTypeImage:
			var input images.ImagesContext
			object, ok := args["context"].(map[string]any)
			if !ok {
				return ToolResult{}, reject("invalid image context")
			}
			data, err := encodeBounded(object, h.session.limits, h.session.limits.MaxRecordBytes)
			if err != nil {
				return ToolResult{}, err
			}
			if err = json.Unmarshal(data, &input); err != nil {
				return ToolResult{}, reject("invalid image context")
			}
			if err = validatePaidImageContext(input); err != nil {
				return ToolResult{}, err
			}
			options := &images.ImagesOptions{}
			if h.options.ImageRequestOptions != nil {
				resolved, e := h.options.ImageRequestOptions(ctx, ref)
				if e != nil {
					return ToolResult{}, e
				}
				if resolved != nil {
					copy := *resolved
					copy.Headers = cloneStringMap(resolved.Headers)
					copy.SuppressHeaders = append([]string(nil), resolved.SuppressHeaders...)
					options = &copy
				}
			}
			if options.OnPayload != nil || options.OnPayloadWithTelemetry != nil {
				return ToolResult{}, reject("paid image payload rewrite hook unsupported")
			}
			if options.Metadata != nil {
				value, e := copyObject(JSON(options.Metadata), h.session.limits)
				if e != nil {
					return ToolResult{}, e
				}
				options.Metadata = map[string]any(value)
			}
			options.Context = ctx
			options.Signal = nil
			model := value.(*goai.ImageModel)
			native := &images.ImagesModel{ID: model.ID, Name: model.Name, Api: images.ImagesApi(model.Api), Provider: images.ImagesProvider(model.Provider), Input: append([]string{}, model.Input...), Cost: model.Cost}
			result, err := images.GenerateImages(native, input, options)
			if err != nil {
				return ToolResult{}, err
			}
			if result == nil {
				return ToolResult{}, reject("image provider returned nil")
			}
			output = result
			usage = result.Usage
			stop = string(result.StopReason)
		default:
			return ToolResult{}, ErrUnsupported
		}
		object, err := dtoObject(output, h.session.limits)
		if err != nil {
			return ToolResult{}, err
		}
		return ToolResult{Content: fmt.Sprintf("%s result", kind), StructuredOutput: object, HasStructuredOutput: true, Usage: usage, IsError: stop != "stop"}, nil
	}}
}
func installPaidModelTools(registry *Registry) error {
	for _, registration := range []ToolRegistration{paidModelRegistration(classifierToolName, goai.ModelTypeClassifier), paidModelRegistration(imagesToolName, goai.ModelTypeImage)} {
		if existing, ok := registry.current(registration.Definition.Name); ok {
			if !existing.paidModel {
				return reject("reserved paid model tool collision")
			}
			continue
		}
		if err := registry.Register(registration); err != nil {
			return err
		}
	}
	return nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// Classify runs a paid classifier as a non-replay-safe nested durable task.
func (a *ModelAccess) Classify(ctx context.Context, key string, ref goai.TypedModelRef, input goai.ClassifierContext) (*goai.ClassifierResult, error) {
	if err := a.check(); err != nil {
		return nil, err
	}
	if ref.Type != "" && ref.Type != goai.ModelTypeClassifier {
		return nil, reject("wrong classifier model type")
	}
	if err := validatePaidClassifierContext(input); err != nil {
		return nil, err
	}
	object, err := classifierContextObject(input, a.runtime.harness.session.limits)
	if err != nil {
		return nil, err
	}
	result, err := a.runtime.CallTool(ctx, key, classifierToolName, JSON{"provider": ref.Provider, "id": ref.ID, "context": object})
	if err != nil {
		return nil, err
	}
	if !result.HasStructuredOutput {
		return nil, reject("classifier call has no structured result")
	}
	value, ok := result.StructuredOutput.(map[string]any)
	if !ok {
		return nil, reject("classifier result shape")
	}
	var out goai.ClassifierResult
	if err = fromObject(JSON(value), &out, a.runtime.harness.session.limits); err != nil {
		return nil, err
	}
	return &out, nil
}
func (a *ModelAccess) GenerateImages(ctx context.Context, key string, ref goai.TypedModelRef, input images.ImagesContext) (*images.AssistantImages, error) {
	if err := a.check(); err != nil {
		return nil, err
	}
	if ref.Type != "" && ref.Type != goai.ModelTypeImage {
		return nil, reject("wrong image model type")
	}
	if err := validatePaidImageContext(input); err != nil {
		return nil, err
	}
	object, err := dtoObject(input, a.runtime.harness.session.limits)
	if err != nil {
		return nil, err
	}
	result, err := a.runtime.CallTool(ctx, key, imagesToolName, JSON{"provider": ref.Provider, "id": ref.ID, "context": object})
	if err != nil {
		return nil, err
	}
	if !result.HasStructuredOutput {
		return nil, reject("image call has no structured result")
	}
	value, ok := result.StructuredOutput.(map[string]any)
	if !ok {
		return nil, reject("image result shape")
	}
	var out images.AssistantImages
	if err = fromObject(JSON(value), &out, a.runtime.harness.session.limits); err != nil {
		return nil, err
	}
	return &out, nil
}
