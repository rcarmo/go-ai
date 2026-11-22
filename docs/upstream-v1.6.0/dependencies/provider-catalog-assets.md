# Retained AI catalog assets

All **43 provider JSON files** match across the verified `@earendil-works/pi-ai` v1.6.0 archive, the retained Oracle `dist/providers/data` directory, and the restored Oracle `src/providers/data` directory. The [receipt](provider-catalog-assets.jsonl) records each file's paths, byte length and SHA-256. The Oracle pin is `faac585884ae2c013a6b1400f3e9e77f10b34bac`.

- The archive and both data directories contain exactly those 43 files plus `.manifest.json`. The verifier checks exact names, not just counts, and compares raw bytes without re-serializing JSON.
- The manifest matches the native regeneration fixture at the earlier regeneration commit and the evidence base. Its schema is v6 and its SHA-256 is `cc46553f3f008788e667600a873d3d83a4df8fe46c67b06c65b16fd828450e8b`. The verifier also checks all 43 archive members against their declared manifest digests.
- The retained `dist/models.generated.js` matches its published member separately. Neither provider data directory contains tracked files at the upstream Git pin; the restored source-path copies are published assets, not source Git blobs.

The earlier `d6986cd` model-regeneration receipt covers the three generated Go catalogs and exact comparison of 1,698 chat, 74 image and 81 classifier records. This packet links the retained inputs to the archive and that receipt. It does not rerun model generation or prove that any historical test loaded these files.

## Reproduce offline

```sh
ledger=docs/upstream-v1.6.0/dependencies
flock /tmp/go-ai/v160-focused-verify.lock bun run "$ledger/verify-provider-catalog-assets.ts" \
  "$pinned_git_mirror" "$oracle" "$release_archive_dir" > /tmp/pi-v160-provider-assets.jsonl
cmp "$ledger/provider-catalog-assets.jsonl" /tmp/pi-v160-provider-assets.jsonl
```

The verifier uses local Git objects and the previously verified archive. It streams archive members to stdout for comparison and does not write to the Oracle. In isolated controls, adding a legal JSON newline failed byte equality, and replacing an expected filename with an unlisted name failed the set check despite the same file count. No installs, network requests, Go tests, or exclusions are part of this receipt.
