# Durable Node test inputs

The Durable workspace's full development install would also bring in its Cloudflare test harness, which the pinned Node config does not use. With limited free disk, the Node run reused the pinned Codemode Vitest install and staged the exact runtime packages separately. This does not exclude or run any Cloudflare tests.

1. Export the pinned root lockfile and all tracked workspace `package.json` files to an empty staging directory. Their relative paths must stay the same.
2. In that directory, run `npm ci --workspace packages/codemode --workspace packages/durable --include-workspace-root=false --omit=dev --ignore-scripts --no-audit --no-fund`. Do not amend the lock or manifests.
3. Run `bun run docs/upstream-v1.6.1/dependencies/stage-node-placements.ts "$STAGE" "$PI_ORACLE"`. It checks 107 installed placements against the pinned lock: 101 npm packages and six workspace links. It hard-links regular package files where absent and verifies the resulting package bytes. Use the same filesystem for both paths. It does not replace existing packages or package-manager metadata.
4. Provide `packages/ai/dist` from `package/dist` in the [verified pi-ai archive](package-archives.jsonl). The pinned AI runtime does not export a `source` entry for its main module. Without the published directory the unchanged suite cannot resolve that entry. This is a staged artifact alongside the untouched Git sources, not a rewrite of the tests or the input lockfile.
5. Byte-compare the selected test, config, setup and support files and the two changed Codemode runtime files with the pinned Git blobs. Run Vitest 4.1.12 with that config and an isolated network namespace. The original source and its test selectors do not change between the preflight and the passing run.

Five selected `packages/durable/test/codemode.test.ts` cases passed in this scope: host cutoff, decided-result readback, text spill at the token budget, path reuse after restart, and saved image bytes. This is not a full Durable test-workspace install.
