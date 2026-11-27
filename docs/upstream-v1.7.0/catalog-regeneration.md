# v1.7.0 catalog regeneration verification

The v1.7.0 candidates match all 1,858 published records through the root Go registries: 1,701 chat, 74 image, and 83 classifier. This closes the field losses found by the [readiness audit](catalog-readiness.md). The verification used the exact npm tarball and an overlay with only the two pending public type declarations: `BedrockCompat.SupportsReasoningReplay *bool` and `ModelCost.ImageOutput *float64`.

The generator now recognises the v1.7.0 schema-v6 structure hash and exact manifest digest. It verifies the SHA-256 of all 43 provider payloads and recomputes the structure hash before writing output. The published npm archive matched the release SHA-256 and SHA-512 integrity; the extracted inputs matched the verified stage. No generated records were edited by hand.

`make check-model-regeneration` reproduced all three Go files byte for byte and passed the public comparator with the type-only overlay. `make check-model-regeneration-self-test`, the generator tests, and `go vet` for both commands also passed.

## Controls

- The manifest fixture reaches the provider digest check unchanged. A one-millisecond change to its timestamp is rejected with `invalid v1.7.0 catalog manifest digest`.
- The generator tests distinguish absent, `false`, and `true` Bedrock replay metadata, and absent, zero, and decimal image-output pricing.
- The public full-record comparison rejects changing `supportsReasoningReplay` from `false` to `true` on `amazon-bedrock/global.moonshotai.kimi-k3`, or changing `imageOutput` from `9.820359` to `9.82036` on `openrouter/black-forest-labs/flux-3-image`. The generator independently rejects both modified payloads at the provider digest check.
- The regeneration comparator's negative self-test changes those two fields in scratch Go source and changes a classifier's display name, without changing any identity.

## Integrated gate

Apply the live public type definitions, then the generator/workflow commit and the separate generated-files commit. Verify the real types against the full published catalog again. With a verified complete npm `dist` tree, the two existing gates can run offline:

```sh
export PI_AI_MODELS_GENERATED_JS="$VERIFIED_DIST/models.generated.js"
CGO_ENABLED=0 GOPROXY=off GOSUMDB=off make check-model-regeneration
CGO_ENABLED=0 GOPROXY=off GOSUMDB=off make check-model-regeneration-self-test
```

## Native tests that still assume older rows

The two published snapshots differ by 38 retired chat identities. The candidate adds none of them back. A focused 24-test run had 22 passing tests against the previous Go files and 12 against the verified candidates.

| Native test | Stale assumption |
| --- | --- |
| `TestQwenTokenPlanModelsExposeTextAndOmitImageModels`, `TestSourceV160QwenTokenPlanReasoning` | The two individual-plan catalogs still expect `qwen4-max-preview`; v1.7.0 lists `qwen4-max`. |
| `TestV0821ClaudeOpus5CatalogMetadata` | Bedrock no longer lists `jp.anthropic.claude-opus-5`. |
| `TestTogetherModelsReasoningControlsFromTogetherAPISurface` | Together no longer lists `openai/gpt-oss-120b` or `deepseek-ai/DeepSeek-V4-Pro-0813`; the first lookup now fails. Before regeneration this test already failed later on `MiniMaxAI/MiniMax-M3`. |

Six newly failing tests still assert 1,696 chat records: `TestRegisterBuiltinModels`, `TestV0844CatalogCountsAndProviderAPIs`, `TestV0850CatalogCountsAndGrokRemoval`, `TestV100UnifiedTypedLookupAndImageCompatibility`, `TestV100TypedRegistryCountsAndRepresentativeMetadata`, and `TestV101CatalogTypedCountsAndNewClassifierPrices`. The last also contains an older `deepseek-ai/DeepSeek-V4-Pro-0813` lookup after its count assertion. `TestV101SingleFieldCompatibilityMetadataRetained` fails on the OpenCode affinity count on both trees. These historical native tests were left unchanged.

The catalog verification does not exercise Bedrock replay behavior, image usage/cost accounting, Pi Image runtime registration, or native pointer ownership.

## Production type verification

The integrated gate on `ce632ba13b7b17de891601e555e65dc7adf63103` reproduced the generated files and verified all 1,858 public records with the production types, without an overlay. The regeneration self-test and the focused catalog and retired-model checks passed. The stale expectations listed above describe the candidate audit before their separate test reconciliations; they are not remaining failures. Ant Ling's retired `Ring-2.6-1T` and `Ling-2.6-flash` fixture branch was subsequently removed, and the v1.7 `Ling-3.1-flash` compatibility is covered in [the Ant Ling receipt](openai-completions-ant-ling.md). Historical wire fixtures do not register any retired rows.
