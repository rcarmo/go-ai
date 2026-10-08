package goai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

const openAIDecisionsLabel = "OpenAI Decisions"

func classifyOpenAIDecisions(model *ClassifierModel, classCtx ClassifierContext, opts *ClassifierOptions) (*ClassifierResult, error) {
	out := classifierBaseResult(model)
	ctx := context.Background()
	if opts != nil && opts.Context != nil {
		ctx = opts.Context
	}
	fail := func(err error) (*ClassifierResult, error) {
		out.Answers = map[string]ClassifierAnswer{}
		out.StopReason = StopReasonError
		if ctx.Err() != nil {
			out.StopReason = StopReasonAborted
		}
		var httpError *classifierHTTPError
		if errors.As(err, &httpError) && httpError.status == 504 {
			out.ErrorMessage = "OpenAI Decisions error (504): the request timed out at the gateway. Very large inputs (above roughly 600K tokens) currently exceed its time limit."
		} else {
			out.ErrorMessage = FormatProviderError(NormalizeProviderError(err), "OpenAI Decisions error")
		}
		return out, nil
	}
	if model.Api != ClassifierApiOpenAIDecisions {
		return fail(fmt.Errorf("unsupported classifier API: %s", model.Api))
	}
	if opts == nil || opts.APIKey == "" {
		return fail(fmt.Errorf("no API key for provider: %s", model.Provider))
	}
	if len(classCtx.Images) > 128 {
		return fail(fmt.Errorf("OpenAI Decisions accepts at most 128 images, got %d", len(classCtx.Images)))
	}
	state, err := json.Marshal(classCtx.State)
	if err != nil {
		return fail(err)
	}
	var input any = string(state)
	if len(classCtx.Images) > 0 {
		parts := []any{map[string]any{"type": "input_text", "text": string(state)}}
		for _, image := range classCtx.Images {
			parts = append(parts, map[string]any{"type": "input_image", "image_url": "data:" + image.MimeType + ";base64," + image.Data})
		}
		input = []any{map[string]any{"role": "user", "content": parts}}
	}
	ids := make([]string, 0, len(classCtx.Questions))
	for id := range classCtx.Questions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	questions := make([]any, 0, len(ids))
	for _, id := range ids {
		q := classCtx.Questions[id]
		criteria, err := q.normalizedCriteria()
		if err != nil {
			return fail(err)
		}
		q.Criteria = criteria
		wire := map[string]any{"type": q.Type, "name": id, "instructions": q.Instructions}
		switch q.Type {
		case "choice":
			criteria := q.Criteria.(map[string]string)
			keys := make([]string, 0, len(criteria))
			for key := range criteria {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			choices := make([]any, 0, len(keys))
			for _, key := range keys {
				choice := map[string]any{"value": key}
				if criteria[key] != "" {
					choice["description"] = criteria[key]
				}
				choices = append(choices, choice)
			}
			wire["choices"] = choices
		case "score":
			levels := []any{}
			for _, label := range q.Criteria.([]string) {
				levels = append(levels, map[string]any{"label": label})
			}
			wire["levels"] = levels
		case "bool":
			wire["type"] = "predicate"
			criteria := q.Criteria.(ClassifierBoolCriteria)
			meanings := []string{}
			if criteria.True != "" {
				meanings = append(meanings, "True means: "+criteria.True)
			}
			if criteria.False != "" {
				meanings = append(meanings, "False means: "+criteria.False)
			}
			if len(meanings) > 0 {
				wire["instructions"] = q.Instructions + "\n\n" + strings.Join(meanings, "\n")
			}
		}
		questions = append(questions, wire)
	}
	base, err := url.Parse(strings.TrimRight(model.BaseURL, "/") + "/")
	if err != nil {
		return fail(err)
	}
	endpoint := base.ResolveReference(&url.URL{Path: "decisions"}).String()
	value, err := postClassifierRequest(ctx, openAIDecisionsLabel, endpoint, model, map[string]any{"model": model.ID, "input": input, "questions": questions}, opts, []int{504})
	if err != nil {
		return fail(err)
	}
	body, ok := value.(map[string]any)
	if !ok {
		return fail(fmt.Errorf("OpenAI Decisions returned an unexpected response"))
	}
	out.Usage = parseClassifierUsage(body["usage"], model)
	out.Answers, err = parseDecisionsAnswers(body["answers"], classCtx)
	if err != nil {
		return fail(err)
	}
	return out, nil
}

func parseDecisionsAnswers(value any, ctx ClassifierContext) (map[string]ClassifierAnswer, error) {
	values, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("OpenAI Decisions returned an unexpected response")
	}
	byName := map[string]map[string]any{}
	for _, value := range values {
		if raw, ok := value.(map[string]any); ok {
			if name, ok := raw["name"].(string); ok {
				byName[name] = raw
			}
		}
	}
	normalized := map[string]any{}
	for id, q := range ctx.Questions {
		raw, ok := byName[id]
		if !ok {
			return nil, fmt.Errorf("OpenAI Decisions did not return an answer for %s", id)
		}
		copy := make(map[string]any, len(raw))
		for k, v := range raw {
			copy[k] = v
		}
		if raw["type"] == "refusal" {
			return nil, fmt.Errorf("OpenAI Decisions refused to answer %s", id)
		}
		switch q.Type {
		case "choice":
			entries, ok := raw["probabilities"].([]any)
			if !ok {
				return nil, fmt.Errorf("OpenAI Decisions returned invalid probabilities for %s", id)
			}
			probs := map[string]any{}
			for _, entry := range entries {
				pair, ok := entry.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("OpenAI Decisions returned invalid probabilities for %s", id)
				}
				key, ok := pair["value"].(string)
				if !ok {
					return nil, fmt.Errorf("OpenAI Decisions returned invalid probabilities for %s", id)
				}
				number, err := requiredFloat(openAIDecisionsLabel, pair["probability"], "probability for "+id+"."+key)
				if err != nil {
					return nil, err
				}
				probs[key] = number
			}
			copy["probabilities"] = probs
		case "bool":
			if raw["type"] != "predicate" {
				return nil, fmt.Errorf("OpenAI Decisions did not return a predicate answer for %s", id)
			}
			copy["type"], copy["noul"] = "noul", raw["probability"]
		}
		normalized[id] = copy
	}
	return parseClassifierAnswers(openAIDecisionsLabel, normalized, ctx)
}
