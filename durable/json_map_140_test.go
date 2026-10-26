package durable

import "testing"

type json140NamedMap map[string]any

func TestJSON140PlainMapPreservesGenericBudgetChecks(t *testing.T) {
	plain := map[string]any{"items": []any{JSON{"name": "fixture", "enabled": true}}, "nil": nil}
	generic := json140NamedMap{"items": []any{json140NamedMap{"name": "fixture", "enabled": true}}, "nil": nil}
	for _, name := range []string{"normal", "nodes", "depth", "members", "string", "bytes"} {
		t.Run(name, func(t *testing.T) {
			limits := DefaultLimits()
			budget := limits.MaxRecordBytes
			switch name {
			case "nodes":
				limits.MaxNodes = 3
			case "depth":
				limits.MaxDepth = 2
			case "members":
				limits.MaxMembers = 1
			case "string":
				limits.MaxStringBytes = 3
			case "bytes":
				budget = 5
			}
			a, ae := encodeBounded(plain, limits, budget)
			b, be := encodeBounded(generic, limits, budget)
			if (ae == nil) != (be == nil) {
				t.Fatal("plain/generic acceptance mismatch", ae, be)
			}
			if name != "normal" {
				if ae == nil {
					t.Fatal("budget bypass", name)
				}
				return
			}
			var av, bv JSON
			if err := decodeStrict(a, limits, budget, &av); err != nil {
				t.Fatal(err)
			}
			if err := decodeStrict(b, limits, budget, &bv); err != nil {
				t.Fatal(err)
			}
			if !equalJSONValue(av, bv) {
				t.Fatal("plain/generic semantic mismatch", av, bv)
			}
		})
	}
	cycle := JSON{}
	cycle["self"] = cycle
	if _, err := encodeBounded(cycle, DefaultLimits(), DefaultLimits().MaxRecordBytes); err == nil {
		t.Fatal("cycle accepted")
	}
}
