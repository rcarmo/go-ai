package goai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ClassifierQuestion struct {
	Type    string   `json:"type"`
	Choices []string `json:"choices,omitempty"`
}

type ClassifierContext struct {
	State     string                        `json:"state"`
	Questions map[string]ClassifierQuestion `json:"questions"`
}

type ClassifierAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Probability   float64            `json:"probability,omitempty"`
}

type ClassifierResult struct {
	Api          ClassifierApi               `json:"api"`
	Provider     ClassifierProvider          `json:"provider"`
	Model        string                      `json:"model"`
	Answers      map[string]ClassifierAnswer `json:"answers"`
	StopReason   StopReason                  `json:"stopReason"`
	Timestamp    int64                       `json:"timestamp"`
	Usage        *Usage                      `json:"usage,omitempty"`
	ErrorMessage string                      `json:"errorMessage,omitempty"`
}

type ClassifierPayloadHook func(payload map[string]any, model *ClassifierModel) (map[string]any, error)
type ClassifierResponseHook func(response ClassifierResponseMetadata, model *ClassifierModel) error

type ClassifierResponseMetadata struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
}

type ClassifierOptions struct {
	APIKey          string
	Temperature     float64
	Headers         map[string]string
	SuppressHeaders []string
	Context         context.Context
	Timeout         time.Duration
	TimeoutMs       int
	MaxRetries      int
	MaxRetryDelayMs int
	HTTPClient      *http.Client
	Env             ProviderEnv
	OnPayload       ClassifierPayloadHook
	OnResponse      ClassifierResponseHook
}

type ClassifierApiProvider struct {
	Api      ClassifierApi
	Classify func(model *ClassifierModel, ctx ClassifierContext, options *ClassifierOptions) (*ClassifierResult, error)
}

var classifierApiProviders = map[ClassifierApi]*ClassifierApiProvider{}

func RegisterClassifierApiProvider(p *ClassifierApiProvider) {
	if p == nil || p.Api == "" || p.Classify == nil {
		return
	}
	classifierRegistryMu.Lock()
	defer classifierRegistryMu.Unlock()
	classifierApiProviders[p.Api] = p
}

func GetClassifierApiProvider(api ClassifierApi) *ClassifierApiProvider {
	classifierRegistryMu.RLock()
	defer classifierRegistryMu.RUnlock()
	return classifierApiProviders[api]
}

func Classify(model *ClassifierModel, ctx ClassifierContext, opts *ClassifierOptions) (*ClassifierResult, error) {
	if model == nil {
		return classifierError(nil, fmt.Errorf("nil classifier model"), false), nil
	}
	p := GetClassifierApiProvider(model.Api)
	if p == nil {
		return classifierError(model, fmt.Errorf("no classifier provider registered"), false), nil
	}
	return p.Classify(model, ctx, opts)
}

func classifierBaseResult(model *ClassifierModel) *ClassifierResult {
	out := &ClassifierResult{Answers: map[string]ClassifierAnswer{}, StopReason: StopReasonStop, Timestamp: time.Now().UnixMilli()}
	if model != nil {
		out.Api = model.Api
		out.Provider = model.Provider
		out.Model = model.ID
	}
	return out
}

func classifierError(model *ClassifierModel, err error, aborted bool) *ClassifierResult {
	out := classifierBaseResult(model)
	out.StopReason = StopReasonError
	if aborted {
		out.StopReason = StopReasonAborted
	}
	if err != nil {
		out.ErrorMessage = err.Error()
	}
	return out
}

type systemOneTransport struct {
	api   ClassifierApi
	label string
	url   func(*ClassifierModel) string
}

