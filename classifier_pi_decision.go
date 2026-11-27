package goai

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
)

const piDecisionLabel = "Pi Decision"

func classifyPiDecision(model *ClassifierModel, input ClassifierContext, opts *ClassifierOptions) (*ClassifierResult, error) {
	out := classifierBaseResult(model)
	ctx := context.Background()
	if opts != nil && opts.Context != nil {
		ctx = opts.Context
	}
	fail := func(err error) (*ClassifierResult, error) {
		out.StopReason = StopReasonError
		if ctx.Err() != nil {
			out.StopReason = StopReasonAborted
		}
		out.ErrorMessage = FormatProviderError(NormalizeProviderError(err), piDecisionLabel+" error")
		return out, nil
	}
	if model.Api != ClassifierApiPiDecision {
		return fail(fmt.Errorf("unsupported classifier API: %s", model.Api))
	}
	if len(input.Images) != 0 && !slices.Contains(model.Input, "image") {
		return fail(piDecisionUnsupportedImages(model))
	}
	if opts == nil || opts.APIKey == "" {
		return fail(fmt.Errorf("no API key for provider: %s", model.Provider))
	}
	endpoint, err := url.JoinPath(model.BaseURL, "decision")
	if err != nil {
		return fail(err)
	}
	wireContext := map[string]any{"state": input.State, "questions": input.Questions}
	if len(input.Images) != 0 {
		wireContext["images"] = input.Images
	}
	payload := map[string]any{"model": model.ID, "context": wireContext}
	if opts.SessionID != nil {
		payload["options"] = map[string]any{"sessionId": *opts.SessionID}
	}
	value, err := postClassifierRequestWithResponsePhase(ctx, piDecisionLabel, endpoint, model, payload, opts, nil, true)
	if err != nil {
		return fail(err)
	}
	body, ok := value.(map[string]any)
	if !ok {
		return fail(fmt.Errorf("%s returned an unexpected response", piDecisionLabel))
	}
	out.ResponseID, _ = body["responseId"].(string)
	out.Usage = parsePiDecisionUsage(body["usage"], model)
	answers, err := parsePiDecisionAnswers(body["answers"], input)
	if err != nil {
		return fail(err)
	}
	out.Answers = answers
	return out, nil
}

func piDecisionUnsupportedImages(model *ClassifierModel) error {
	return fmt.Errorf("%s model %s does not support image input", piDecisionLabel, model.ID)
}

func parsePiDecisionUsage(value any, model *ClassifierModel) *Usage {
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	_, inputErr := requiredFloat(piDecisionLabel, m["input"], "input")
	_, outputErr := requiredFloat(piDecisionLabel, m["output"], "output")
	cost, _ := m["cost"].(map[string]any)
	billedTotal, costErr := requiredFloat(piDecisionLabel, cost["total"], "cost.total")
	if inputErr != nil && outputErr != nil && costErr != nil {
		return nil
	}
	usage := &Usage{Input: positiveInt(m["input"]), Output: positiveInt(m["output"]), CacheRead: positiveInt(m["cacheRead"]), CacheWrite: positiveInt(m["cacheWrite"])}
	optionalCounter := func(key string) (int, bool) {
		n, err := requiredFloat(piDecisionLabel, m[key], key)
		if err != nil || n < 0 || n >= math.Ldexp(1, strconv.IntSize-1) || math.Trunc(n) != n {
			return 0, false
		}
		return int(n), true
	}
	usage.Reasoning, usage.ReasoningSet = optionalCounter("reasoning")
	usage.ImageOutput, usage.ImageOutputSet = optionalCounter("imageOutput")
	if total, err := requiredFloat(piDecisionLabel, m["totalTokens"], "totalTokens"); err == nil && total >= 0 {
		usage.TotalTokens = positiveInt(m["totalTokens"])
	} else {
		maxInt := int(^uint(0) >> 1)
		for _, count := range []int{usage.Input, usage.Output, usage.CacheRead, usage.CacheWrite} {
			usage.TotalTokens += min(count, maxInt-usage.TotalTokens)
		}
	}
	if costErr == nil {
		nonNegative := func(key string) float64 {
			f, _ := requiredFloat(piDecisionLabel, cost[key], key)
			return max(0, f)
		}
		usage.Cost = CostBreakdown{Input: nonNegative("input"), Output: nonNegative("output"),
			CacheRead: nonNegative("cacheRead"), CacheWrite: nonNegative("cacheWrite"), Total: max(0, billedTotal)}
		if billed, ok := cost["totalFromProvider"].(bool); ok {
			usage.Cost.TotalFromProvider = &billed
		}
		return usage
	}
	usage.Cost = CalculateCost(&Model{Cost: model.Cost}, usage)
	return usage
}

