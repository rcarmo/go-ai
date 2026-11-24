# llama.cpp classifier: pinned offline cases

The [Oracle receipt](llama-cpp-classify.json) records **15 passes, no failures, skips, or todos** for the entire unchanged `packages/ai/test/llama-cpp-classify.test.ts` at pi v1.6.0. The probe verifies the test's bytes against the [Round 21 queue](primary-queue-r21.jsonl), stages 12 exact source files and 44 retained dependency placements, and denies Node network permission. The original supplies a fake fetch and synthetic responses; it needs no catalogue, model server, or credentials. Reducing the middle readout depth from `4096` to `4095` in the scratch adapter caused exactly the readout-escalation test at line 247 to fail; the other 14 passed.

## Native comparisons

[`TestSourceV160LlamaCPPClassifier`](../../../tests/classifier_llama_cpp_upstream_test.go) uses `goai.Classify` with an injected HTTP transport. It captures the actual request body and headers and returns the pinned fixture responses. The mismatched-API case invokes the public registered llama.cpp provider directly, as the original does, rather than dispatching to the other API. No actual server is contacted.

| Pinned case lines | What the native comparison checks |
| --- | --- |
| 117, 157, 201 | Exact request fields and auth, all three answer types, exact JSON prompt text in the supplied order, shared prefix, and final question instructions |
| 216, 229 | Temperature scaling and zero rejection before any request |
| 238, 257, 269, 292 | Exact depth order, no invented answers, closed thinking marker, split-token fallback and rejection, and label token cache reuse |
| 306, 325, 352, 373 | Exact bounds and API diagnostics, completion-only request hooks, HTTP status/body detail, and cancellation |
| 381, 387 | URL normalization through the actual completion request, expected score and confidence from supplied logits |

The first native run used Go map order. The [later ordered comparison](../README.md#explicit-llamacpp-question-and-choice-order) supplies the source question and choice order explicitly, matches the prompt bytes, and attaches the supplied logits to the source choice keys. Ordinary Go maps still use sorted order. The native tests compare the probabilities through the public classifier against an independent formula.

The compiled native baseline failed five subtests. Explicit zero was treated as omission and made 22 mock requests; malformed label, bounds and API details had different capitalization; the HTTP 400 message lost the server body. [`ClassifierOptions.TemperatureOverride`](../../../classifier_runtime.go) now provides explicit presence for llama.cpp, while retaining existing nonzero callers and the default of 1. The [adapter](../../../classifier_llama_cpp.go) uses the pinned diagnostics and formats bounded HTTP error bodies through the existing provider error normalizer.

## Reproduce

```sh
inventory=docs/upstream-v1.6.0/test-inventory
bash "$inventory/probe-llama-cpp-classify.sh" "$pinned_git_mirror" "$retained_oracle" \
  > /tmp/pi-v160-llama-cpp-classify.json
cmp "$inventory/llama-cpp-classify.json" /tmp/pi-v160-llama-cpp-classify.json
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./tests \
  TEST_FLAGS='-run LlamaCPP -count=2 -shuffle=42 -p 1 -timeout 65s'
```

The Oracle probe acquires the shared lock itself. The receipt reproduced byte-for-byte. The equivalent native depth mutation failed only the escalation comparison; after restoration, focused Go tests passed twice with CGO disabled. Vet, Staticcheck `0.8.2`, ShellCheck, and diff checks passed. This packet does not test a live llama.cpp server.
