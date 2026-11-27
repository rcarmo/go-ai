# OpenRouter image settings, errors and usage

All eight unchanged OpenRouter Images originals and all 46 unchanged models-runtime originals passed offline against the v1.7.0 Git bytes in the [locked AI stage](ai-originals.md), without skips. The native ledger maps the five changed OpenRouter image cases and the image-pricing case from the models-runtime suite. This does not claim all 46 runtime cases.

## Native comparison

On unchanged `8b034be83fa3a215b3162bb5de668c5300f32347`, four focused cases failed: no `image_config`, no failed-finish classification, no missing-choice or choice-error classification, and the authoritative usage total was lost. The additional native pricing comparison did not initially compile because `ModelCost.ImageOutput` was absent. With only the declaration added, the exact token-split and pricing assertions failed.

The adapter now passes the optional `aspect_ratio` and `image_size` under `image_config`. It resolves first-choice errors (falling back to a top-level error) or unsuccessful finish reasons to empty output, retaining response ID and usage. The OpenRouter billed-total rules preserve a reported zero. `Usage.Reasoning` and optional `Usage.ImageOutput` are subsets of output, not extra tokens. Present image tokens use the optional image-output price; the remainder uses the ordinary output price. A missing split assigns all output to the image rate, and a reported split cannot exceed output. An absent rate leaves the old calculation unchanged.

The native checks use the public image API with intercepted HTTP requests and explicit model fixtures, and call the public pricing function directly. The supplemental ownership check verifies copies do not share the optional price. The generated catalog has its own full-record gate.

## Verification

The focused native image, pricing and cost selection passed twice without skips. Four compiled controls rejected wrong `image_config`, dropping an authoritative zero, dropping `native_finish_reason`, and ignoring the image-output rate. Scoped vet and Staticcheck v0.8.2 passed. The Go test names describe the behavior; this receipt and the ledgers record the source version.
