# Go catalog regeneration readiness

The stock Go generator rejects v1.7.0's verified schema-v6 manifest before reading any model records. Its exact structure hash, `3ee288633d9a75c317bc3d661808279975a41591576b865c2000e09567e422ab`, is outside the checked-in manifest allowlist. The default regeneration check still pins v1.6.1.

The [audit receipt](catalog-readiness.json) uses a scratch-only Go overlay to admit the exact v1.7.0 structure hash and manifest digest. All later catalog validation and field projection stay unchanged. A second overlay loads the generated candidates through the public Go registries without replacing the checked-in files. The stock full-record comparator rejects the candidate. The stock legacy `-input` path emits the same candidate bytes for all three kinds and loses the same fields.

| Kind | Published records | Matching Go candidate records | Different records |
| --- | ---: | ---: | ---: |
| Chat | 1,701 | 1,695 | 6 |
| Image | 74 | 4 | 70 |
| Classifier | 83 | 83 | 0 |

Every candidate identity matches a published record. The receipt lists the complete field differences:

- Six Amazon Bedrock Moonshot Kimi K3/K3.1 records carry `/compat/supportsReasoningReplay: false`. `compatEntry`, the Bedrock generator arm and `BedrockCompat` need to preserve explicit `false` separately from absence. The thinking level maps and other fields already survive regeneration.
- Seventy OpenRouter image records carry `/cost/imageOutput`. The generator's cost reader and writer and the public `ModelCost` need to preserve this optional price. Use of that price when accounting for generated images is a separate runtime check.

All 74 published image records use `openrouter-images`. The published records contain no `pi-image` entry. Pi Image runtime registration and Radius's dynamic image configuration remain separate from this static catalog comparison.

## Reproduce offline

Use a checkout with the Go sources at the audited base and the verified npm archive and complete extracted `dist` tree. The audit rechecks the archive and the selected `dist` members, then compares the published JavaScript exports with all 43 provider JSON payloads before running the Go generators. It neither installs dependencies nor contacts a provider.

```sh
source scripts/project-env.sh
bun run docs/upstream-v1.7.0/catalog-readiness.ts "$PI_AI_ARCHIVE" "$VERIFIED_DIST" \
  | cmp - docs/upstream-v1.7.0/catalog-readiness.json
```

This covers the root public Go model registries. It leaves the catalog pin, production generated files, provider runtime, and separate image adapter unchanged.
