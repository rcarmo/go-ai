# Pi Decision cost-only usage and optional splits

The v1.7.0 Pi Decision adapter calls the shared `parsePiUsage` in `packages/ai/src/api/json-request.ts`. It accepts a finite reported total without input or output counters, preserves optional `reasoning` and `imageOutput` (including explicit zero), and estimates cost from the model when the server omits a billed total.

All five unchanged Pi Decision and eight unchanged Pi Image originals passed offline on the byte-verified v1.7.0 source. The six new [source-derived witnesses](pi-usage-witness.test.ts) are supplemental: they use the unchanged public Pi Decision adapter and the same [usage fixtures](../../tests/testdata/pi-usage.json) as Go. They are not upstream assertion sites or a new upstream test count.

On untouched `15d9040d37157d03fc739d82b89d8a9ad25aa5be`, Go dropped both cost-only cases and both reported-split cases; the two invalid/absent cases already passed. The native fix retains token counter presence and reported cost provenance. It uses the existing bounded integer representation and does not infer missing optional members. All six cases pass through `goai.Classify`; no registry, auth, or request changes were needed.

Twenty focused tests passed twice without skips. Compiled controls caught the old token-only gate and losses of each split; vet and Staticcheck v0.8.2 passed. A catalogue estimate confirms that the image split affects pricing without increasing the output token count.

To reproduce the supplemental source check, copy `pi-usage-witness.test.ts` and `tests/testdata/pi-usage.json` (as `pi-usage.fixture.json`) into a private verified AI stage's `packages/ai/test/`. Run Vitest on that file offline, after running the unchanged originals separately. In Go, use `make test PACKAGES=./tests TEST_FLAGS='-run TestPiDecisionUsageOptionalSplitsAndCost -v'`.
