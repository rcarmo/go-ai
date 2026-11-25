package durable

import (
	"testing"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

var v160InvalidGlobalNames = []struct{ name, native string }{
	{"a.b.c", "Invalid global name"},
	{"a.", "Invalid global name"},
	{".a", "Invalid global name"},
	{"tools.x", `Invalid global name "tools"`},
	{"store.x", `Invalid global name "store"`},
	{"a.not-valid", "Invalid global name"},
	{"not-valid", "Invalid global name"},
	{"tools", `Invalid global name "tools"`},
	{"console", `Invalid global name "console"`},
}

const v160GlobalNamespaceCollision = "models collision"

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:541–557.
func TestSourceV160CodemodePublishedGlobalAdmission(t *testing.T) {
	root := officialWorkerPackageRootV160(t)
	for _, placement := range []string{"inline", "portable", "node-worker"} {
		t.Run(placement, func(t *testing.T) {
			cases := map[string][]string{v160GlobalNamespaceCollision: {"models", "models.list"}}
			for _, tc := range v160InvalidGlobalNames {
				cases[tc.name] = []string{tc.name}
			}
			got := runV160PublishedGlobalAdmission(t, root, placement, cases)
			for _, tc := range v160InvalidGlobalNames {
				if want := `Invalid global name "` + tc.name + `"`; got.Rejected[tc.name] != want {
					t.Errorf("constructor with %q: %q, want %q", tc.name, got.Rejected[tc.name], want)
				}
			}
			if want := `Global "models" conflicts with the namespace "models"`; got.Rejected[v160GlobalNamespaceCollision] != want {
				t.Errorf("namespace collision = %q, want %q", got.Rejected[v160GlobalNamespaceCollision], want)
			}
		})
	}
}

func TestV160CodemodeNativeGlobalAdmission(t *testing.T) {
	run, acquired, callbacks := newV160GlobalAdmissionEngine(t)
	check := func(t *testing.T, names []string, want string) {
		t.Helper()
		before := acquired.Load()
		got := run(names, `return "unreachable";`)
		if got.OK || got.Error == nil || got.Error.Kind != quickjs.ErrorSandbox || got.Error.Message != want || acquired.Load() != before || callbacks.Load() != 0 {
			t.Errorf("names %q: ok=%v value=%s error=%v reservations=%d callbacks=%d; want %q before worker capacity", names, got.OK, got.Value, got.Error, acquired.Load()-before, callbacks.Load(), want)
		}
	}
	for _, tc := range v160InvalidGlobalNames {
		t.Run(tc.name, func(t *testing.T) { check(t, []string{tc.name}, tc.native) })
	}
	t.Run(v160GlobalNamespaceCollision, func(t *testing.T) {
		check(t, []string{"models", "models.list"}, "global conflicts with the namespace")
		check(t, []string{"models.list", "models"}, "global conflicts with the namespace")
	})
	before := acquired.Load()
	if got := run([]string{"models.list"}, `return await models.list();`); !got.OK || string(got.Value) != `"callback"` || acquired.Load()-before != 1 || callbacks.Load() != 1 {
		t.Fatalf("valid namespace member: ok=%v value=%s error=%v reservations=%d callbacks=%d", got.OK, got.Value, got.Error, acquired.Load()-before, callbacks.Load())
	}
}
