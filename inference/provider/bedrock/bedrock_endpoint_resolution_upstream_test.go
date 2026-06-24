package bedrock

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestUpstreamBedrockEndpointResolution(t *testing.T) {
	goai.RegisterBuiltinModels()
	euModel := goai.GetModel(goai.ProviderAmazonBedrock, "eu.anthropic.claude-sonnet-4-5-20250929-v1:0")
	if euModel == nil {
		t.Fatal("missing built-in EU Bedrock model")
	}
	if euModel.BaseURL != "https://bedrock-runtime.eu-central-1.amazonaws.com" {
		t.Fatalf("EU model base URL = %q, want %q", euModel.BaseURL, "https://bedrock-runtime.eu-central-1.amazonaws.com")
	}

	usModel := goai.GetModel(goai.ProviderAmazonBedrock, "us.anthropic.claude-opus-4-8")
	if usModel == nil {
		t.Fatal("missing built-in US Bedrock model")
	}
	if endpoint := getStandardBedrockEndpointRegion(usModel.BaseURL); endpoint == "" && usModel.BaseURL != "" {
		t.Fatalf("US model has unparsable standard endpoint %q", usModel.BaseURL)
	}

	tests := []struct {
		name           string
		model          *goai.Model
		opts           *goai.StreamOptions
		env            goai.ProviderEnv
		ambientProfile bool
		wantRegion     string
		wantEndpoint   bool
	}{
		{
			name:         "does not pin standard AWS endpoints when AWS_REGION is configured",
			model:        usModel,
			env:          goai.ProviderEnv{"AWS_REGION": "us-east-2"},
			wantRegion:   "us-east-2",
			wantEndpoint: false,
		},
		{
			name:         "derives region from a built-in EU endpoint when no region or profile is configured",
			model:        euModel,
			wantRegion:   "eu-central-1",
			wantEndpoint: true,
		},
		{
			name:         "handles missing regions for explicit profiles",
			model:        euModel,
			opts:         &goai.StreamOptions{Profile: "bedrock-profile"},
			wantRegion:   "eu-central-1",
			wantEndpoint: true,
		},
		{
			name:         "handles missing regions for scoped profiles",
			model:        euModel,
			env:          goai.ProviderEnv{"AWS_PROFILE": "scoped-bedrock-profile"},
			wantRegion:   "eu-central-1",
			wantEndpoint: true,
		},
		{
			name:           "handles missing regions for ambient profiles",
			model:          euModel,
			ambientProfile: true,
			wantRegion:     "",
			wantEndpoint:   false,
		},
		{
			name: "still passes custom Bedrock endpoints through to the SDK client",
			model: &goai.Model{
				ID:       usModel.ID,
				Provider: usModel.Provider,
				Api:      usModel.Api,
				BaseURL:  "https://bedrock-vpc.example.com",
			},
			env:          goai.ProviderEnv{"AWS_REGION": "us-west-2"},
			wantRegion:   "us-west-2",
			wantEndpoint: true,
		},
		{
			name: "extracts region from inference profile ARN regardless of AWS_REGION",
			model: &goai.Model{
				ID:       "arn:aws:bedrock:us-west-2:123456789012:application-inference-profile/abc123",
				Provider: goai.ProviderAmazonBedrock,
				Api:      goai.ApiBedrockConverseStream,
			},
			env:          goai.ProviderEnv{"AWS_REGION": "us-east-1"},
			wantRegion:   "us-west-2",
			wantEndpoint: false,
		},
		{
			name: "extracts region from GovCloud inference profile ARN",
			model: &goai.Model{
				ID:       "arn:aws-us-gov:bedrock:us-gov-west-1:123456789012:application-inference-profile/abc123",
				Provider: goai.ProviderAmazonBedrock,
				Api:      goai.ApiBedrockConverseStream,
			},
			env:          goai.ProviderEnv{"AWS_REGION": "us-east-1"},
			wantRegion:   "us-gov-west-1",
			wantEndpoint: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configuredRegion := getConfiguredBedrockRegion(tt.model, tt.opts, tt.env)
			endpointRegion := getStandardBedrockEndpointRegion(tt.model.BaseURL)
			useEndpoint := shouldUseExplicitBedrockEndpoint(tt.model.BaseURL, configuredRegion, tt.ambientProfile)
			region := bedrockARNRegion(tt.model.ID)
			if region == "" {
				region = configuredRegion
			}
			if region == "" && endpointRegion != "" && useEndpoint {
				region = endpointRegion
			}

			if region != tt.wantRegion {
				t.Fatalf("resolved region = %q, want %q", region, tt.wantRegion)
			}
			if useEndpoint != tt.wantEndpoint {
				t.Fatalf("use explicit endpoint = %v, want %v", useEndpoint, tt.wantEndpoint)
			}
		})
	}
}
