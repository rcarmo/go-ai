# Classifier contract correction — issue #1

Replacement local receipt after the auditor's response-hook ordering finding for [go-ai #1](https://github.com/rcarmo/go-ai/issues/1), required by [gi #27](https://github.com/rcarmo/gi/issues/27). This is separate from the accepted v1.0.0 runtime audit. No commit, push, hosted CI, tag or publication is authorised by this receipt.

## Pins and scope

- Local base: `05edf10e5bcd284261629b77e56922a04aa6282b` (`HEAD == origin/main` at start).
- Accepted v1.0.0 runtime: `6795b5235ecd04110c838e48d5958b514f283996`; the issue candidate does not amend or replace it.
- Official package: `@earendil-works/pi-ai@1.0.0`, gitHead `a13d35a742c6ef8462812a28fbe1d8c8b7431c32`.
- Official artifact: `/workspace/tmp/pi-ai-100.tgz`, SHA-256 `f39b99c29b8598f175b10840e5d2a81983e7c0ce5cae4d7df83a1007447d2c2b`.
- Reference files below are under `/workspace/tmp/pi-ai-audit-100/tar/package/dist/`.
- Catalogs, dependency manifests, OAuth, chat/image transports and release tools are unchanged.
- Exact nine-path scope: `classifier_types.go` (new), `classifier_runtime.go`, `classifier_llama_cpp.go`, `tests/classifier_contract_issue1_test.go` (new), `tests/classifier_llama_cpp_issue1_test.go` (new), `tests/classifier_runtime_v0991_test.go`, `tests/classifier_llama_cpp_v0991_test.go`, `docs/issues/classifier-1.md` (new), and `RELEASE.md` (separate issue receipt).

## Contract and production-path crosswalk

| Official reference | Go correction | Production/public proof |
| --- | --- | --- |
| `types.d.ts` JSON object state and question union | `classifier_types.go`: object-only state; nested JSON domain validation; `UseNumber` avoids round-trip integer loss; questions require instructions and discriminator-specific criteria | `TestIssue1ClassifierPublicJSONRoundTripAndZeroAnswers`, `TestIssue1ClassifierRejectsMalformedPublicJSON`, `TestIssue1ClassifierRejectsNonJSONStateBeforeTransport` |
| `types.d.ts` answer union | Required zero-valued answer fields are emitted; wrong/missing/null fields, non-finite numbers and unknown discriminators reject; only public `bool` JSON, never wire `noul` | Public round-trip/malformed tests; `TestIssue1ClassifierMalformedWireAnswersKeepUsage`, `TestIssue1ClassifierOverflowAnswerPreservesBilledUsage` |
| `api/system-one-shared.js` wire request/answer parsing and usage | Public bool maps to wire `noul` only; all question meanings and instructions remain; choice probabilities/confidence and score/confidence required; malformed/overflowing answers retain billed usage; reported zero-token usage remains present | `TestIssue1ClassifierSystemOneTransportContracts`, malformed-wire and response-hook/usage tests; retained v0991 retry/timeout/cancel tests |
| `api/typesafe-system-one.js` | TypeSafe/OpenRouter-compatible flat `{model,state,questions}` POST to `/systemone` | Transport-contract HTTP fixture for TypeSafe API |
| `api/cloudflare-workers-ai-system-one.js` | `{model,input:{state,questions}}` POST to `/run`; unwrap completed result and reject unsuccessful/malformed envelopes | Transport-contract HTTP fixture; `TestIssue1CloudflareClassifierRejectsMalformedEnvelopes`; retained account-placeholder fixture |
| `api/llama-cpp-classify.js` rendering | Structured state twice, all questions overview, per-question instructions/meanings, labelled options/levels/Yes-No meanings, full state-is-data system prompt | `TestIssue1LlamaCPPStructuredPromptAndNumericalAnswers` captures real `/apply-template` request |
| `api/llama-cpp-classify.js` readout | Router model on every request, thinking off, close trailing think block, one-token pre-sampling probabilities, positive finite temperature, option bounds, missing-label escalation and all-underflow rejection | Structured-prompt/numerical, option-bounds, readout-error tests; retained cache/escalation/choice/score/cancel/timeout tests |
| Provider headers/hooks | Apply defaults, model headers, then caller headers to case-insensitive HTTP headers; propagate marshal and response-hook errors. Dispatch response hooks only after successfully reading and decoding the entire 2xx JSON response, before transport/answer semantics; fail read errors and bodies over the existing 1 MiB bound | Transport-contract mixed-case headers; response-hook and invalid-hook-payload fixtures; `TestIssue1SystemOneResponseHookDispatchAfterDecodedSuccess`, `TestIssue1LlamaCPPResponseHookDispatchAfterDecodedSuccess` |

## Go adaptations and migration

`ClassifierContext.State` was previously `string`; `ClassifierQuestion.Choices` was a string slice, without question instructions or criterion meanings. Those shapes cannot represent the official contract. This issue intentionally corrects that source API: callers must wrap text in an object and supply instructions plus criteria. There is no silent string/Choices fallback.

```go
ctx := goai.ClassifierContext{
    State: map[string]any{"text": "message to judge", "metadata": map[string]any{"trusted": false}},
    Questions: map[string]goai.ClassifierQuestion{
        "kind": {Type: "choice", Instructions: "Choose the message kind", Criteria: map[string]string{"request": "asks for action", "note": "provides information"}},
        "quality": {Type: "score", Instructions: "Rate quality", Criteria: []string{"poor", "adequate", "good"}},
        "safe": {Type: "bool", Instructions: "Is the message safe?", Criteria: goai.ClassifierBoolCriteria{True: "safe to process", False: "unsafe to process"}},
    },
}
```

