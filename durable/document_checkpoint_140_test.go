package durable

import "testing"

func TestCheckpoint140ExplicitPolicyRequired(t *testing.T) {
	for _, family := range []bool{false, true} {
		options := DefinitionOptions{Kind: "checkpoint.explicit-140", Version: 1, Scope: "session", Family: family, Initial: func(JSON) (JSON, error) { return JSON{}, nil }}
		if _, err := DefineDocument(options); err == nil {
			t.Fatal("missing checkpoint policy accepted", family)
		}
		calls := 0
		options.CheckpointWhen = func(JSON, []Operation, CheckpointInfo) (bool, error) { calls++; return false, nil }
		if _, err := DefineDocument(options); err != nil {
			t.Fatal(err)
		}
		if calls != 0 {
			t.Fatal("definition construction evaluated checkpoint code")
		}
	}
}
