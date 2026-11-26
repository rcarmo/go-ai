# Published Codemode output layout and absolute limits

Two unchanged Durable originals passed before the native test edit on Node `v26.13.0` and Vitest `4.1.12`: output layout (`codemode.test.ts:970–975`) and absolute output limits (`:990–1007`). The other 40 file cases were unselected. The prepared test, configuration and lockfile matched Git at `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`; the original test SHA-256 was `55453420e8773f75caa8f2167c1965e50553e8934c399d352e53555fe1e9662d`.

`TestSourceV161CodemodePublishedWorkerOutputLayout` uses the real v1.6.1 portable guest through the Node subprocess bridge, with a public Go Harness and SQLite. The receipt starts with `Script completed`, its measured wall-time line, and `Output:`. Its body contains exactly the numbered `one`, compact `{"two":2}`, and `three` items, in that order, followed by the single console block `log 1`. Moving the console ahead of the text in a scratch Go overlay fails the exact body comparison.

`TestSourceV161CodemodePublishedWorkerAbsoluteOutputLimits` runs the two original scripts with `max_output_tokens: 2000000`. The 6 MiB return has a truncation warning and at most 1 MiB plus 1,000 ASCII bytes of visible text. The 200,000-line return has a warning and fewer than 100,000 visible lines. The public receipt names a readable spill file in each case, and its content matches the complete original byte for byte. Cutting one byte from the saved 6 MiB body fails the test at 6,291,455 bytes. For the line fixture, raising the Go line ceiling alone does not distinguish the original: the regular inline text budget already keeps this ASCII fixture below 100,000 visible lines. This maps the observed output bound, not an independent line-limiter claim. The native 200,000-line receipt did not meet an extra exact inline path-hint check. The original asserts the structured path, and the native test reads that file in full. Inline hint formatting and the truncated preview's exact bytes are outside these source assertions. The separate native metadata-budget test keeps its existing path-hint checks.

The published package root matched fresh scratch extraction of both retained archives. pi-codemode `1.6.1` SHA-256 was `69b391796f465fb7a3432c8aa5b119a0b9983d3c910aa04e2619f7f294e7b7ac`; QuickJS-WASI `3.6.2` SHA-256 was `f1f4349f19a2d849e33ea0ae9bec2e7062b8839f4eceb17c9051ddbaa2720982`. This uses the portable entry, not the standalone Node worker topology.

Both translated cases passed without changing production. The old v1.6.0 output and metadata-budget tests and both new tests passed twice with shuffle seed 42 on Go `1.27.3`, CGO disabled. Vet and Staticcheck `0.8.2` passed for `./durable`. A third scratch overlay hid the saved path from the line receipt; the source-mapped truncation warning and spill readback checks both failed. No saved starts, redaction rules, package assets or source test bytes changed.

## Reproduce the selections

With the verified v1.6.1 Durable Node Oracle and the two published guest roots staged:

```sh
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3
export GOAI_CODEMODE_V161_PACKAGE_ROOT GOAI_CODEMODE_V160_PACKAGE_ROOT
(
  cd "$oracle/packages/durable"
  flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn \
    node ../../node_modules/vitest/vitest.mjs run test/codemode.test.ts --pool=forks --maxWorkers=1 \
    -t 'lays out text items, console lines, and the returned value under a header|keeps a result inside the tool.s output limits whatever max_output_tokens says, spilling the rest'
)
flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn make test PACKAGES=./durable \
  'TEST_FLAGS=-v -run "^Test(SourceV161CodemodePublishedWorker(OutputLayout|AbsoluteOutputLimits)|SourceV160CodemodePublishedWorkerOutput|CodemodeModelOutputMetadataBudgetV160)$$" -count=2 -shuffle=42 -p 1 -timeout 105s'
```