`Criteria` accepts the concrete forms above; JSON decoding restores them. A bool object with string `true`/`false` keys also works. Instructions may be empty, as may criterion descriptions; they must have string type when decoding JSON. Empty questions, choice criteria and score criteria are permitted by the public/System One contract; llama.cpp enforces its own 2–62 choices / 2–10 score levels before network calls.

Go maps do not retain insertion order. llama.cpp sorts question IDs and choice keys, consistently using that order for overview, labels, prompt options, winner/ties and probability mapping. This is a documented native adaptation, not a claim of JS insertion-order identity. Score levels retain array order. Prompt JSON whitespace/escaping follows Go `encoding/json`; field values, repetition and task structure are tested, not generated-output hashes. The HTTP prompt fixture decodes the rendered state and checks that `<p>judge & retain</p>` survives semantically.

State accepts recursive JSON values, including typed string-key maps/slices, numbers and null. It rejects structs, pointers, byte slices, non-string-key maps, non-finite numbers and cycles instead of relying on lossy `encoding/json` coercion. Large JSON integer text survives the validation/normalisation path. Answer numeric validation matches official finite-number requirements; it does not add range/sum/key-membership restrictions absent upstream.

Existing Go option conventions remain: zero temperature means default 1, and retry zero values retain the existing no-retry configuration. This is not an issue-specific retry API redesign. Usage token counts remain integer-valued in the Go `Usage` type. No live credentials or llama-server model were used; HTTP fixtures exercise the real production transport and independent analytic softmax/expectation references at absolute tolerance `1e-12` (well above double-precision rounding for these short reductions).

## Local validation

Replacement logs: `/workspace/tmp/go-ai-issue1-review2/{focused,focused-race,check,repro,race,shuffle,catalog,catalog-fault,diff-check}.log`. Earlier `/workspace/tmp/go-ai-issue1-local/` logs cover the initial candidate only.

All commands below passed again after the hook-ordering and body-read correction:

```sh
go test ./tests -run 'Test(Issue1|V0991.*Classifier)' -count=3 -v
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./tests -run 'Test(Issue1|V0991.*Classifier)' -count=3
make check
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
TMPDIR=/workspace/tmp go test -shuffle=on ./...
make test-repro
PI_AI_MODELS_GENERATED_JS=/workspace/tmp/pi-ai-audit-100/tar/package/dist/models.generated.js ./scripts/check-model-regeneration.sh
PI_AI_MODELS_GENERATED_JS=/workspace/tmp/pi-ai-audit-100/tar/package/dist/models.generated.js python3 scripts/test-check-model-regeneration.py
git diff --check
```

`make check` covers full deterministic tests (`-count=3`), vet/staticcheck, logging, release inventory/fault tests, regeneration/fault tests, SBOM validation/self-tests, publisher checks, pinned vulnerability checks and licence checks. `make test-repro` repeats full fast gates, build and race tests. Pinned catalogs regenerate at 1532 chat / 57 image / 15 classifier; deliberate corruption tests fail as expected. No dependencies changed. `govulncheck` reports no reachable vulnerabilities; licence scans pass with the existing assembly-inspection warnings. Local SBOM is a working-tree gate artifact at the base revision, not release provenance for an uncommitted candidate.

A final raw-HTTP overflow fixture (`1e400`) proved that numeric conversion must occur after usage parsing, not while decoding the whole JSON document; System One now uses `UseNumber` and validates numbers in the answer parser. All gates above were rerun after this correction.

The auditor found both response hooks executing before status/body decoding. Corrected ordering is: read complete bounded body, check 2xx, decode entire JSON, invoke the observed hook, then parse transport/answer semantics. Short-body read errors, oversized bodies, non-2xx, invalid syntax and trailing JSON suppress the hook. Valid JSON scalar/array/null and malformed objects still invoke the observed hook before semantic rejection; System One malformed answers retain usage unless the hook itself fails. llama.cpp only observes `/completion`, preserving the official unobserved `/tokenize` and `/apply-template` requests.

Two new table-driven tests cover 53 cases: TypeSafe and Cloudflare each exercise 11 dispatch outcomes; llama.cpp exercises those outcomes across all three endpoints (hook errors only on observed completion). Scalar/array/null llama replies produce stable endpoint-specific errors without raw-body content. A deliberately short `Content-Length` response contains otherwise valid complete JSON, ensuring the discarded-read-error bug cannot pass accidentally.

The candidate now contains 13 issue-specific tests plus the migrated v0991 regressions. An independent delegated review of the replacement hook ordering, body reads, semantic errors and usage preservation found no concrete issues. Earlier initial-candidate failures were corrected: test-only bool JSON key-order assertion; case-insensitive authorization precedence relying on map iteration; two staticcheck ST1005 literal error messages. An earlier narrow JSON/state/usage review found no concrete issues but did not detect the later auditor hook-ordering finding. Timed-out or path-rejected delegates provide no acceptance evidence.

Cross-port coordination is owned by `@auditor`: Swift transport/contract gaps were identified for separate bounded follow-up; Rust contract appeared compliant and verification was requested. Their completion is not claimed here. The issue candidate still requires auditor scope/test acceptance and separate commit/push authorisation. Publication HOLD remains in force.
