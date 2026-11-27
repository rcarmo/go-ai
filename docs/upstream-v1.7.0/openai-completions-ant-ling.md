# OpenAI Completions Ant Ling compatibility

## Pinned originals

The v1.7.0 `packages/ai/test/openai-completions-tool-choice.test.ts` selection passed all 48 tests with no skips on Node v26.13.0 and Vitest 4.1.12. This receipt keeps the original file SHA-256 at `961e6e605ccfd5bfa46d3ef5dafc7031f27dce46883215b376fdf507ffd9f7f0` for upstream commit `fb0fe823539f6d04850f6e34748c01f01f212e1f`; it does not rewrite an earlier receipt.

The Ant Ling cases at lines 1828–1901 verify the published v1.7.0 compatibility metadata, the deprecated `ant-ling` format alias, selectable levels, and the on/off DeepSeek-compatible `thinking.type` control. Their source bytes matched the pinned Git object and the oracle stage used by the 48-test run.

## Native port

`TestCompletionsAntLingThinkingCompatibility` now uses the registered `Ling-3.1-flash` record and the public simple/direct Completions paths. The port normalizes an explicit `ant-ling` format to `deepseek`, infers `deepseek` for Ant Ling provider IDs and URLs, and enables thinking whenever a reasoning option is requested even if no effort can be mapped. Turning reasoning off emits `thinking.type: disabled` unless the model explicitly disallows the off control. The retired v1.6 model fixture and ID-specific route branch were removed rather than preserved as a second contract.

The focused test passed twice with shuffle seed 42. Three compiled negative controls failed as expected: requiring a mapped effort before enabling DeepSeek thinking, retaining the explicit `ant-ling` alias, and dropping Ant Ling inference. The full OpenAI provider package passed once; package-scoped `go vet` and Staticcheck v0.8.2 also passed with `CGO_ENABLED=0`, `-p 1` and bounded timeouts.

## Policy boundary

This closes the Ant Ling delta in the changed Completions suite only. It does not close the wider v1.7 provider, Durable, Codemode, Schema, live-credential, acceptance-profiling or release gates recorded in the v1.7 inventory.
