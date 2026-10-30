package durable

import (
	"reflect"
	"unicode/utf8"

	goai "github.com/rcarmo/go-ai"
)

func diagnosticTextField(v reflect.Value, name string) bool {
	if v.Type() == nativeContentBlockType && name == "text" {
		return v.Interface().(goai.ContentBlock).HasToolArgumentDiagnostic()
	}
	return v.Type() == reflect.TypeFor[ToolDiagnostic]() && name == "message" && v.Interface().(ToolDiagnostic).argumentError
}
func argumentDiagnosticReceipt(r MessageReceipt) bool {
	return r.Role == goai.RoleToolResult && r.ErrorCode == "invalid_arguments" && r.IsError
}
func appendDiagnosticWire(view *receiptWireView, r MessageReceipt, l Limits, remaining int) error {
	appendText := func(kind string, index int, text string) error {
		if !argumentDiagnosticReceipt(r) {
			return reject("argument diagnostic outside error receipt")
		}
		chunks, err := encodeArgumentCarrier(JSON{}, text, l, remaining)
		if err != nil {
			return err
		}
		for _, chunk := range chunks {
			remaining -= len(chunk)
		}
		view.arguments = append(view.arguments, ReceiptUTF16Arguments{Kind: kind, Index: index, Chunks: chunks})
		return nil
	}
	for i, block := range r.Content {
		if !block.HasToolArgumentDiagnostic() {
			continue
		}
		if block.Type != "text" || !argumentDiagnosticReceipt(r) {
			return reject("invalid argument diagnostic block")
		}
		if utf8.ValidString(block.Text) {
			continue
		}
		if err := appendText("text", i, block.Text); err != nil {
			return err
		}
		if reflect.ValueOf(view.content).Pointer() == reflect.ValueOf(r.Content).Pointer() {
			view.content = append([]goai.ContentBlock(nil), r.Content...)
		}
		view.content[i].SetToolArgumentDiagnostic("")
	}
	for i, d := range r.Diagnostics {
		if !d.argumentError {
			continue
		}
		if !argumentDiagnosticReceipt(r) || d.Code != "invalid_arguments" || d.Severity != "error" {
			return reject("invalid argument diagnostic")
		}
		if utf8.ValidString(d.Message) {
			continue
		}
		if err := appendText("diagnostic", i, d.Message); err != nil {
			return err
		}
		if reflect.ValueOf(view.diagnostics).Pointer() == reflect.ValueOf(r.Diagnostics).Pointer() {
			view.diagnostics = append([]ToolDiagnostic(nil), r.Diagnostics...)
		}
		view.diagnostics[i].Message = ""
	}
	return nil
}
func validateDiagnosticWitness(r *MessageReceipt, w ReceiptUTF16Arguments, l Limits) (string, error) {
	if !argumentDiagnosticReceipt(*r) || w.Index < 0 {
		return "", reject("argument diagnostic witness context")
	}
	switch w.Kind {
	case "text":
		if w.Index >= len(r.Content) || r.Content[w.Index].Type != "text" || r.Content[w.Index].Text != "" {
			return "", reject("argument diagnostic text placeholder")
		}
	case "diagnostic":
		if w.Index >= len(r.Diagnostics) {
			return "", reject("argument diagnostic index")
		}
		d := r.Diagnostics[w.Index]
		if d.Code != "invalid_arguments" || d.Severity != "error" || d.Message != "" {
			return "", reject("argument diagnostic placeholder")
		}
	default:
		return "", reject("unknown argument witness kind")
	}
	carrier, err := decodeArgumentCarrier(w.Chunks, l)
	if err != nil {
		return "", err
	}
	if len(carrier.Arguments) != 0 || utf8.ValidString(carrier.ArgumentsError) {
		return "", reject("invalid diagnostic-only carrier")
	}
	return carrier.ArgumentsError, nil
}

// Final checkpoint remarks mirror the typed receipt. Its witness is the sole
// authority for restoring empty checkpoint placeholders, avoiding two carriers.
func restoreCheckpointDiagnosticRemarks(cp *toolCheckpoint) error {
	if cp.Result == nil || !argumentDiagnosticReceipt(*cp.Result) {
		return nil
	}
	native := false
	for _, d := range cp.Result.Diagnostics {
		if d.argumentError {
			native = true
		}
	}
	if !native {
		return nil
	}
	if !cp.StreamingArguments || len(cp.Diagnostics) != len(cp.Result.Diagnostics) {
		return reject("argument diagnostic checkpoint context")
	}
	for i, d := range cp.Result.Diagnostics {
		stored := cp.Diagnostics[i]
		if stored.Code != d.Code || stored.Severity != d.Severity {
			return reject("argument diagnostic checkpoint identity")
		}
		if d.argumentError {
			if stored.Message != "" {
				return reject("argument diagnostic checkpoint placeholder")
			}
			cp.Diagnostics[i] = d
		} else if stored.Message != d.Message {
			return reject("argument diagnostic checkpoint mismatch")
		}
	}
	return nil
}
