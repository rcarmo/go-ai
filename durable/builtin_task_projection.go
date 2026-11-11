package durable

import goai "github.com/rcarmo/go-ai"

// Source builtin task contract (pi-durable v1.5.0 GenerationTask version2 and
// ToolTask version3). The native runtime keeps its private execution
// checkpoint; every public TaskRecord is derived from persisted facts in it.
// Complete edge-phase, old-data and migration acceptance is still open; this
// projection does not replace the private durable storage/execution boundary.
const (
	generationTaskVersion = 2
	toolTaskVersion       = 3
)

func generationPublicState(owned Task, cp generationCheckpoint) TaskState {
	status := owned.Status
	attempt := func(next bool) uint64 {
		if next {
			return cp.Attempt + 1
		}
		return cp.Attempt
	}
	compacted := func(checkpoint JSON) JSON {
		if cp.Compaction != 0 {
			checkpoint["compacted"] = cp.Compaction
		}
		return checkpoint
	}
	switch cp.Phase {
	case "queued", "prepare-next":
		// Native attempt counts charged requests; source prepare names the next one.
		return TaskState{Status: status, Checkpoint: compacted(JSON{"phase": "prepare", "attempt": attempt(true)})}
	case "compaction":
		checkpoint := compacted(JSON{"phase": "prepare", "attempt": attempt(true)})
		if cp.OverflowMessage != "" {
			checkpoint["overflow"] = cp.OverflowMessage
		} else {
			checkpoint["threshold"] = true
		}
		if status == "running" {
			return TaskState{Status: status, Checkpoint: checkpoint}
		}
		return TaskState{Status: "waiting", Checkpoint: checkpoint, On: []ID{cp.Compaction}, Policy: "allSettled"}
	case "intent":
		// The intent commit charges its attempt and records the source cutoff.
		return TaskState{Status: status, Checkpoint: compacted(JSON{"phase": "request", "attempt": attempt(false), "model": generationModelRef(cp), "thinkingLevel": generationThinkingLevel(cp), "streamOptions": generationStreamOptions(cp.Agent.Settings), "cutoff": cp.Cutoff})}
	case "retry":
		state := TaskState{Status: status, Checkpoint: compacted(JSON{"phase": "retry", "attempt": attempt(false), "until": cp.RetryUntil})}
		if status == "sleeping" {
			until := cp.RetryUntil
			state.Until = &until
		}
		return state
	case "poll":
		checkpoint := compacted(JSON{"phase": "poll", "attempt": attempt(false), "model": generationModelRef(cp), "thinkingLevel": generationThinkingLevel(cp), "cutoff": cp.Cutoff, "pollAt": cp.PollAt})
		if cp.Deferred != nil {
			checkpoint["handle"] = cp.Deferred
		}
		state := TaskState{Status: status, Checkpoint: checkpoint}
		if status == "sleeping" {
			until := cp.PollAt
			state.Until = &until
		}
		return state
	case "tools":
		pending := make([]string, 0, len(cp.PendingTools))
		for _, tool := range cp.PendingTools {
			pending = append(pending, tool.CallID)
		}
		tools := append([]ID{}, cp.Children...)
		checkpoint := JSON{"phase": "tools", "assistant": cp.AssistantEntry, "tools": tools, "pending": pending}
		if status != "completing" {
			return TaskState{Status: status, Checkpoint: checkpoint}
		}
		// A parallel round waits on all its tasks; a sequential round on the one it started.
		on := tools
		if cp.Sequential && len(on) > 0 {
			on = []ID{on[len(on)-1]}
		}
		return TaskState{Status: "waiting", Checkpoint: checkpoint, On: append([]ID{}, on...), Policy: "allSettled"}
	case "finish":
		if cp.AfterRun == nil {
			return TaskState{}
		}
		return TaskState{Status: status, Checkpoint: JSON{"phase": "finish", "run": JSON{"reason": cp.AfterRun.Reason, "first": cp.AfterRun.First, "answer": cp.AfterRun.Answer, "last": cp.AfterRun.Last}}}
	case "drain":
		if cp.AfterRun == nil {
			return TaskState{}
		}
		tasks := append([]ID{}, cp.AfterRunTasks...)
		checkpoint := JSON{"phase": "drain", "answer": cp.AfterRun.Answer, "tasks": tasks}
		if status == "running" {
			return TaskState{Status: status, Checkpoint: checkpoint}
		}
		return TaskState{Status: "waiting", Checkpoint: checkpoint, On: append([]ID{}, tasks...), Policy: "allSettled"}
	}
	return TaskState{}
}

func generationModelRef(cp generationCheckpoint) JSON {
	return JSON{"provider": string(cp.Agent.Model.Provider), "modelId": cp.Agent.Model.ID}
}

func generationThinkingLevel(cp generationCheckpoint) string {
	if cp.Agent.ThinkingLevel == "" {
		return string(goai.ThinkingOff)
	}
	return string(cp.Agent.ThinkingLevel)
}

// ConversationStreamOptions holds only the present source fields.
func generationStreamOptions(settings RequestSettings) JSON {
	out := JSON{}
	if settings.Transport != "" {
		out["transport"] = string(settings.Transport)
	}
	if settings.TimeoutMs != nil {
		out["timeoutMs"] = *settings.TimeoutMs
	}
	if settings.MaxRetries != nil {
		out["maxRetries"] = *settings.MaxRetries
	}
	if settings.MaxRetryDelayMs != nil {
		out["maxRetryDelayMs"] = *settings.MaxRetryDelayMs
	}
	if settings.Headers != nil {
		headers := JSON{}
		for key, value := range settings.Headers {
			headers[key] = value
		}
		out["headers"] = headers
	}
	if settings.Metadata != nil {
		out["metadata"] = settings.Metadata
	}
	if settings.CacheRetention != "" {
		out["cacheRetention"] = string(settings.CacheRetention)
	}
	if settings.Deferred != nil {
		if settings.Deferred.Window == "" {
			out["deferred"] = true
		} else {
			out["deferred"] = JSON{"window": settings.Deferred.Window}
		}
	}
	return out
}

func toolPublicInput(cp toolCheckpoint) *TaskValue {
	if cp.Assistant == 0 {
		return nil // records before the source input was persisted carry no assistant witness
	}
	return &TaskValue{Present: true, Value: JSON{"kind": "model", "assistant": cp.Assistant, "callId": cp.CallID}}
}

func toolPublicState(owned Task, cp toolCheckpoint) TaskState {
	if !cp.Started {
		return TaskState{Status: owned.Status, Checkpoint: JSON{"phase": "call"}}
	}
	replay := "unsafe"
	if cp.Offer.ReplaySafe {
		replay = "safe"
	}
	arguments := cp.Arguments
	if arguments == nil {
		arguments = JSON{}
	}
	var publicArguments any = arguments
	if cp.StreamingArguments {
		publicArguments = sourceToolArguments(arguments)
	}
	return TaskState{Status: owned.Status, Checkpoint: JSON{"phase": "execute", "arguments": publicArguments, "replay": replay}}
}