func init() {
	RegisterClassifierApiProvider(&ClassifierApiProvider{Api: ClassifierApiTypeSafeSystemOne, Classify: func(model *ClassifierModel, ctx ClassifierContext, opts *ClassifierOptions) (*ClassifierResult, error) {
		return classifySystemOne(systemOneTransport{api: ClassifierApiTypeSafeSystemOne, label: "System One API", url: func(model *ClassifierModel) string { return strings.TrimRight(model.BaseURL, "/") + "/systemone" }}, model, ctx, opts)
	}})
	RegisterClassifierApiProvider(&ClassifierApiProvider{Api: ClassifierApiCloudflareWorkersAI, Classify: func(model *ClassifierModel, ctx ClassifierContext, opts *ClassifierOptions) (*ClassifierResult, error) {
		return classifySystemOne(systemOneTransport{api: ClassifierApiCloudflareWorkersAI, label: "Cloudflare Workers AI System One", url: func(model *ClassifierModel) string {
			return strings.TrimRight(ResolveCloudflareBaseURL(&Model{Provider: Provider(model.Provider), BaseURL: model.BaseURL}, ProviderEnvFromClassifierOptions(opts)), "/") + "/run/" + model.ID
		}}, model, ctx, opts)
	}})
	RegisterClassifierApiProvider(&ClassifierApiProvider{Api: ClassifierApiLlamaCPP, Classify: classifyLlamaCPP})
}

func ProviderEnvFromClassifierOptions(opts *ClassifierOptions) ProviderEnv {
	if opts == nil {
		return nil
	}
	return opts.Env
}

func classifySystemOne(transport systemOneTransport, model *ClassifierModel, classCtx ClassifierContext, opts *ClassifierOptions) (*ClassifierResult, error) {
	out := classifierBaseResult(model)
	ctx := context.Background()
	if opts != nil && opts.Context != nil {
		ctx = opts.Context
	}
	if model.Api != transport.api {
		return classifierError(model, fmt.Errorf("unsupported classifier API: %s", model.Api), false), nil
	}
	apiKey := ""
	if opts != nil {
		apiKey = opts.APIKey
	}
	if apiKey == "" {
		return classifierError(model, fmt.Errorf("no API key for provider: %s", model.Provider), false), nil
	}
	payload := map[string]any{"model": model.ID}
	wire := wireClassifierContext(classCtx)
	payload["state"] = wire.State
	payload["questions"] = wire.Questions
	if opts != nil && opts.OnPayload != nil {
		transformed, err := opts.OnPayload(payload, model)
		if err != nil {
			return classifierError(model, err, false), nil
		}
		if transformed != nil {
			payload = transformed
		}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, transport.url(model), bytes.NewReader(body))
	if err != nil {
		return classifierError(model, err, false), nil
	}
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	applyClassifierHeaders(req.Header, model, apiKey, opts)
	client := http.DefaultClient
	if opts != nil && opts.HTTPClient != nil {
		client = opts.HTTPClient
	}
	retryCfg := classifierRetryConfig(opts)
	if opts != nil && opts.Timeout > 0 {
		retryCfg.RequestTimeout = opts.Timeout
	}
	if opts != nil && opts.TimeoutMs > 0 {
		retryCfg.RequestTimeout = time.Duration(opts.TimeoutMs) * time.Millisecond
	}
	client = withClassifierTimeout(client, retryCfg)
	resp, err := DoProviderRequestWithRetry(ctx, client, req, retryCfg)
	if err != nil {
		return classifierError(model, err, ctx.Err() != nil), nil
	}
	defer resp.Body.Close()
	if opts != nil && opts.OnResponse != nil {
		_ = opts.OnResponse(ClassifierResponseMetadata{Status: resp.StatusCode, Headers: responseHeaders(resp.Header)}, model)
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return classifierError(model, fmt.Errorf("%s returned %d", transport.label, resp.StatusCode), false), nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return classifierError(model, err, false), nil
	}
	if usage := parseClassifierUsage(decoded["usage"], model); usage != nil {
		out.Usage = usage
	}
	answers, err := parseClassifierAnswers(transport.label, decoded["answers"], classCtx)
	if err != nil {
		out.StopReason = StopReasonError
		out.ErrorMessage = err.Error()
		return out, nil
	}
	out.Answers = answers
	return out, nil
}

func wireClassifierContext(ctx ClassifierContext) ClassifierContext {
	out := ClassifierContext{State: ctx.State, Questions: map[string]ClassifierQuestion{}}
	for id, q := range ctx.Questions {
		if q.Type == "bool" {
			q.Type = "noul"
		}
		out.Questions[id] = q
	}
	return out
}

