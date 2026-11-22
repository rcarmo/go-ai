# Google GenAI loaded input: one pinned Oracle case

In a new focused run, Node's module load hook returned `node_modules/@google/genai/dist/node/index.mjs` as ESM. Its 1,198,102 bytes matched `package/dist/node/index.mjs` in the [verified `@google/genai` 2.32.0 archive](provider-archives.md), with SHA-256 `e07e9c3fc85b5a41586b013ffb7d9cd46ece70e5131090b43b04801a73b91665`. The [machine-readable receipt](google-sdk-load.json) records the exact pin, package integrity, loader path and selector.

The run selected one unchanged assertion case from `packages/ai/test/google-stop-reason-continuation.test.ts`: **ends the turn as length instead of letting the SDK send a continuation request**. It passed on Node `v26.13.0` and Vitest `4.1.12`. The case uses a dummy key and stub fetch for Google Generative AI and Vertex; it checks two total fetches and both `length` results. The other case in the file was outside this selection, not an approved exclusion.

The hook hashes the actual `source` returned by Node's loader, rather than hashing a path and assuming the module loaded. Appending one LF to the scratch copy still let the same unchanged test pass, but the observed load digest differed and the provenance check rejected it. This control separates behavioral success from input identity.

## Reproduce offline

```sh
ledger=docs/upstream-v1.6.0/dependencies
bash "$ledger/probe-google-sdk-load.sh" "$pinned_git_mirror" "$oracle" "$retained_archive_dir" \
  > /tmp/pi-v160-google-sdk-load.json
cmp "$ledger/google-sdk-load.json" /tmp/pi-v160-google-sdk-load.json
```

The probe uses the retained Oracle installation and the already verified provider archive. It checks the lock, the archive's compressed integrity, and exact pinned test, configuration and provider source bytes. It copies the relevant workspace and dependency tree to disposable scratch and serializes both focused runs with the shared lock. The test receives a scrubbed environment and Node's permission model without network permission. No installs, shared-cache changes, or real provider requests are needed. The wrong-digest control is expected to print one rejection on stderr.

This proves one loader input for this run only. It does not reconstruct earlier loads, enumerate transitive modules, establish the OpenAI load path, or extend native parity or release acceptance.
