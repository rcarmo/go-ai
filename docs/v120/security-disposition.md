# Go1.27.1 security disposition

Fresh `govulncheck@v1.7.0 -json ./...` on2026-10-15, source environment, Go1.27.1 linux/amd64/GOMAXPROCS2. No policy entries, toolchain files, dependencies or runtime behaviour changed. Advisory data was fetched from `https://vuln.go.dev/ID/<ID>.json`; raw advisories/scan output were removed after analysis.

## Remediation applied

Following approved continuation, validation and CI now use Go1.27.2. The enforced scanner passes with no findings after dependency refresh. No exceptions or scanner-level changes were made. Historical analysis below explains the prior1.27.1 blocker.

## Scanner levels

The enforced gate rejects13 advisory IDs. Previous notes called all13 reachable; the raw scan distinguishes9 symbol-level,2 package-level and2 module-only advisories. A symbol path is static reachability, not proof of an exploitable configured deployment. The policy currently rejects every reported ID regardless of level; that enforcement remains unchanged.

| Advisory | Highest observed level | Affected contract |
| --- | --- | --- |
| GO-2026-6599 | Module | `html/template` JavaScript escaping |
| GO-2026-6600 | Module | `html/template` JavaScript template-literal context |
| GO-2026-6603 | Symbol | HTTP/2 server Trailer memory-limit bypass |
| GO-2026-6604 | Package | Windows os.Root junction escape |
| GO-2026-6605 | Symbol | HTTP/1 CONNECT client desynchronisation |
| GO-2026-6607 | Symbol | TLS ECH outer-extension memory exhaustion |
| GO-2026-6608 | Symbol | MIME multipart remaining-memory bypass |
| GO-2026-6609 | Package | HTTP Range parsing CPU exhaustion |
| GO-2026-6610 | Symbol | HTTP/2 response-smuggling/framing headers |
| GO-2026-6611 | Symbol | HTTP/2 window-update CPU exhaustion |
| GO-2026-6612 | Symbol | HTTP/2 flow-control refund |
| GO-2026-6613 | Symbol | HTTP/1 CONNECT server desynchronisation |
| GO-2026-6617 | Symbol | HTTP/2 HPACK race/server crash |

All13 standard-library advisories list Go1.27.2 as the fixed release for the pinned branch (also1.26.9). Several HTTP/2 advisories list x/net0.60.0, but upgrading x/net does not replace Go1.27.1's bundled standard-library implementation.

## Application scope

Provider clients use net/http/TLS, including host-supplied clients and AWS Smithy transport. The OAuth callback server uses a loopback HTTP listener and accepts only GET in its callback handler. The HTTP/1 server CONNECT exploit requires successful CONNECT handling; that is not this handler's contract. The Windows os.Root advisory is outside the current linux runtime, but portable source/release scans still report its package. Module-only template advisories do not supply application symbol traces.

Scanner traces include conservative interface edges: for example an HTTP/2 Close path can be connected through an io.Closer implementation. Those paths should not be interpreted as direct proof that an application callback starts an HTTP/2 server. They also do not justify silently ignoring scanner findings.

Disabling all HTTP/2, replacing host clients, removing OAuth, suppressing modules or changing scanner level would change supported behaviour or weaken the requested security gate. Application input limits cannot repair all TLS/HTTP standard-library defects for arbitrary host-supplied clients. Vendoring a toolchain fork would be a toolchain change and would require its own maintenance, validation and approval.

## Required decision

The comprehensive remediation is approval to use at least Go1.27.2, followed by normal/static/non-CGO, full-root race, pre-release CPU/allocation analysis and a fresh enforced vulnerability scan. This is a proposed change only; Go1.27.1 remains pinned. No exceptions or gate-level changes were made. Reviewed deployment-scoped exceptions would require separate explicit approval, rationale, mitigation, owner and expiry; this report grants none.
