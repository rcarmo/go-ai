package durable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	goai "github.com/rcarmo/go-ai"
)

// TaskRecord is the public runtime record. Its JSON representation follows
// pi-durable v1.5.0; internal Task/NativeTaskExecution remain a distinct,
// versioned storage envelope. Never reinterpret persisted envelope JSON as this
// public form without an explicit migration.
func (record TaskRecord) MarshalJSON() ([]byte, error) {
	if record.Input == nil || !record.Input.Present {
		return nil, reject("task record input is absent")
	}
	state, err := publicTaskStateJSON(record.State)
	if err != nil {
		return nil, err
	}
	memos := map[string]any(nil)
	if record.Memos != nil {
		memos = make(map[string]any, len(record.Memos))
		for key, value := range record.Memos {
			if value == nil || !value.Present {
				return nil, reject("task record memo is absent")
			}
			memos[key] = value.Value
		}
	}
	out := map[string]any{"id": record.ID, "conversationId": record.Conversation, "kind": record.Kind, "version": record.Version, "input": record.Input.Value, "background": record.Background, "abortRequested": record.AbortRequested, "state": state}
	if record.Owner != 0 {
		out["owner"] = record.Owner
	}
	if record.Memos != nil {
		out["memos"] = memos
	}
	if record.Trace != nil {
		out["trace"] = record.Trace
	}
	if record.Links != nil {
		out["links"] = *record.Links
	}
	if record.AbortReason != "" {
		out["abortReason"] = record.AbortReason
	}
	if record.AbandonOnRestart {
		out["abandonOnRestart"] = true
	}
	if record.StartedAt != nil {
		out["startedAt"] = *record.StartedAt
	}
	if record.EndedAt != nil {
		out["endedAt"] = *record.EndedAt
	}
	if record.Restarts != 0 || record.wireRestartsPresent {
		out["restarts"] = record.Restarts
	}
	// This encoder validates strict JSON, preserves exact numbers, and never
	// invokes arbitrary caller-owned marshalers on task input or checkpoints.
	return encodeBounded(out, maximumDurableLimits(), maximumDurableLimits().MaxRecordBytes)
}

func publicTaskStateJSON(state TaskState) (map[string]any, error) {
	out := map[string]any{"status": state.Status}
	switch state.Status {
	case "pending", "running":
		out["checkpoint"] = state.Checkpoint
	case "waiting":
		out["checkpoint"] = state.Checkpoint
		on := state.On
		if on == nil {
			on = []ID{}
		}
		out["on"] = on
		out["policy"] = state.Policy
	case "sleeping":
		out["checkpoint"] = state.Checkpoint
		if state.Until == nil {
			return nil, reject("sleeping task deadline absent")
		}
		out["until"] = *state.Until
	case "completing", "terminal":
		if state.Outcome == nil {
			return nil, reject("task outcome absent")
		}
		result := map[string]any{"status": state.Outcome.Status}
		if state.Outcome.Result != nil {
			if !state.Outcome.Result.Present {
				return nil, reject("task outcome result absent")
			}
			result["result"] = state.Outcome.Result.Value
		}
		if state.Outcome.Error != nil {
			failure := map[string]any{"message": state.Outcome.Error.Message}
			if state.Outcome.Error.Detail != nil {
				if !state.Outcome.Error.Detail.Present {
					return nil, reject("task error detail absent")
				}
				failure["detail"] = state.Outcome.Error.Detail.Value
			}
			result["error"] = failure
		}
		if state.Outcome.Reason != "" || state.Outcome.wireReasonPresent {
			result["reason"] = state.Outcome.Reason
		}
		out["outcome"] = result
	default:
		return nil, fmt.Errorf("invalid task status %q", state.Status)
	}
	return out, nil
}

