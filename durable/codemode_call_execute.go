package durable

import (
	"context"
	"fmt"
	"strings"

	goai "github.com/rcarmo/go-ai"
)

func (r *TaskRuntime) executeCodemodeCall(linked context.Context, key string, start CodemodeStart, call CodemodeCall, selectedGlobals []CodemodeGlobal, paid bool) (CodemodeDelivery, error) {
	var delivery CodemodeDelivery
	l := r.harness.session.limits
	if call.Global && (codemodeDiscoveryName(call.Name) || codemodeModelCatalogName(call.Name) || codemodeCustomGlobal(start, call.Name) != nil) {
		var value any
		var globalErr error
		if codemodeCustomGlobal(start, call.Name) != nil {
			value, globalErr = r.callCodemodeGlobal(linked, start, call, selectedGlobals)
		} else if codemodeModelCatalogName(call.Name) {
			value, globalErr = r.codemodeModelCatalog(linked, call)
		} else {
			value, globalErr = codemodeDiscoveryOutcome(start, call)
		}
		undefined := false
		if globalErr == nil {
			_, undefined = value.(codemodeUndefined)
			if undefined {
				value = nil
			}
		}
		if value != nil && globalErr == nil {
			object, e := dtoObject(JSON{"value": value}, l)
			if e != nil {
				return CodemodeDelivery{}, e
			}
			value = object["value"]
		}
		now, err := r.Now()
		if err != nil {
			return CodemodeDelivery{}, err
		}
		delivery = CodemodeDelivery{Seq: 0, Now: now, Call: call}
		if globalErr != nil {
			delivery.Error = globalErr.Error()
		} else {
			delivery.Value = value
			delivery.HasValue = !undefined
		}
	} else {
		name, arguments := call.Name, call.Arguments
		var argumentErr error
		if call.Global {
			name, arguments, argumentErr = codemodePaidArguments(call)
			// Persisted opt-in permits replay, never fresh paid effects after
			// current host revocation. A retained registry is not authority.
			if !paid {
				argumentErr = reject("paid model globals revoked")
			}
		}
		if argumentErr != nil {
			now, err := r.Now()
			if err != nil {
				return CodemodeDelivery{}, err
			}
			delivery = CodemodeDelivery{Seq: 0, Now: now, Call: call, Error: argumentErr.Error()}
		} else {
			// Confirmed deliveries replay above; fresh calls retain current authority.
			allowed := call.Global
			structured := call.Global
			if !allowed {
				tools, err := r.CodemodeTools(linked)
				if err != nil {
					return CodemodeDelivery{}, err
				}
				for _, tool := range tools {
					if tool.Name == name {
						allowed = true
						structured = tool.StructuredOutput
						break
					}
				}
			}
			result := ToolResult{IsError: true, Content: "Tool " + name + " is no longer callable"}
			if allowed {
				var err error
				if call.StreamingArguments && !call.Global {
					result, err = r.CallToolWithArguments(linked, fmt.Sprintf("%s.c%d", key, call.ID), name, goai.StreamingArgumentsJSON(arguments))
				} else {
					result, err = r.CallTool(linked, fmt.Sprintf("%s.c%d", key, call.ID), name, arguments)
				}
				if err != nil {
					return CodemodeDelivery{}, err
				}
			}
			clock, err := r.Now()
			if err != nil {
				return CodemodeDelivery{}, err
			}
			delivery = CodemodeDelivery{Seq: 0, Now: clock, Call: call}
			value, present, text := result.StructuredOutput, result.HasStructuredOutput, result.Content
			if r.definition == ToolTask() && !structured {
				// Canonical nested results collapse text and images for ordinary
				// callers. Codemode's schema-less tool declarations expose text.
				if present {
					text = codemodeNestedText(value)
				}
				value, present = text, true
			}
			if present && (structured || !result.IsError) {
				delivery.Value = value
				delivery.HasValue = true
			} else if result.IsError {
				if present {
					delivery.Properties = JSON{"structuredOutput": value}
				}
				delivery.Error = text
				if delivery.Error == "" {
					messages := []string{}
					for _, diagnostic := range result.Diagnostics {
						if diagnostic.Severity == "error" {
							messages = append(messages, diagnostic.Message)
						}
					}
					delivery.Error = strings.Join(messages, "\n")
				}
				if delivery.Error == "" {
					delivery.Error = "Tool " + call.Name + " failed"
				}
			} else {
				delivery.Value = result.Content
				delivery.HasValue = true
			}
		}
	}
	return delivery, nil
}

// A schema-less nested result is a string, a single image, or a list of
// text and image blocks. Only text reaches the Codemode script.
func codemodeNestedText(value any) string {
	switch output := value.(type) {
	case string:
		return output
	case []any:
		var text []string
		for _, item := range output {
			block := generationObject(item)
			if s, ok := block["text"].(string); ok && block["type"] == "text" {
				text = append(text, s)
			}
		}
		return strings.Join(text, "\n")
	default:
		return ""
	}
}
