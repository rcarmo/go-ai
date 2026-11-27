# Bedrock reasoning replay and flat effort

Bedrock now sends unsigned reasoning blocks for models such as Nova and omits earlier reasoning when `BedrockCompat.SupportsReasoningReplay` is explicitly false. For reasoning-enabled models outside the Claude and GPT families it sends only explicitly mapped `reasoning_effort` values. With reasoning unset, Kimi K3.1 sends `none`; GLM 5.3 omits the field. The Claude and GPT branches retain their existing payload shapes.

## Oracle and native checks

Thirteen unchanged originals passed on the verified, locked v1.7.0 AI stage: eleven `Bedrock flat reasoning_effort payload` cases and two `Bedrock reasoning replay` cases. The selector filtered 39 unrelated cases. Both test files, the Converse adapter, compatibility schema, runner config and npm lock matched Git bytes at `fb0fe823539f6d04850f6e34748c01f01f212e1f`. The tests ran with Node 26.13.0 and Vitest 4.1.12, an empty home, no network and no provider credentials. A separate recheck matched 481 tracked AI source and test, Schema source and Telemetry source blobs in the stage.

The native tests capture the `ConverseStreamInput` through public `goai.Stream` and abort before the provider request, preserving the capture error identity. On unchanged Go `4bbf033e360745c3f41bceaaec84f381d6c880db`, 15 of 20 checks failed: ten mapped efforts and Kimi's `off` value were absent, Nova's reasoning was sent as text, and all three Kimi replay cases retained reasoning. The unchanged native supported-level checks and both omission controls passed. The redacted Kimi case is supplemental to the originals.

All 61 top-level Bedrock provider tests passed twice in the same shuffled process, without skips. Three compiled Go overlays reverted replay opt-out, Nova's unsigned reasoning member and mapped `off` independently; each failed its exact request assertion. Scoped vet and Staticcheck v0.8.2 passed for the root package and Bedrock, using Go 1.27.3 with CGO disabled. No AWS request was made for these cases.

## Catalog input boundary

The [test fixture](../../inference/provider/bedrock/testdata/bedrock_reasoning_v170.json) projects `id`, `name`, `reasoning`, `compat` and `thinkingLevelMap` for the five Kimi and GLM records from the published Bedrock data. That provider payload matches the release source and verified AI npm distribution (SHA-256 `8446d4d944ed66a46b39e320cd6e811a20f3314ea186274f985333a240fbae98`); the projection has SHA-256 `05e793e10cbce3e2a8d1fd9b40fa595db74b655c7542ef624f788e15f5f5eb3d`. Go projects the public compatibility type into `Model.BedrockCompat`.

The adapter comparisons use the verified fixture fields. The MAI packet then added `TestBedrockReasoningCatalogPayload`, which calls `RegisterBuiltinModels` and obtains each model through `GetModel`, rather than using only the projected fixture. The exact base of this lane, `15d9040d37157d03fc739d82b89d8a9ad25aa5be`, already contained the matching generated Bedrock records. No production change is required by this test.

## Registered catalog receipt

Against unchanged production at the exact lane base, the published-source oracle receipt under `/tmp/go-ai/v170-bedrock-catalog.8XFxMM/originals.exit` reports exit zero; its report records 51 passing tests and one credential test filtered. The direct catalog test passed twice with `CGO_ENABLED=0` and `-run '^TestBedrockReasoningCatalogPayload$' -count=1`. For `global.moonshotai.kimi-k3` and `us.moonshotai.kimi-k3.1`, registered lookup matches the published `supportsReasoningReplay=false` flags and the payload omits earlier reasoning. For `global.moonshotai.kimi-k3.1`, `us.moonshotai.kimi-k3.1` and `global.zai.glm-5.3`, registered lookup matches the published thinking-level maps and emits the sole `reasoning_effort=medium` field.

Three compiled controls failed at their intended native assertions: allowing replay retained both Kimi reasoning blocks, rendering unsigned reasoning as text changed Nova's member type, and omitting the `off` mapping removed Kimi's `reasoning_effort=none`. The full `make vet` and `make staticcheck` gates passed with Go 1.27.3 and `CGO_ENABLED=0`. These direct registered-catalog checks upgrade the two previously partial locators to mapped. Nova 2's separate `reasoningConfig` shape and live Bedrock execution remain outside these checks; no `reasoningConfig` implementation was added or inferred.
