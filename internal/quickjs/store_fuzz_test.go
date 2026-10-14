package quickjs

import "testing"

func FuzzQuickJSStorePatch(f *testing.F) {
	for _, raw := range []string{`[]`, `[["key","null"]]`, `[["delete"]]`, `[[null]]`, `[["key","1"],["key"]]`} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		patch, err := parseStoreWrites(raw)
		if err != nil {
			if len(patch.Set) != 0 || len(patch.Delete) != 0 {
				t.Fatal("rejected patch contains effects")
			}
			return
		}
		seen := map[string]bool{}
		for _, key := range patch.Delete {
			if seen[key] {
				t.Fatal("duplicate deletion")
			}
			seen[key] = true
			if _, ok := patch.Set[key]; ok {
				t.Fatal("conflicting patch")
			}
		}
	})
}
