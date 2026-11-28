# Shared prepared schema checks

## Pinned original and native behavior

This slice is based exactly on native main `393e8ed78fbcef88b22a58a7447a08d55dd5f402` and the official pi v1.7.0 commit `fb0fe823539f6d04850f6e34748c01f01f212e1f`. The unchanged `packages/schema/test/shared.test.ts` Git blob is `ba3e4ab30e18b587b17088e035866144d85a936f`, with SHA-256 `cdde5a66a4e2aaa169c07d4baa3d5f43efbaa9de48ad210656a4314034be6ec4`. The detached Oracle checkout remained at the pinned commit and the file was byte-checked before the original run. Vitest 4.1.12 on Node 26.13.0 passed the complete two-test file under `/tmp/go-ai/v170-focused-verify.lock`.

`TestSourceV170SchemaSharedBareChecks` translates the observable behavior around the bare-string shared check through `CompileJSONSchema` and `CompiledJSONSchema.Check`. JavaScript check-function identity is not an observable Go API contract, so that internal identity assertion is partial rather than mapped. `TestSourceV170SchemaSharedPatternChecksSurviveCollection` prepares the original 50 distinct patterns plus the retained pattern, releases the unused handles, forces collection, and verifies that the retained public validator still accepts and rejects the source values.

The native tests passed against unchanged production code. No semantic fix was demonstrated. The shared-regex count and JavaScript `WeakRef` collection assertions have no public Go equivalent and are recorded explicitly in the locator ledger rather than replaced with white-box cache counters.

## Verification and bounds

The two native tests passed once after the test port and twice with `-shuffle=42 -count=2`. `make vet` and Staticcheck v0.8.2 passed with `CGO_ENABLED=0`; all commands serialized through the shared v1.7 verification lock. This slice changes no production code, equality handling, absolute IRI checks, or IDN formats. It does not claim JavaScript cache identity, full Schema acceptance, profiling, provider coverage, or publication readiness.
