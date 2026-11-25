package durable

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

const v160MissingNamespaceMember = `await models.generateImage();`
const v160ExistingNamespaceMember = `return await models.generateImages();`
const v160NamespaceMemberDiagnostic = `models.generateImage does not exist. Did you mean models.generateImages? Check for a member with "generateImage" in models.`

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:525–539.
func TestSourceV160CodemodePublishedMissingNamespaceMember(t *testing.T) {
	root := officialWorkerPackageRootV160(t)
	for _, placement := range []string{"inline", "portable", "node-worker"} {
		t.Run(placement, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", "testdata/worker-namespace-missing-v160.mjs", placement, v160MissingNamespaceMember, v160ExistingNamespaceMember)
			cmd.Env = append(os.Environ(), "GOAI_WORKER_PACKAGE_ROOT="+root)
			raw, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("published namespace probe failed: %v\n%s", err, raw)
			}
			var got struct {
				Miss struct {
					OK    bool
					Error *quickjs.Error
				}
				Hit struct {
					OK    bool
					Value string
				}
				AfterMiss, Callbacks int
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("published namespace probe returned invalid JSON: %v\n%s", err, raw)
			}
			if got.Miss.OK || got.Miss.Error == nil || got.Miss.Error.Kind != quickjs.ErrorScript || got.Miss.Error.Message != v160NamespaceMemberDiagnostic || got.AfterMiss != 0 {
				t.Errorf("missing member: ok=%v error=%v callbacks=%d; want exact diagnostic and no effect", got.Miss.OK, got.Miss.Error, got.AfterMiss)
			}
			if !got.Hit.OK || got.Hit.Value != "callback" || got.Callbacks != 1 {
				t.Errorf("existing member: ok=%v value=%q callbacks=%d; want one effect", got.Hit.OK, got.Hit.Value, got.Callbacks)
			}
		})
	}
}

func TestV160CodemodeNativeMissingNamespaceMember(t *testing.T) {
	run, _, callbacks := newV160GlobalAdmissionEngine(t)
	names := []string{"models.classify", "models.generateImages"}
	miss := run(names, v160MissingNamespaceMember)
	if miss.OK || miss.Error == nil || miss.Error.Kind != quickjs.ErrorScript || miss.Error.Message != v160NamespaceMemberDiagnostic || callbacks.Load() != 0 {
		t.Errorf("missing member: ok=%v error=%v callbacks=%d; want exact diagnostic and no effect", miss.OK, miss.Error, callbacks.Load())
	}
	hit := run(names, v160ExistingNamespaceMember)
	if !hit.OK || string(hit.Value) != `"callback"` || callbacks.Load() != 1 {
		t.Errorf("existing member: ok=%v value=%s error=%v callbacks=%d; want one effect", hit.OK, hit.Value, hit.Error, callbacks.Load())
	}
}
