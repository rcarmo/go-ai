# Locked AI tests run unchanged on v1.7.0

All 59 selected originals passed, with no skips: Pi Image (8), OpenAI Completions empty tools (12), Completions stored reasoning formats (16), Completions opaque reasoning replay (7), and Bedrock message conversion (16). The exact tests, runner config and lock matched pinned Git bytes. Vitest 4.1.12 ran on Node 26.13.0 with loopback as its only network interface. No provider keys or services were used.

The [four release assets](ai-release-assets.json) match GitHub's published SHA-256 and sizes. They are the source archive, coding-agent install manifests and checksum file. The checksum file separately lists the three downloaded payload digests. Platform binaries and the shipped installer dependency closure were not evaluated.

An isolated `npm ci --ignore-scripts --no-audit --no-fund --include-workspace-root=false` selected `packages/ai`, `packages/schema` and `packages/telemetry`. The first offline attempt missed `undici` 8.12.4 in the old cache; the completed install used the npm registry and the unchanged pin-matched lock. The [stage check](verify-ai-stage.ts) matches 178 installed external placements and the three workspace links to that lock. It does not measure the dependency closure for other workspaces or platforms.

The release source and verified npm distribution contain the same 43 provider data payloads. Each payload matches its catalog manifest SHA-256. Only `generatedAt` differs in the manifests. A comparison of [complete exported model records](catalog-delta.jsonl) found 43 chat additions, 38 removals and 66 changes; 70 image records changed without additions or removals, and two classifier records were added. The generated entry module itself is byte-identical to v1.6.1; it imports different model data. No Go generator pins or catalog records changed in this slice.

Recheck a private stage with `bun run docs/upstream-v1.7.0/verify-ai-stage.ts "$PI_AI_TEST_STAGE" "$PI_NPM_AI_DIST"`. The [execution receipt](ai-originals.json) names the five files. Original execution and distribution checks do not establish native parity.
