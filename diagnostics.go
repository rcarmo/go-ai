package goai

import (
	"fmt"
	"runtime"
	"time"
)

type codedError interface{ Code() any }

// FormatThrownValue returns a compact, user-facing error string.
func FormatThrownValue(value any) string {
	if value == nil {
		return "<nil>"
	}
	if err, ok := value.(error); ok {
		if err.Error() != "" {
			return err.Error()
		}
		return fmt.Sprintf("%T", err)
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

// ExtractDiagnosticError converts an error into a serializable diagnostic shape.
func ExtractDiagnosticError(err error) DiagnosticError {
	if err == nil {
		return DiagnosticError{Message: "<nil>"}
	}
	diag := DiagnosticError{Name: fmt.Sprintf("%T", err), Message: err.Error()}
	if c, ok := err.(codedError); ok {
		diag.Code = c.Code()
	}
	buf := make([]byte, 16*1024)
	if n := runtime.Stack(buf, false); n > 0 {
		diag.Stack = string(buf[:n])
	}
	return diag
}

// CreateAssistantMessageDiagnostic creates a timestamped assistant diagnostic.
func CreateAssistantMessageDiagnostic(kind string, err error, details map[string]any) AssistantMessageDiagnostic {
	return AssistantMessageDiagnostic{Type: kind, Timestamp: time.Now().UnixMilli(), Error: ExtractDiagnosticError(err), Details: details}
}

// AppendAssistantMessageDiagnostic appends a diagnostic to an assistant message.
func AppendAssistantMessageDiagnostic(message *Message, diagnostic AssistantMessageDiagnostic) {
	if message == nil {
		return
	}
	message.Diagnostics = append(message.Diagnostics, diagnostic)
}
