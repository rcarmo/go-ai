package durable

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

var workerScriptErrorName = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]{0,63}$`)
var workerScriptFrame = regexp.MustCompile(`^    at (?:codemode\.js:[0-9]+(?::[0-9]+)?|[A-Za-z0-9_$.<> \[\]-]+ \(codemode\.js:[0-9]+(?::[0-9]+)?\))$`)

func decodeWorkerScriptError(raw json.RawMessage, l Limits) (*quickjs.Error, error) {
	if len(raw) > l.MaxRecordBytes {
		return nil, reject("worker script error exceeds limit")
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, err
	}
	var detail struct{ Name, Message, Stack string }
	if err := json.Unmarshal([]byte(encoded), &detail); err != nil {
		return nil, err
	}
	if detail.Message == "" {
		detail.Message = "Script failed"
	}
	if !workerScriptErrorName.MatchString(detail.Name) {
		detail.Name = ""
	}
	script := &quickjs.Error{Kind: quickjs.ErrorScript, Name: detail.Name, Message: detail.Message}
	if detail.Name == "" || len(detail.Stack) > 8<<10 {
		return script, nil
	}
	header := detail.Name + ": " + detail.Message
	frames, ok := strings.CutPrefix(detail.Stack, header+"\n")
	if !ok {
		return script, nil
	}
	// Keep the guest's script frames. A transport-supplied host or prelude
	// path is not a script diagnostic and must not reach the model.
	kept := []string{header}
	for _, frame := range strings.Split(frames, "\n") {
		if workerScriptFrame.MatchString(frame) {
			kept = append(kept, frame)
		}
	}
	if len(kept) > 1 {
		script.Stack = strings.Join(kept, "\n")
	}
	return script, nil
}
