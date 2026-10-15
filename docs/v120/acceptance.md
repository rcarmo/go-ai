# 1.2.0 release acceptance

Native pi-ai/durable1.2.0 port accepted for publication after practical upstream alignment and mechanical Go layout changes. Official upstream target `06a466935536f8c9bcacb2d5efc0fc021be1bdff`. [Crosswalks](durable-crosswalk.md) and [chronological implementation receipts](implementation-status.md) retain detailed evidence; older incomplete statements describe earlier checkpoints.

## Accepted behaviour and organisation

- Provider/schema/catalogues1554chat/61image/34classifier; exact pinned regeneration.
- Durable nested-tool/replay/structured errors, chunked images/large returned strings, provider/hook/snapshot/reopen recovery.
- Real6MiB VM return spills intact and replays; upstream4.5MiB image read accepted; ordinary host timezone/DST, deterministic UTC.
- Default public large-image reconnect reserves actual baseline demand from unchanged total observer memory budget; exhaustion/release checks pass.
- Extra4096-call default throttle removed:100000 call admission with explicit lower host budgets retained. Native schema/setup/storage bounds remain sensible admission constraints, not claims of unlimited upstream JS objects.
- Generator moved to `cmd/generate-models`; public library packages remain standard importable Go packages.

## Final verification

Go1.27.2/Staticcheck0.8.2/GOMAXPROCS2, seed1792082342786540542: full-root race durable786.135s/tools33.731s/QuickJS118.023s/consumers31.353s. Normal durable116.648s/tools4.851s/QuickJS17.007s/consumers6.263s. Vet/staticcheck/logging/non-CGO/security/module verification/local supply-chain self-tests pass. No security exceptions. Final host semantics identity65; official worker fingerprint independently pinned.

CPU, alloc_space and alloc_objects analysed for6MiB/reservation/timezone/default-call workload. Durable3.27s CPU samples/4423.44MB/1085664objects; transaction validation dominates large-state allocations. QuickJS830ms CPU samples/118.54MB/1054637objects; VM calls/setup dominate. Prior equivalent-workload integrity optimisation reduced allocation space~5.9%; no further timing improvement claimed. Captures/binaries/temporary logs disposed.

Published npm signatures and publish/SLSA attestations verified with TUF/Sigstore trust, exact workflow/tag/source identity and negative tamper checks. Official Node VM integration passes. Live paid-provider/auth/Cloudflare deployment tests were not required for this native release and remain unrun; no deployment equivalence claim.

## Publication

User authorised commit/push/release. Native tag must be annotated by Rui Carmo and point to the accepted source commit. Publish SBOM/checksum through the native release workflow and verify public assets. Earlier archive/SBOM reproducibility checks apply to their recorded revisions; final release assets require commit-accurate regeneration. No previous release asset is overwritten.
