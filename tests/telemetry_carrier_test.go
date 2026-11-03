package goai_test

import (
	"encoding/json"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestTelemetryCarrierDataOnlyAndStrictDecoding(t *testing.T) {
	for _, data := range []string{`{}`, `{"scope":"opaque","span":"雪/\u0000"}`} {
		var got goai.TelemetryCarrier
		if err := json.Unmarshal([]byte(data), &got); err != nil {
			t.Fatal(err)
		}
		if got == nil {
			t.Fatal("empty carrier became absent")
		}
		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		var want map[string]string
		if err := json.Unmarshal([]byte(data), &want); err != nil {
			t.Fatal(err)
		}
		var restored map[string]string
		if err := json.Unmarshal(encoded, &restored); err != nil || !reflect.DeepEqual(restored, want) {
			t.Fatal("carrier changed", restored, want, err)
		}
	}
	for _, data := range []string{`null`, `[]`, `"s"`, `1`, `{"span":null}`, `{"span":1}`, `{"span":false}`, `{"span":[]}`, `{"span":{}}`, `{"span":"unterminated}`} {
		got := goai.TelemetryCarrier{"existing": "unchanged"}
		if err := json.Unmarshal([]byte(data), &got); err == nil {
			t.Error("invalid carrier accepted", data)
		}
		if !reflect.DeepEqual(got, goai.TelemetryCarrier{"existing": "unchanged"}) {
			t.Error("failed decode mutated receiver", data, got)
		}
	}
	span := &goai.TelemetrySpan{Context: goai.TelemetryCarrier{"traceparent": "PRIVATE_PROPAGATION"}, Value: make(chan int)}
	for _, value := range []any{span, &goai.StreamOptions{Telemetry: span}} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"Context", "context", "Value", "value", "Telemetry", "telemetry"} {
			if _, present := object[key]; present {
				t.Error("backend data entered ordinary request JSON", key)
			}
		}
	}
}
