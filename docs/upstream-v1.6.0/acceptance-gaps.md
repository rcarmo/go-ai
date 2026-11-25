# What still blocks a parity claim

Target: pi v1.6.0 at `faac585884ae2c013a6b1400f3e9e77f10b34bac`. The [verification log](README.md) records each test run and native comparison. The [primary queue](test-inventory/primary-queue-r21.md) is a historical audit; later results are in the log. Neither the queue's file count nor the assertion ledger measures full parity.

## Checks that need access or approval

| Check | What is needed |
| --- | --- |
| Live OpenAI and Codex replay | Authorised by the user; waiting for provisioned access. `packages/ai/test/openai-reasoning-replay-e2e.test.ts` uses `gpt-6.2-sol` for OpenAI, Codex SSE and Codex WebSocket. The round-29 preflight found no workspace credential grants, no `OPENAI_API_KEY`, and no provisioned `openai-codex` entry in the user auth file. No live request was sent. Use a private test auth directory for Codex because the upstream helper may refresh and save credentials. |
| Cloudflare worker | The [pinned example passed on local workerd](dependencies/cloudflare-codemode-example.md) with verified inputs. It was a supplemental example run. A hosted check, if required by the target matrix, still needs separate approval. |
| Full local acceptance | Once the open behavior and test cases are resolved, obtain separate approval to run the full checks on the exact candidate. Measure CPU, allocated bytes and allocation counts, fix measured outliers and remove raw profiles. No race-detector coverage is claimed. Hosted CI, SBOM publication, tags and release still need their own authorisations. |

## Approved scope decisions

- **Real PowerShell:** the user excluded the Windows-only real process check in `packages/durable/test/tools.test.ts:617`. Keep the mocked PowerShell checks and their existing results. This exclusion does not cover other Windows behavior.
- **Read-only storage:** the user authorised a SQLite equivalent for the torn-JSONL assertions. The unchanged JSONL result remains an upstream result. The Go comparison must check the committed view and unchanged file bytes; the approval does not add a JSONL backend or establish native JSONL format compatibility.

## Other behavior gaps
- **Failed-run withdrawal:** upstream withdraws the queue in one commit. Go does the same within its write limit, but a lower configured limit can reject the whole commit. Any fix must preserve atomic withdrawal and enforcement of the limit; any retained difference needs explicit acceptance.
- **JavaScript event and value identity:** the upstream process observes `unhandledRejection`, and some tests compare object or SDK identity. Go checks its own cancellation, callback and result behavior. These checks do not establish JavaScript event or object identity; classify the required equivalent before closing those rows.
- **Codemode replay:** Go rejects unsupported saved-start shapes in storage before replay. The source version test rewrites its saved start and expects a tool-result refusal. Go still joins late host callbacks before returning the result. Its portable worker records each delivery and acknowledgement separately. The completed 50-result native case published 103 journal changes; the source requires fewer than ten. The production storage guard is unchanged. See [batching requirements](codemode-batching.md).
- **Codemode store patches:** raw sandbox placements can return an accepted series of writes whose combined patch exceeds one Go Durable record. The adapter rejects that reply before publishing its value or writes. A compatible solution must preserve bounded records, atomic store updates and replay safety.
- **Standalone Schema setup:** `CompileJSONSchema` now rejects reachable unresolved references during preparation by default, with a value-level reject option. The older checks and tool validation retain lazy resolution. The prepared API now supports JSON-value coercion and parsing; full meta-schema checks and broader JavaScript-specific behavior remain open.
- **Anthropic thread metadata:** the pinned SDK adds `?beta=true`; Go uses its existing beta headers and includes an additional interleaved-thinking beta. Some historical user text is sent as a block array rather than a string. The thread and tool-result comparisons keep these differences separate.

Continue the offline case mapping and narrower runtime checks without waiting for these approvals. Do not silently exclude a case or use one run to stand for a different host or interface.
