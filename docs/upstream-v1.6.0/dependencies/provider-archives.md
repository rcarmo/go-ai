# Google GenAI and OpenAI npm archive receipt

Both previously missing archives now match the SHA-512 integrity in the official pi v1.6.0 npm lock at `faac585884ae2c013a6b1400f3e9e77f10b34bac`. The earlier cache searches remain historical observations.

| Pinned placement | Compressed bytes | SHA-256 |
| --- | ---: | --- |
| `node_modules/@google/genai` `2.32.0` | 1,836,214 | `93ba5d4d2ec28df2b00dc925637cb4d86a118eccfd9d3dca470f2908c58ea052` |
| `node_modules/openai` `7.59.0` | 3,733,234 | `7f1deacae8d17efc9c02622b032851c7e28e148b62b1da7747eb8b426ea4dba9` |

**Acquisition.** Unauthenticated direct HTTPS GETs to the two exact `registry.npmjs.org` URLs in [the receipt](provider-archives.jsonl) returned HTTP 200 at those URLs. The requests disabled the curl config, proxy and redirects; no credentials, package installs, or npm cache changes were used. Downloads went to isolated scratch. An OpenSSL check matched each compressed SHA-512 integrity before retention in `/tmp/go-ai/runs/durable-port/npm-verified-v1.6.0/`. Only the receipts and verifier are tracked in Git.

## Verify the retained files offline

```sh
ledger=docs/upstream-v1.6.0/dependencies
bun run "$ledger/verify-provider-archives.ts" "$pinned_git_mirror" "$retained_archive_dir" \
  > /tmp/pi-v160-provider-archives.jsonl
cmp "$ledger/provider-archives.jsonl" /tmp/pi-v160-provider-archives.jsonl
```

This closes the two archive-byte gaps only. It does not establish the load path or bytes used in a past Oracle run, dependency-tree installation, package signatures, provider behavior, or native correspondence. No credentialed calls or test suites were run.
