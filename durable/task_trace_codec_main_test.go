package durable

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type mainTaskTraceExecutable struct{ calls *int }

func (v mainTaskTraceExecutable) MarshalJSON() ([]byte, error) {
	*v.calls++
	return []byte(`{"callback":"ran"}`), nil
}

// Native data/ownership guards for the new task wire fields. These are not
// additional upstream-body parity claims.
func TestMainTaskTraceCodecOwnership(t *testing.T) {
	l := DefaultLimits()
	parent := goai.TelemetryCarrier{"trace": "parent"}
	linked := []goai.TelemetryCarrier{{"trace": "link"}, {}}
	task := Task{ID: 1, Conversation: 1, Kind: "task.trace-codec", Status: "pending", Checkpoint: JSON{"phase": "work"}, Trace: parent, Links: &linked}
	owned, err := copyTask(task, l)
	if err != nil {
		t.Fatal(err)
	}
	parent["trace"] = "caller-mutated"
	linked[0]["trace"] = "caller-mutated"
	linked[1]["new"] = "caller-mutated"
	mainTaskTraceFields(t, owned, goai.TelemetryCarrier{"trace": "parent"}, []goai.TelemetryCarrier{{"trace": "link"}, {}})
	canonical, err := CanonicalTask(owned, l)
	if err != nil {
		t.Fatal(err)
	}
	canonical.Trace.(goai.TelemetryCarrier)["trace"] = "public-mutated"
	(*canonical.Links)[0]["trace"] = "public-mutated"
	mainTaskTraceFields(t, owned, goai.TelemetryCarrier{"trace": "parent"}, []goai.TelemetryCarrier{{"trace": "link"}, {}})
	data, err := encodeBounded(owned, l, l.MaxRecordBytes)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Task
	if err := decodeStrict(data, l, l.MaxRecordBytes, &decoded); err != nil {
		t.Fatal(err)
	}
	roundtrip, err := copyTask(decoded, l)
	if err != nil {
		t.Fatal(err)
	}
	mainTaskTraceFields(t, roundtrip, goai.TelemetryCarrier{"trace": "parent"}, []goai.TelemetryCarrier{{"trace": "link"}, {}})
	for _, present := range []bool{false, true} {
		empty := owned
		empty.Trace = nil
		empty.Links = nil
		if present {
			empty.Trace = goai.TelemetryCarrier{}
			items := []goai.TelemetryCarrier{}
			empty.Links = &items
		}
		value, err := copyTask(empty, l)
		if err != nil {
			t.Fatal(err)
		}
		wire := mainTaskTraceWire(t, value)
		if present {
			if string(wire["trace"]) != "{}" || string(wire["links"]) != "[]" {
				t.Fatal("lost present-empty fields", wire)
			}
		} else if wire["trace"] != nil || wire["links"] != nil {
			t.Fatal("invented absent fields", wire)
		}
	}
	marker := owned
	marker.Trace = "root"
	marker.Links = nil
	marker, err = copyTask(marker, l)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(mainTaskTraceWire(t, marker)["trace"]); got != `"root"` {
		t.Fatal("raw root sentinel", got)
	}
	calls := 0
	cycle := map[string]any{}
	cycle["self"] = cycle
	for _, value := range []any{"wrong", 1, true, []any{}, map[string]any{"n": 1}, map[string]any{"n": nil}, cycle, mainTaskTraceExecutable{&calls}, (*goai.TelemetryCarrier)(nil), goai.TelemetryCarrier(nil)} {
		bad := owned
		bad.Trace = value
		if _, err := copyTask(bad, l); err == nil {
			t.Errorf("accepted invalid trace %T", value)
		}
	}
	if calls != 0 {
		t.Fatal("caller marshaler executed", calls)
	}
	for _, items := range [][]goai.TelemetryCarrier{nil, {nil}} {
		bad := owned
		bad.Links = &items
		if _, err := copyTask(bad, l); err == nil {
			t.Fatal("accepted null links/carrier")
		}
	}
	for _, raw := range []string{`{"bad":1}`, `{"bad":null}`, `{"bad":true}`, `[{}]`, `"not-root"`} {
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		bad := owned
		bad.Trace = value
		if _, err := copyTask(bad, l); err == nil {
			t.Fatal("invalid JSON carrier", raw)
		}
	}
	bad := owned
	bad.Trace = goai.TelemetryCarrier{"large": strings.Repeat("x", l.MaxStringBytes+1)}
	if _, err := copyTask(bad, l); err == nil {
		t.Fatal("carrier bypassed string budget")
	}
	if !reflect.DeepEqual(owned.Trace, goai.TelemetryCarrier{"trace": "parent"}) {
		t.Fatal("failed copies mutated source", owned.Trace)
	}
}
