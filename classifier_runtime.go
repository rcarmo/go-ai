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
	if len(ctx.Images) > 0 {
		acceptsImages := false
		for _, input := range model.Input {
			acceptsImages = acceptsImages || input == "image"
		}
		if !acceptsImages {
			return classifierError(model, fmt.Errorf("model %s/%s does not accept image input", model.Provider, model.ID), false), nil
		}
	}
	// Validate and normalize the public JSON union before any provider request.
	encoded, err := json.Marshal(ctx)
	if err != nil {
		return classifierError(model, err, false), nil
	}
	var normalized ClassifierContext
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return classifierError(model, err, false), nil
	}
	return p.Classify(model, normalized, opts)
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
			return strings.TrimRight(ResolveCloudflareBaseURL(&Model{Provider: Provider(model.Provider), BaseURL: model.BaseURL}, ProviderEnvFromClassifierOptions(opts)), "/") + "/run"
		}}, model, ctx, opts)
	}})
	RegisterClassifierApiProvider(&ClassifierApiProvider{Api: ClassifierApiLlamaCPP, Classify: classifyLlamaCPP})
	RegisterClassifierApiProvider(&ClassifierApiProvider{Api: ClassifierApiOpenAIDecisions, Classify: classifyOpenAIDecisions})
}

func ProviderEnvFromClassifierOptions(opts *ClassifierOptions) ProviderEnv {
	if opts == nil {
		return nil
	}
	return opts.Env
}

func classifySystemOne(transport systemOneTransport, model *ClassifierModel, classCtx ClassifierContext, opts *ClassifierOptions) (*ClassifierResult, error) {
	out := classifierBaseResult(model)
	if len(classCtx.Images) > 0 {
		return classifierError(model, fmt.Errorf("%s does not support image input", transport.label), false), nil
	}
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
	if transport.api == ClassifierApiCloudflareWorkersAI {
		payload["input"] = wire
	} else {
		payload["state"] = wire["state"]
		payload["questions"] = wire["questions"]
	}
	value, err := postClassifierRequest(ctx, transport.label, transport.url(model), model, payload, opts, nil)
	if err != nil {
		return classifierError(model, err, ctx.Err() != nil), nil
	}
	decoded, ok := value.(map[string]any)
	if !ok {
		return classifierError(model, fmt.Errorf("%s returned an unexpected response", transport.label), false), nil
	}
	if transport.api == ClassifierApiCloudflareWorkersAI {
		result, ok := decoded["result"].(map[string]any)
		if !ok || decoded["success"] == false {
			return classifierError(model, fmt.Errorf("%s returned an unexpected response", transport.label), false), nil
		}
		if _, direct := result["answers"]; direct {
			decoded = result
		} else {
			if result["state"] != "Completed" {
				return classifierError(model, fmt.Errorf("%s returned an unexpected response", transport.label), false), nil
			}
			decoded, ok = result["result"].(map[string]any)
			if !ok {
				return classifierError(model, fmt.Errorf("%s returned an unexpected response", transport.label), false), nil
			}
		}
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

func wireClassifierContext(ctx ClassifierContext) map[string]any {
	questions := map[string]any{}
	for id, q := range ctx.Questions {
		typeName := q.Type
		if typeName == "bool" {
			typeName = "noul"
		}
		questions[id] = map[string]any{"type": typeName, "instructions": q.Instructions, "criteria": q.Criteria}
	}
	return map[string]any{"state": ctx.State, "questions": questions}
}

func applyClassifierHeaders(h http.Header, model *ClassifierModel, apiKey string, opts *ClassifierOptions) {
	h.Set("Content-Type", "application/json")
	if apiKey != "" {
		h.Set("Authorization", "Bearer "+apiKey)
	}
	// Apply each precedence layer to http.Header directly: map iteration must
	// not choose between differently cased spellings of the same header.
	ApplyHeaders(h, model.Headers)
	if opts != nil {
		ApplyHeaders(h, opts.Headers)
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
	if _, hasInput := m["input_tokens"]; !hasInput {
		if _, hasOutput := m["output_tokens"]; !hasOutput {
			return nil
		}
	}
	usage := &Usage{Input: input, Output: output, TotalTokens: input + output}
	usage.Cost = CalculateCost(&Model{Cost: model.Cost}, usage)
	return usage
}

func positiveInt(value any) int {
	f, err := requiredFloat("classifier", value, "token count")
	if err == nil && f > 0 && f < float64(int(^uint(0)>>1)) {
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
		case "bool":
			if typeValue != "noul" {
				return nil, fmt.Errorf("%s did not return a bool answer for %s", label, id)
			}
			prob, err := requiredFloat(label, raw["noul"], "probability for "+id)
			if err != nil {
				return nil, err
			}
			out[id] = ClassifierAnswer{Type: "bool", Probability: prob}
		default:
			return nil, fmt.Errorf("unknown classifier question type %q", question.Type)
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
	if number, isNumber := value.(json.Number); isNumber {
		var err error
		f, err = number.Float64()
		ok = err == nil
	}
	if !ok || !finiteClassifierNumber(f) {
		return 0, fmt.Errorf("%s returned an invalid %s", label, field)
	}
	return f, nil
}

// postClassifierRequest shares the bounded HTTP/retry/timeout/hook boundary.
func postClassifierRequest(ctx context.Context, label, endpoint string, model *ClassifierModel, payload map[string]any, opts *ClassifierOptions, noRetryStatuses []int) (any, error) {
	apiKey := ""
	if opts != nil {
		apiKey = opts.APIKey
	}
	if opts != nil && opts.OnPayload != nil {
		transformed, err := opts.OnPayload(payload, model)
		if err != nil {
			return nil, err
		}
		if transformed != nil {
			payload = transformed
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
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
	if len(noRetryStatuses) > 0 {
		retryCfg.RetryableStatuses = []int{408, 409, 429}
		for status := 500; status < 600; status++ {
			skip := false
			for _, excluded := range noRetryStatuses {
				skip = skip || status == excluded
			}
			if !skip {
				retryCfg.RetryableStatuses = append(retryCfg.RetryableStatuses, status)
			}
		}
	}
	client = withClassifierTimeout(client, retryCfg)
	resp, err := DoProviderRequestWithRetry(ctx, client, req, retryCfg)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, fmt.Errorf("%s could not read response", label)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &classifierHTTPError{status: resp.StatusCode, body: string(data), label: label}
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	// Validate the entire document, but defer numeric conversion until field
	// parsing so an overflowing answer still retains reported billed usage.
	if !json.Valid(data) {
		return nil, fmt.Errorf("%s returned invalid JSON", label)
	}
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%s returned invalid JSON", label)
	}
	// Match upstream: only successfully read/decoded 2xx replies are observed,
	// including valid JSON whose answers or transport envelope are malformed.
	if opts != nil && opts.OnResponse != nil {
		if err := opts.OnResponse(ClassifierResponseMetadata{Status: resp.StatusCode, Headers: responseHeaders(resp.Header)}, model); err != nil {
			return nil, err
		}
	}
	return value, nil
}

type classifierHTTPError struct {
	status      int
	body, label string
}

func (e *classifierHTTPError) Error() string                    { return fmt.Sprintf("%s returned %d", e.label, e.status) }
func (e *classifierHTTPError) ProviderErrorStatus() (int, bool) { return e.status, true }
func (e *classifierHTTPError) ProviderErrorBody() (any, bool)   { return e.body, true }

var _ ProviderErrorShape = (*classifierHTTPError)(nil)
