package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestStopReasonDeferredSerializesWithHandle(t *testing.T) {
	msg := goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonDeferred, Deferred: &goai.DeferredHandle{Provider: "faux", ModelID: "m", Api: "faux", ID: "h", PollAfterMs: 25}}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"role":"assistant","content":null,"timestamp":0,"stopReason":"deferred","deferred":{"provider":"faux","modelId":"m","api":"faux","id":"h","pollAfterMs":25}}` {
		t.Fatalf("json=%s", raw)
	}
}

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
