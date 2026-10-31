## Google SDK framing

Google and Vertex now use a private framer matching the tested delimiter, prefix and EOF rules of locked `@google/genai` 2.21.0. Shared SSE and Mistral retain their existing behaviour.

Reference target: `81f3cd412758ebdd92324c7d63b669a189408d0a`. Its `package-lock.json` supplies the archive URL, version and SHA-512 integrity. `scripts/probe-google-sdk-framing.ts` checks that integrity, extracts the inspected `ApiClient.processStreamResponse`, `HttpResponse` and async-generator helpers, and executes only those fragments. It does not import the package, install dependencies or execute install/auth code.

The [generated fixture](../../inference/provider/google/testdata/sdk_framing_main.json) records 18 wires, decoded chunks, failure classes and the last finish reason. Every SDK input ran with whole-buffer and one-byte UTF-8 reads. Ten inputs reject; eight reach EOF without a framing/JSON exception. One accepted input ignores an event-prefaced STOP, leaving the adapter without a finish reason.

| Cases | Locked SDK result |
| --- | --- |
| Complete LF, CR and CRLF frames | Parse both chunks |
| Four unfinished STOP tails: no ending, LF, CR or CRLF | Reject before yielding the tail |
| Whitespace after a complete STOP | Accept |
| Unfinished comment after STOP | Reject at EOF |
| Complete comment after STOP | Ignore comment |
| `[DONE]` before or after STOP | JSON parse failure |
| LF followed by CRLF as boundary | Incomplete segment |
| `event:` line preceding `data:` | Ignore entire event |
| Repeated `data:` prefixes inside JSON | JSON parse failure |
| Raw newlines inside JSON after the first `data:` | Parse JSON |
| NEL tail / BOM tail | Reject NEL; trim BOM |

Before production changes, `TestMainSourceGoogleSDKFraming` had 24 failures and 12 passes against acfa8cc. The 36 registered HTTP cases now pass, checking decoded observer values/counts, retained Unicode text/response ID, terminal classification and the SDK's exact incomplete-segment diagnostic. Syntax-error wording uses Go's parser. A separate 36-case reader-partition test checks the native framer against the same reference.

`parseGoogleFrames` keeps the existing 8 KiB initial scanner buffer, 4 MiB physical-line limit, cancellation-aware bounded channel and caller-owned reader. Three guard cases cover a bounded line, an over-limit line and an event exceeding 4 MiB made of bounded lines. The limit has not become a whole-event cap. Early-return body ownership and cancellation tests still pass.

The former native Google audit expected standard SSE joining of repeated `data:` prefixes. Its positive output assertions are unchanged, using the SDK-supported raw-multiline spelling. The exact former wire is retained in two HTTP rejection cases. Mistral/shared SSE still join ordinary multiline fields and flush EOF data.

## Reproduce the reference

Source `scripts/project-env.sh`. Download the archive URL from the pinned lockfile beneath `$PROJECT_TMP_ROOT/runs/google-framing/current/`, then run:

```sh
bun scripts/probe-google-sdk-framing.ts "$PROJECT_TMP_ROOT/runs/google-framing/current/genai.tgz" --write
```

The generator writes the intentional test fixture. Remove the archive and extracted/probe scratch immediately after analysis. Both raw SDK files and disposable run logs from this comparison were removed.

## Development checks and further review

Go 1.27.3, `GOMAXPROCS=4`: full root normal tests; Google, Mistral and shared SSE normal/race suites; root vet; Google-scoped staticcheck v0.8.2; and `git diff --check` pass. No dependency or catalogue changes; v1.4.0 is unchanged. Acceptance and CPU/allocation profiling are incomplete; the 23 root staticcheck findings outside Google still need review.

These fixtures exercise SSE framing and JSON syntax before SDK object transformations. Callback enrichment/shape, observer mutation/identity, detached snapshots, valid-but-unsupported DTOs and SDK read-chunk API-error recognition need further work. Malformed UTF-8/TextDecoder equivalence and empty-data events need explicit reference coverage. Credentials, other preflight/deadline paths and timestamp/duration contracts also need review.