func decodePublicJSON(data []byte, target any) error {
	if len(data) > maximumDurableLimits().MaxRecordBytes {
		return reject("task record byte limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return reject("trailing task record data")
	}
	return nil
}

func (record *TaskRecord) UnmarshalJSON(data []byte) error {
	var wire struct {
		ID               ID                         `json:"id"`
		Conversation     ID                         `json:"conversationId"`
		Owner            ID                         `json:"owner"`
		Kind             string                     `json:"kind"`
		Version          uint64                     `json:"version"`
		Input            json.RawMessage            `json:"input"`
		Background       bool                       `json:"background"`
		AbortRequested   bool                       `json:"abortRequested"`
		State            json.RawMessage            `json:"state"`
		Memos            map[string]json.RawMessage `json:"memos"`
		Trace            json.RawMessage            `json:"trace"`
		Links            *[]goai.TelemetryCarrier   `json:"links"`
		AbortReason      string                     `json:"abortReason"`
		AbandonOnRestart bool                       `json:"abandonOnRestart"`
		StartedAt        *int64                     `json:"startedAt"`
		EndedAt          *int64                     `json:"endedAt"`
		Restarts         *uint64                    `json:"restarts"`
	}
	if err := decodePublicJSON(data, &wire); err != nil {
		return err
	}
	if len(wire.Input) == 0 || len(wire.State) == 0 {
		return reject("task record input or state absent")
	}
	var input any
	if err := decodePublicJSON(wire.Input, &input); err != nil {
		return err
	}
	input, err := ownJSONValue(input, maximumDurableLimits())
	if err != nil {
		return err
	}
	state, err := parsePublicTaskState(wire.State)
	if err != nil {
		return err
	}
	var memos map[string]*TaskValue
	if wire.Memos != nil {
		memos = make(map[string]*TaskValue, len(wire.Memos))
		for key, raw := range wire.Memos {
			var value any
			if err := decodePublicJSON(raw, &value); err != nil {
				return err
			}
			value, err = ownJSONValue(value, maximumDurableLimits())
			if err != nil {
				return err
			}
			memos[key] = &TaskValue{Present: true, Value: value}
		}
	}
	var trace any
	if wire.Trace != nil {
		if err := decodePublicJSON(wire.Trace, &trace); err != nil {
			return err
		}
		trace, err = copyTaskTrace(trace, maximumDurableLimits())
		if err != nil {
			return err
		}
	}
	result := TaskRecord{ID: wire.ID, Conversation: wire.Conversation, Owner: wire.Owner, Kind: wire.Kind, Version: wire.Version, Input: &TaskValue{Present: true, Value: input}, Background: wire.Background, AbortRequested: wire.AbortRequested, State: state, Memos: memos, Trace: trace, Links: wire.Links, AbortReason: wire.AbortReason, AbandonOnRestart: wire.AbandonOnRestart, StartedAt: wire.StartedAt, EndedAt: wire.EndedAt}
	if wire.Restarts != nil {
		result.Restarts = *wire.Restarts
		result.wireRestartsPresent = true
	}
	*record = result
	return nil
}

func parsePublicTaskState(data []byte) (TaskState, error) {
	var wire struct {
		Status     string          `json:"status"`
		Checkpoint json.RawMessage `json:"checkpoint"`
		On         []ID            `json:"on"`
		Policy     string          `json:"policy"`
		Until      *int64          `json:"until"`
		Outcome    json.RawMessage `json:"outcome"`
	}
	if err := decodePublicJSON(data, &wire); err != nil {
		return TaskState{}, err
	}
	result := TaskState{Status: wire.Status, On: wire.On, Policy: wire.Policy, Until: wire.Until}
	if wire.Checkpoint != nil {
		if err := decodePublicJSON(wire.Checkpoint, &result.Checkpoint); err != nil {
			return TaskState{}, err
		}
		owned, err := ownJSONValue(result.Checkpoint, maximumDurableLimits())
		if err != nil {
			return TaskState{}, err
		}
		result.Checkpoint = owned
	}
	if wire.Outcome != nil {
		var outcome struct {
			Status string          `json:"status"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string          `json:"message"`
				Detail  json.RawMessage `json:"detail"`
			} `json:"error"`
			Reason *string `json:"reason"`
		}
		if err := decodePublicJSON(wire.Outcome, &outcome); err != nil {
			return TaskState{}, err
		}
		result.Outcome = &TaskOutcome{Status: outcome.Status}
		if outcome.Result != nil {
			var value any
			if err := decodePublicJSON(outcome.Result, &value); err != nil {
				return TaskState{}, err
			}
			value, err := ownJSONValue(value, maximumDurableLimits())
			if err != nil {
				return TaskState{}, err
			}
			result.Outcome.Result = &TaskValue{Present: true, Value: value}
		}
		if outcome.Error != nil {
			result.Outcome.Error = &TaskOutcomeError{Message: outcome.Error.Message}
			if outcome.Error.Detail != nil {
				var value any
				if err := decodePublicJSON(outcome.Error.Detail, &value); err != nil {
					return TaskState{}, err
				}
				value, err := ownJSONValue(value, maximumDurableLimits())
				if err != nil {
					return TaskState{}, err
				}
				result.Outcome.Error.Detail = &TaskValue{Present: true, Value: value}
			}
		}
		if outcome.Reason != nil {
			result.Outcome.Reason = *outcome.Reason
			result.Outcome.wireReasonPresent = true
		}
	}
	return result, nil
}

// Snapshot detachment must not round-trip the legacy/private representation
// through the public wire decoder. Old legacy task views may lack input, while
// canonical v1.5 public TaskRecords require it; copy ownership independently.
func copyPublicTaskRecord(record TaskRecord, limits Limits) (TaskRecord, error) {
	result := record
	var err error
	result.StartedAt = copyTaskTime(record.StartedAt)
	result.EndedAt = copyTaskTime(record.EndedAt)
	if record.Input != nil {
		result.Input, err = copyTaskValue(record.Input, limits)
		if err != nil {
			return TaskRecord{}, err
		}
	}
	result.Memos, err = copyTaskMemos(record.Memos, limits)
	if err != nil {
		return TaskRecord{}, err
	}
	result.State, err = copyTaskState(record.State, limits)
	if err != nil {
		return TaskRecord{}, err
	}
	result.Trace, err = copyTaskTrace(record.Trace, limits)
	if err != nil {
		return TaskRecord{}, err
	}
	result.Links, err = copyTaskLinks(record.Links, limits)
	if err != nil {
		return TaskRecord{}, err
	}
	return result, nil
}

// Standalone TaskState uses the original public union. Stored native execution
// envelopes are decoded separately below to preserve their published legacy
// representation, which predates this public codec.
func (state TaskState) MarshalJSON() ([]byte, error) {
	value, err := publicTaskStateJSON(state)
	if err != nil {
		return nil, err
	}
	return encodeBounded(value, maximumDurableLimits(), maximumDurableLimits().MaxRecordBytes)
}
func (state *TaskState) UnmarshalJSON(data []byte) error {
	parsed, err := parsePublicTaskState(data)
	if err != nil {
		return err
	}
	*state = parsed
	return nil
}

// The legacy SQLite frame codec stores TaskValue's presence wrapper inside the
// native execution envelope. Decode this exact private shape without invoking
// TaskState's public JSON decoder; conflating the two would silently reinterpret
// a user value shaped like {"present":true,"value":...}.
func (execution *NativeTaskExecution) UnmarshalJSON(data []byte) error {
	type storedTaskState TaskState
	var wire struct {
		Version        uint64                `json:"version"`
		Input          *TaskValue            `json:"input"`
		Background     bool                  `json:"background"`
		AbortRequested bool                  `json:"abortRequested"`
		Memos          map[string]*TaskValue `json:"memos"`
		State          storedTaskState       `json:"state"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return reject("trailing stored execution data")
	}
	*execution = NativeTaskExecution{Version: wire.Version, Input: wire.Input, Background: wire.Background, AbortRequested: wire.AbortRequested, Memos: wire.Memos, State: TaskState(wire.State)}
	return nil
}

// Native SQLite snapshots and framed writes retain the historical private
// execution envelope. In particular, TaskState's public marshaler must never
// change an existing file or native crash-witness serialization.
func (execution NativeTaskExecution) MarshalJSON() ([]byte, error) {
	return encodeBounded(execution, maximumDurableLimits(), maximumDurableLimits().MaxRecordBytes)
}
