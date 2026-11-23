# What still blocks a parity claim

Target: pi v1.6.0 at `faac585884ae2c013a6b1400f3e9e77f10b34bac`. The [verification log](README.md) records each test run and native comparison. The [primary queue](test-inventory/primary-queue-r21.md) is a historical audit; later results are in the log. Neither the queue's file count nor the assertion ledger measures full parity.

## Checks that need access or approval

| Check | What is needed |
| --- | --- |
| Live OpenAI and Codex replay | `packages/ai/test/openai-reasoning-replay-e2e.test.ts` uses `gpt-6.2-sol` with an OpenAI API key and a Codex token. It covers OpenAI, Codex SSE and Codex WebSocket. Run the actual requests only with explicit approval and provisioned access. Offline replay tests do not close this check. |
| Real PowerShell | `packages/durable/test/tools.test.ts:617` requires a Windows host. The mocked adapter tests can run offline; a real process check needs a Windows runner. Any hosted runner needs separate approval. |
| Cloudflare worker | The [pinned example passed on local workerd](dependencies/cloudflare-codemode-example.md) with verified inputs. It was a supplemental example run. A hosted check, if required by the target matrix, still needs separate approval. |
| Full local acceptance | Once the open behavior and test cases are resolved, obtain separate approval to run the full checks on the exact candidate. Measure CPU, allocated bytes and allocation counts, fix measured outliers and remove raw profiles. No race-detector coverage is claimed. Hosted CI, SBOM publication, tags and release still need their own authorisations. |

## Behavior that needs an exact match or a scope decision

- **Read-only storage:** the unchanged torn-JSONL test passed upstream. Go uses SQLite only and has separate tests for sharing, refusal and recovery. Do not report the JSONL case as a native pass or add a second backend silently. If SQLite behavior is to stand in for the JSONL-specific assertions, record that scope decision explicitly.
- **Failed-run withdrawal:** upstream withdraws the queue in one commit. Go does the same within its write limit, but a lower configured limit can reject the whole commit. Any fix must preserve atomic withdrawal and enforcement of the limit; any retained difference needs explicit acceptance.
- **JavaScript event and value identity:** the upstream process observes `unhandledRejection`, and some tests compare object or SDK identity. Go checks its own cancellation, callback and result behavior. These checks do not establish JavaScript event or object identity; classify the required equivalent before closing those rows.

Continue the offline case mapping and narrower runtime checks without waiting for these approvals. Do not silently exclude a case or use one run to stand for a different host or interface.
