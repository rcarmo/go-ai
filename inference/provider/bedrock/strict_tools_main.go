package bedrock

import (
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/stricttools"
)

func bedrockStrictSupported(model *goai.Model) bool {
	return model != nil && model.BedrockCompat != nil && model.BedrockCompat.SupportsStrictMode != nil && *model.BedrockCompat.SupportsStrictMode
}
func bedrockPreferredStrict(tool goai.Tool, supported bool) bool {
	c := tool.ConstrainedSampling
	if c == nil || c.Type != "json_schema" || c.Strict != "prefer" {
		return false
	}
	strict, err := goai.ResolveJSONSchemaStrictSampling(tool, supported)
	return err == nil && strict != nil && *strict
}
func validateBedrockStrictTools(model *goai.Model, tools []goai.Tool) (bool, error) {
	found := false
	for _, tool := range tools {
		if _, err := goai.ResolveJSONSchemaStrictSampling(tool, bedrockStrictSupported(model)); err != nil {
			return false, err
		}
		found = found || bedrockPreferredStrict(tool, bedrockStrictSupported(model))
	}
	return found, nil
}
func bedrockStrictLimit(err error) bool {
	var validation *types.ValidationException
	return errors.As(err, &validation) && stricttools.LimitMessage(validation.ErrorMessage())
}
func buildBedrockToolConfig(model *goai.Model, tools []goai.Tool, disablePreferred bool, opts *goai.StreamOptions) *types.ToolConfiguration {
	if len(tools) == 0 {
		return nil
	}
	config := &types.ToolConfiguration{}
	if opts != nil {
		switch opts.ToolChoice {
		case goai.ToolChoiceAuto:
			config.ToolChoice = &types.ToolChoiceMemberAuto{}
		case goai.ToolChoiceRequired:
			config.ToolChoice = &types.ToolChoiceMemberAny{}
		}
	}
	supported := bedrockStrictSupported(model)
	for _, tool := range tools {
		parameters := tool.Parameters
		var strictFlag *bool
		if strict, err := goai.ResolveJSONSchemaStrictSampling(tool, supported); err == nil && strict != nil && *strict && !(disablePreferred && bedrockPreferredStrict(tool, supported)) {
			if converted, err := goai.JSONSchemaToolParameters(tool, true); err == nil {
				parameters = converted
				strictFlag = aws.Bool(true)
			}
		}
		config.Tools = append(config.Tools, &types.ToolMemberToolSpec{Value: types.ToolSpecification{Name: aws.String(tool.Name), Description: aws.String(tool.Description), Strict: strictFlag, InputSchema: &types.ToolInputSchemaMemberJson{Value: mustDocument(parameters)}}})
	}
	return config
}
