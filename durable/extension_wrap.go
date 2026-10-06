package durable

import (
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
)

func reportSelectionError(reports []func(error), err error) {
	for _, report := range reports {
		reportTaskError(report, err)
	}
}

func applyToolWrap(wrap ExtensionWrap, tool registeredTool, limits Limits) (next registeredTool, err error) {
	defer func() {
		if recover() != nil {
			err = reject("tool wrapper panic")
		}
	}()
	schema, err := copyObject(tool.offer.Schema, limits)
	if err != nil {
		return next, err
	}
	parameters, err := json.Marshal(schema)
	if err != nil {
		return next, err
	}
	reg := ToolRegistration{Definition: goai.Tool{Name: tool.offer.Name, Description: tool.offer.Description, Parameters: parameters}, Implementation: tool.offer.Implementation, Version: tool.offer.Version, ReplaySafe: tool.offer.ReplaySafe, ExecutionMode: tool.offer.ExecutionMode, PrepareArguments: tool.prepare, Validator: tool.validator, Execute: tool.execute}
	if tool.offer.OutputLimits != nil {
		output := *tool.offer.OutputLimits
		reg.OutputLimits = &output
	}
	reg, err = wrap.WrapTool(reg)
	if err != nil {
		return next, err
	}
	if reg.Definition.Name != wrap.Tool {
		return next, reject(fmt.Sprintf("wrapper renamed tool %s to %s", wrap.Tool, reg.Definition.Name))
	}
	trial := NewRegistry()
	if err = trial.Register(reg); err != nil {
		return next, err
	}
	return trial.tools[wrap.Tool], nil
}

func applySectionWrap(wrap ExtensionWrap, section PromptSection) (next PromptSection, err error) {
	defer func() {
		if recover() != nil {
			err = reject("section wrapper panic")
		}
	}()
	next, err = wrap.WrapSection(section)
	if err != nil {
		return next, err
	}
	if next.Key != wrap.Section || next.Render == nil {
		return next, reject("wrapper renamed or invalidated section")
	}
	return next, nil
}
