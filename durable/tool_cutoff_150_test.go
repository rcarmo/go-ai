package durable

import "testing"

// Native codec control: the failed ending is independent of afterTool's
// success/error flag and diagnostics, but its private witness must agree.
func TestToolCutoff150NormalizedEndingWitness(t *testing.T) {
	message := "Tool work was interrupted once in a row; it is not run again"
	outcome := TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: message, Detail: &TaskValue{Present: true, Value: JSON{"reason": "restart_limit"}}}}
	flag := false
	cp := toolCheckpoint{ErrorCode: "restart_limit", CutoffMessage: message, FinalIsError: &flag, Result: &MessageReceipt{ErrorCode: "restart_limit", IsError: false, Diagnostics: []ToolDiagnostic{{Severity: "info", Code: "replaced", Message: "done"}}}}
	if status, err := toolReceiptRawStatus(outcome, cp); err != nil || status != "done" {
		t.Fatal(status, err)
	}
	for _, fault := range []string{"message", "reason", "status", "receipt-flag"} {
		t.Run(fault, func(t *testing.T) {
			bad := outcome
			detail := *outcome.Error
			bad.Error = &detail
			next := cp
			receipt := *cp.Result
			next.Result = &receipt
			switch fault {
			case "message":
				bad.Error.Message = "different"
			case "reason":
				bad.Error.Detail = &TaskValue{Present: true, Value: JSON{"reason": "different"}}
			case "status":
				bad.Status = "completed"
			case "receipt-flag":
				next.Result.IsError = true
			}
			if _, err := toolReceiptRawStatus(bad, next); err == nil {
				t.Fatal("corrupt normalized cutoff accepted", fault)
			}
		})
	}
}
