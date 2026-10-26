package durable

import (
	"context"
	"errors"
)

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
	if info.Type != "session_failed" && err != nil && h.session != nil {
		if failure := h.session.failure(); failure != nil {
			var failed *SessionFailed
			if errors.As(err, &failed) && errors.Is(err, failure.(*SessionFailed).Cause) {
				return
			}
		}
	}
	if err != nil {
		h.reportTelemetry(ctx, info)
	}
	deliverReport(ctx, err, info, h.options.OnReport, h.options.OnReportWithInfo)
}

// Listener workers are caller-owned and already detached from mutation lines.
// Standalone Sessions retain their existing WatchEnd diagnostic contract.
func (s *Session) reportListener(ctx context.Context, err error) {
	if scheduler := s.taskScheduler; scheduler != nil {
		scheduler.h.report(ctx, err, ReportInfo{Type: "listener_error"})
	}
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
func (r *TaskRuntime) reportExtensionError(err error, extension string) {
	r.reportError(extensionReportError(err, extension))
}
func (r *TaskRuntime) hostCallbackFailure(err error, callback HostCallback) error {
	info := r.reportInfo("host_error", "")
	info.Extension = ""
	info.Callback = callback
	return &attributedReport{cause: err, info: info, ctx: r.context}
}
func (r *TaskRuntime) reportHostError(err error, callback HostCallback) {
	info := r.reportInfo("host_error", "")
	info.Extension = ""
	info.Callback = callback
	r.report(err, info)
}
func (r *TaskRuntime) reportError(err error) {
	info := r.reportInfo("task_error", "")
	if attributed, ok := err.(*attributedReport); ok {
		info = copyReportInfo(attributed.info)
		if info.Task == nil {
			info.Task = &ReportTask{ID: r.taskID, Kind: r.kind, ConversationID: r.conversation}
		}
		err = attributed.cause
	}
	r.report(err, info)
}
func extensionReportError(err error, extension string) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*attributedReport); ok {
		return err
	}
	return &attributedReport{cause: err, info: ReportInfo{Type: "extension_error", Extension: extension}}
}
func attributedPromptSection(section PromptSection, extension string) PromptSection {
	original := section
	section.Render = func(ctx context.Context, input PromptInput) (*string, error) {
		text, err := renderPromptCallback(ctx, original, input)
		return text, extensionReportError(err, extension)
	}
	return section
}
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
