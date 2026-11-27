# Codemode namespace prompt and portable guest authority

All seven cases in the byte-verified, unchanged `packages/durable/test/codemode-describe.test.ts` passed on Node `v26.13.0` and Vitest `4.1.12` before the native test edit. There were no skips or failures. The test, Vitest configuration and lockfile matched Git at pi v1.6.1 `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`; the original test SHA-256 was `9b16f48a954ba3b99ef7ea0867eacccde1fbf9ac05235f8f4ecae1baf88ca9d9`. This packet maps the public system-prompt case only (`109–141`).

The native translation reads the first real `pi.system` message after a public Harness submission on SQLite. The section lists `read`, the `mcp__github` heading and description, and `list_issues`; it omits the deferred `get_issue` and Codemode itself. The emitted tool declaration carries the exact pinned Lark source grammar. The Go baseline passed without a production change.

The supplemental guest script checks that `"codemode" in tools` is false and calls the deferred `get_issue` with the exact fixture path. The model receives `[false,"deferred result"]`, with one guest start, one deferred callback and one reply. The script uses membership because the published guest throws on a missing property read, including `typeof tools.codemode`. The v1.6.1 package root matched fresh scratch extraction of the retained pi-codemode `1.6.1` archive (`69b391796f465fb7a3432c8aa5b119a0b9983d3c910aa04e2619f7f294e7b7ac`) and QuickJS-WASI `3.6.2` archive (`f1f4349f19a2d849e33ea0ae9bec2e7062b8839f4eceb17c9051ddbaa2720982`).

The checked-in scratch controls expose deferred tools in the prompt or replace the grammar string; each expects the specific prompt or grammar assertion to fail. This checks publication and callable authority without changing module access, ownership, or redaction.

## Reproduce the selections

```sh
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3
export GOAI_CODEMODE_V161_PACKAGE_ROOT
(
  cd "$oracle/packages/durable"
  flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn \
    node ../../node_modules/vitest/vitest.mjs run test/codemode-describe.test.ts --pool=forks --maxWorkers=1
)
flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn make test PACKAGES=./durable \
  'TEST_FLAGS=-v -run "^Test(SourceV161CodemodePublishedNamespacePrompt|SourceV160CodemodeNamespacePromptProjection|SourceV160CodemodeNamespaceDeferredPrompt)$$" -count=2 -shuffle=42 -p 1 -timeout 80s'
bash docs/upstream-v1.6.1/probe-codemode-namespace-prompt.sh
```
