package openroutercost

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

var benchmarkUsage goai.Usage

func BenchmarkReportedCostMain(b *testing.B) {
	for _, test := range []struct{ name, wire string }{{"non-byok", `{"cost":0.1,"is_byok":false}`}, {"byok-components", `{"cost":0.1,"is_byok":true,"cost_details":{"upstream_inference_cost":null,"upstream_inference_prompt_cost":0.6,"upstream_inference_completions_cost":0.3}}`}} {
		b.Run(test.name, func(b *testing.B) {
			var reported Accounting
			if err := json.Unmarshal([]byte(test.wire), &reported); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				usage := goai.Usage{Cost: goai.CostBreakdown{Total: 1}}
				Apply(&usage, reported)
				benchmarkUsage = usage
			}
		})
	}
}

func TestMainCostProvenanceSnapshotIsolation(t *testing.T) {
	billed := true
	message := &goai.Message{Usage: &goai.Usage{Cost: goai.CostBreakdown{TotalFromProvider: &billed}}}
	snapshot := goai.SnapshotEvent(&goai.TextDeltaEvent{Partial: message}).(*goai.TextDeltaEvent)
	*snapshot.Partial.Usage.Cost.TotalFromProvider = false
	if !*message.Usage.Cost.TotalFromProvider {
		t.Fatal("snapshot aliased cost provenance")
	}
	original := &goai.Context{Messages: []goai.Message{*message}}
	clone := goai.CloneContext(original)
	*clone.Messages[0].Usage.Cost.TotalFromProvider = false
	if !*original.Messages[0].Usage.Cost.TotalFromProvider {
		t.Fatal("context clone aliased cost provenance")
	}
	for _, flag := range []*bool{nil, new(bool)} {
		wire, err := json.Marshal(goai.CostBreakdown{TotalFromProvider: flag})
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(wire, &value); err != nil {
			t.Fatal(err)
		}
		field, present := value["totalFromProvider"]
		if present != (flag != nil) || present && field != false {
			t.Fatalf("optional provenance field: %#v", value)
		}
	}
}
