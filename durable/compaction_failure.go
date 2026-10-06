package durable

import (
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
)

func failCompactionNoModel(ctx context.Context, r *TaskRuntime, ref ModelRef) error {
	message := fmt.Sprintf("Model %s/%s is not available", ref.Provider, ref.ID)
	if ref.ID == "" || ref.Provider == "" {
		message = "No model is configured"
	}
	return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
		return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: message, Detail: &TaskValue{Present: true, Value: JSON{"reason": "no_model"}}}}}, nil
	})
}

func summaryFailure(message MessageReceipt) string {
	stop := message.StopReason
	if message.providerStopReason != "" {
		stop = message.providerStopReason
	}
	switch stop {
	case goai.StopReasonError, goai.StopReasonAborted:
		text := message.providerError
		if text == "" {
			text = string(stop)
		}
		return "Summarization failed: " + text
	case goai.StopReasonLength:
		return "Summarization hit the token limit; the summary is incomplete"
	}
	for _, block := range message.Content {
		if block.Type == "toolCall" {
			return "Summarization attempted to call a tool"
		}
	}
	return "Summarization produced no text"
}
