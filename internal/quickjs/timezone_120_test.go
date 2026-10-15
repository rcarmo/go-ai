package quickjs

import (
	"context"
	"testing"
	"time"
)

func TestQuickJS120OrdinaryHostTimezoneAndDeterministicUTC(t *testing.T) {
	old := time.Local
	time.Local = time.FixedZone("test-east", 2*3600)
	defer func() { time.Local = old }()
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	for _, tc := range []struct {
		deterministic bool
		want          string
	}{{false, `[-120,14]`}, {true, `[0,12]`}} {
		result := engine.Execute(context.Background(), `const d=new Date('2024-01-01T12:00:00Z');return [d.getTimezoneOffset(),d.getHours()];`, Options{Deterministic: tc.deterministic, Timeout: 5 * time.Second})
		if result.Error != nil || string(result.Value) != tc.want {
			t.Fatal(tc.deterministic, string(result.Value), result.Error)
		}
	}
}