func applyClassifierHeaders(h http.Header, model *ClassifierModel, apiKey string, opts *ClassifierOptions) {
	headers := map[string]string{"authorization": "Bearer " + apiKey, "content-type": "application/json"}
	for k, v := range model.Headers {
		headers[k] = v
	}
	if opts != nil {
		for k, v := range opts.Headers {
			headers[k] = v
		}
	}
	for k, v := range headers {
		h.Set(k, v)
	}
	if opts != nil {
		SuppressHeaders(h, opts.SuppressHeaders)
	}
}

func classifierRetryConfig(opts *ClassifierOptions) RetryConfig {
	if opts == nil || (opts.MaxRetries == 0 && opts.MaxRetryDelayMs == 0) {
		return NoRetryConfig()
	}
	cfg := DefaultRetryConfig()
	cfg.MaxRetries = opts.MaxRetries
	cfg.MaxRetryDelayMs = opts.MaxRetryDelayMs
	return cfg
}

func withClassifierTimeout(client *http.Client, cfg RetryConfig) *http.Client {
	if client != nil && cfg.RequestTimeout == 0 {
		return client
	}
	if client == nil {
		client = http.DefaultClient
	}
	copy := *client
	if cfg.RequestTimeout > 0 {
		copy.Timeout = cfg.RequestTimeout
	}
	return &copy
}

func responseHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

func parseClassifierUsage(value any, model *ClassifierModel) *Usage {
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	input := positiveInt(m["input_tokens"])
	output := positiveInt(m["output_tokens"])
	if input == 0 && output == 0 {
		return nil
	}
	usage := &Usage{Input: input, Output: output, TotalTokens: input + output}
	usage.Cost.Input = float64(input) / 1_000_000 * model.Cost.Input
	usage.Cost.Output = float64(output) / 1_000_000 * model.Cost.Output
	usage.Cost.Total = usage.Cost.Input + usage.Cost.Output
	return usage
}

func positiveInt(value any) int {
	if f, ok := value.(float64); ok && f > 0 {
		return int(f)
	}
	return 0
}

func parseClassifierAnswers(label string, value any, ctx ClassifierContext) (map[string]ClassifierAnswer, error) {
	answersMap, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s returned an unexpected response", label)
	}
	out := map[string]ClassifierAnswer{}
	for id, question := range ctx.Questions {
		raw, ok := answersMap[id].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s did not return an answer for %s", label, id)
		}
		typeValue, _ := raw["type"].(string)
		switch question.Type {
		case "choice":
			choice, ok := raw["choice"].(string)
			if typeValue != "choice" || !ok {
				return nil, fmt.Errorf("%s did not return a choice answer for %s", label, id)
			}
			probs, err := parseProbabilities(label, raw["probabilities"], id)
			if err != nil {
				return nil, err
			}
			conf, err := requiredFloat(label, raw["confidence"], "confidence for "+id)
			if err != nil {
				return nil, err
			}
			out[id] = ClassifierAnswer{Type: "choice", Choice: choice, Probabilities: probs, Confidence: conf}
		case "score":
			if typeValue != "score" {
				return nil, fmt.Errorf("%s did not return a score answer for %s", label, id)
			}
			score, err := requiredFloat(label, raw["score"], "score for "+id)
			if err != nil {
				return nil, err
			}
			conf, err := requiredFloat(label, raw["confidence"], "confidence for "+id)
			if err != nil {
				return nil, err
			}
			out[id] = ClassifierAnswer{Type: "score", Score: score, Confidence: conf}
		default:
			if typeValue != "noul" {
				return nil, fmt.Errorf("%s did not return a bool answer for %s", label, id)
			}
			prob, err := requiredFloat(label, raw["noul"], "probability for "+id)
			if err != nil {
				return nil, err
			}
			out[id] = ClassifierAnswer{Type: "bool", Probability: prob}
		}
	}
	return out, nil
}

func parseProbabilities(label string, value any, id string) (map[string]float64, error) {
	m, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s returned invalid probabilities for %s", label, id)
	}
	out := map[string]float64{}
	for k, v := range m {
		f, err := requiredFloat(label, v, "probability for "+id+"."+k)
		if err != nil {
			return nil, err
		}
		out[k] = f
	}
	return out, nil
}

func requiredFloat(label string, value any, field string) (float64, error) {
	f, ok := value.(float64)
	if !ok {
		return 0, fmt.Errorf("%s returned an invalid %s", label, field)
	}
	return f, nil
}
