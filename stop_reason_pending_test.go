package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestStopReasonPendingSerializesAsPublicValue(t *testing.T) {
	msg := goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonPending}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"role":"assistant","content":null,"timestamp":0,"stopReason":"pending"}` {
		t.Fatalf("json=%s", raw)
	}
}
