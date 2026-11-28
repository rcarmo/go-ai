# Azure unchanged originals

The five selected v1.7.0 originals are byte-identical to the checked-in Git blobs at `fb0fe823539f6d04850f6e34748c01f01f212e1f`. Vitest 4.1.12 on Node 26.13.0 ran all 37 cases unchanged: five files passed, 37 tests passed, zero failed and zero skipped. Execution used only mocks, in-memory payload capture and loopback transport; no Azure or OpenAI credential or service was used.

| Original | Git blob | SHA-256 | Cases / assertions |
| --- | --- | --- | --- |
| `azure-models.test.ts` | `43566161bac869c6835d09a3a640b8bd9c7f6bde` | `92a5fb5e6f8ef15871259e89579c25a0c0b42044639df85e3693b96c3be9290b` | 1 / 4 |
| `azure-openai-base-url.test.ts` | `c1b5ffeda95dc99177a132978a865ce305c2caa8` | `96aaa0281fb4407d580ee76943930ec7cdcefbd29692c7ab1e0eda8c85b1d349` | 17 / 26 |
| `azure-openai-completions.test.ts` | `5c7869c101f3e58b33d165445c5621b5f70275a2` | `f673cc829c0d7458892eac609434222ffd7aeb3924a05f2654bdfe0a4addf4fe` | 15 / 23 |
| `azure-openai-responses-reasoning-replay.test.ts` | `20226906214dd6e9bf3fa68871cf2969b7e53a10` | `a705f15fb252c6338935c34464b3bc0d71c40e1c1b688a39ff3e7fd0c5177fbe` | 2 / 2 |
| `azure-openai-tool-choice.test.ts` | `1c8f781143999d72db6828f24baa6206d323f04c` | `78de412209eeb5e9f5834cf3059dd886c30efbd3c6fea05b9e996cfa0f7f9a32` | 2 / 4 |

## Native comparison

The exact native base was `cbca8ba81df8a13ee1c88521a078203540a637b1`; the tested runtime commit is `20da487c439bbbd28fac6e8b7e691a9141f17371`. Before the production fix, the new Responses wire test failed because Go requested `/openai/v1/deployments/prod-gpt-5/responses`; the pinned v1 API contract places `prod-gpt-5` in the JSON `model` field and requests `/openai/v1/responses?api-version=v1`. The fix is limited to `ResolveAzureResponsesConfig`; it uses the normalized Azure base URL and deployment as separate values. Tests observe HTTP directly through Go `RoundTripper` and local servers, not Azure or OpenAI SDK behavior.

- Catalog: [azure model tier test](../../azure_models_upstream_v170_test.go#L11) verifies provider, API, Azure context window and the complete OpenAI long-context cost tier.
- API and base URL: [normalization and header cases](../../inference/provider/openairesponses/azure_openai_base_url_test.go#L13), [v1 wire route](../../inference/provider/openairesponses/responses_azure_test.go#L34), and [deployment callback](../../inference/provider/openai/azure_104_test.go#L14) cover native request paths, API dispatch inputs, base URL normalization, invalid configuration, prompt cache key, storage, strict mode, raw event order and user-agent override.
- Completions: [production wire tests](../../inference/provider/openai/azure_104_test.go#L49) cover absent endpoints, mapped deployments, model identity, DeepSeek reasoning effort, omitted cache parameters and thinking fields, system-role preservation, reasoning replay and response attribution.
- Responses replay: [encrypted-content tests](../../inference/provider/openairesponses/azure_reasoning_replay_upstream_test.go#L11) verify both retention and completed-response backfill through parsing and request-item reconstruction.
- Tool choice: [required and none production requests](../../inference/provider/openairesponses/v0843_azure_tool_choice_test.go#L11) preserve both provider-specific and simple options while retaining tool definitions.

All five originals are now represented by 37 scoped cases and 59 mapped assertion sites. [Machine-readable receipt](azure-originals.json) records the source fingerprints, red/green wire result and live-credential gate.

## Credential and security gate

No workspace credential grant exists, and no `AZURE_OPENAI_*` secret was read or written. Live Azure model calls, deployment discovery and service-specific authentication are therefore **not executed**. The local selectors prove deterministic catalog and wire construction only. Test data uses synthetic `test-key` values; none is persisted outside test fixtures.
