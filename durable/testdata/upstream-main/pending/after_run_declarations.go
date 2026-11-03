package durable

// Declaration-only baseline adapter for official v1.5.0 afterRun oracles.
// This file is not compiled by default. The documented overlay adds these
// names and a GenerationHooks.AfterRun field; it adds NO invocation/lifecycle
// implementation. A failed baseline therefore cannot credit hook dispatch.
// Replace this adapter with the reviewed public API when porting the runtime.
type RunSummary struct {
	Reason string
	First  ID
	Answer ID
	Last   ID
}

type RunEndTask struct {
	Task  *TaskDefinition
	Input any
}

type AfterRunResult struct {
	Tasks []RunEndTask
}
