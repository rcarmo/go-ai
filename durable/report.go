package durable

import "context"

// ReportTask addresses a durable record; no inputs or checkpoint content belong
// in reporting metadata.
type ReportTask struct {
	ID             ID     `json:"id"`
	Kind           string `json:"kind"`
	ConversationID ID     `json:"conversationId"`
}

// ReportInfo follows v1.4.0's fixed report vocabulary. Optional fields are set
// only when their provenance is known, never inferred from an error message.
type ReportInfo struct {
	Type           string       `json:"type"`
	Task           *ReportTask  `json:"task,omitempty"`
	Extension      string       `json:"extension,omitempty"`
	ConversationID ID           `json:"conversationId,omitempty"`
	Callback       HostCallback `json:"callback,omitempty"`
	Reason         string       `json:"reason,omitempty"`
}
type HostCallback string

// ReportFrom attributes a Runtime.Report to extension or host code instead of
// the task's own definition. Type is "extension" or "host".
type ReportFrom struct {
	Type      string
	Extension string
	Callback  HostCallback
}

func validHostCallback(callback HostCallback) bool {
	switch callback {
	case "now", "settings", "registry", "env", "models", "setTimer", "onWake":
		return true
	}
	return false
}
func copyReportInfo(info ReportInfo) ReportInfo {
	if info.Task != nil {
		task := *info.Task
		info.Task = &task
	}
	return info
}
func deliverReport(ctx context.Context, err error, info ReportInfo, legacy func(error), rich func(context.Context, error, ReportInfo)) {
	if err == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if rich != nil {
		func() { defer func() { _ = recover() }(); rich(ctx, err, copyReportInfo(info)) }()
	}
	reportTaskError(legacy, err)
}
func (h *Harness) report(ctx context.Context, err error, info ReportInfo) {
	deliverReport(ctx, err, info, h.options.OnReport, h.options.OnReportWithInfo)
}
func (s TaskRegistrySnapshot) installer(kind string, definition *TaskDefinition) string {
	if definition == nil || s.selection == nil || s.Task(kind) != definition {
		return ""
	}
	for i := len(s.selection.extensions) - 1; i >= 0; i-- {
		extension := s.selection.extensions[i]
		if extension.tasks[kind] == definition {
			return extension.name
		}
	}
	return ""
}
func (r *TaskRuntime) reportInfo(kind, reason string) ReportInfo {
	return ReportInfo{Type: kind, Task: &ReportTask{ID: r.taskID, Kind: r.kind, ConversationID: r.conversation}, Extension: r.reportExtension, Reason: reason}
}
func (r *TaskRuntime) report(err error, info ReportInfo) { r.harness.report(r.context, err, info) }
func taskReportInfo(task Task, registry TaskRegistrySnapshot, kind, reason string) ReportInfo {
	return ReportInfo{Type: kind, Task: &ReportTask{ID: task.ID, Kind: task.Kind, ConversationID: task.Conversation}, Extension: registry.installer(task.Kind, registry.Task(task.Kind)), Reason: reason}
}

// Reservation diagnostics capture metadata on-line but are delivered off-line.
// The original error identity is restored before invoking either host callback.
type attributedReport struct {
	cause error
	info  ReportInfo
	ctx   context.Context
}

func (e *attributedReport) Error() string { return e.cause.Error() }
func (e *attributedReport) Unwrap() error { return e.cause }