func parsePiDecisionAnswers(value any, input ClassifierContext) (map[string]ClassifierAnswer, error) {
	m, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s returned an unexpected response", piDecisionLabel)
	}
	answers := make(map[string]ClassifierAnswer, len(input.Questions))
	for id, question := range input.Questions {
		value, present := m[id]
		if !present {
			return nil, fmt.Errorf("%s did not return an answer for %s", piDecisionLabel, id)
		}
		raw, ok := value.(map[string]any)
		if !ok || raw == nil {
			return nil, fmt.Errorf("%s returned an invalid answer for %s", piDecisionLabel, id)
		}
		answer, err := parsePiDecisionAnswer(id, question, raw)
		if err != nil {
			return nil, err
		}
		answers[id] = answer
	}
	return answers, nil
}

func parsePiDecisionAnswer(id string, question ClassifierQuestion, raw map[string]any) (ClassifierAnswer, error) {
	answer := ClassifierAnswer{Type: question.Type}
	if raw["type"] != question.Type {
		return answer, fmt.Errorf("%s did not return a %s answer for %s", piDecisionLabel, question.Type, id)
	}
	var err error
	switch question.Type {
	case "choice":
		criteria, _ := question.Criteria.(map[string]string)
		choice, ok := raw["choice"].(string)
		if !ok {
			return answer, fmt.Errorf("%s did not return a choice answer for %s", piDecisionLabel, id)
		}
		if _, known := criteria[choice]; !known {
			return answer, fmt.Errorf("%s returned unknown choice %q for %s", piDecisionLabel, choice, id)
		}
		probabilities, ok := raw["probabilities"].(map[string]any)
		if !ok || probabilities == nil {
			return answer, fmt.Errorf("%s returned invalid probabilities for %s", piDecisionLabel, id)
		}
		answer.Choice = choice
		answer.Probabilities = make(map[string]float64, len(probabilities))
		for choice, value := range probabilities {
			if _, known := criteria[choice]; !known {
				return answer, fmt.Errorf("%s returned unknown choice %q in probabilities for %s", piDecisionLabel, choice, id)
			}
			answer.Probabilities[choice], err = piDecisionUnitInterval(value, "probability for "+id+"."+choice)
			if err != nil {
				return answer, err
			}
		}
		answer.Confidence, err = piDecisionUnitInterval(raw["confidence"], "confidence for "+id)
	case "score":
		answer.Score, err = requiredFloat(piDecisionLabel, raw["score"], "score for "+id)
		if err != nil {
			return answer, err
		}
		criteria, _ := question.Criteria.([]string)
		if answer.Score < 0 || answer.Score > float64(max(0, len(criteria)-1)) {
			return answer, fmt.Errorf("%s returned out-of-range score for %s: %g", piDecisionLabel, id, answer.Score)
		}
		answer.Confidence, err = piDecisionUnitInterval(raw["confidence"], "confidence for "+id)
	case "bool":
		answer.Probability, err = piDecisionUnitInterval(raw["probability"], "probability for "+id)
	default:
		return answer, fmt.Errorf("unknown classifier question type %q", question.Type)
	}
	return answer, err
}

func piDecisionUnitInterval(value any, field string) (float64, error) {
	n, err := requiredFloat(piDecisionLabel, value, field)
	if err == nil && (n < 0 || n > 1) {
		err = fmt.Errorf("%s returned out-of-range %s: %g", piDecisionLabel, field, n)
	}
	return n, err
}
