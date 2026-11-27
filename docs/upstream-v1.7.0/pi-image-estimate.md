# Pi Image catalog estimate provenance

Pi Image uses the pinned `parsePiUsage` and `calculateCost`. With token counts but no billed total, its catalogue estimate explicitly sets `totalFromProvider: false`; a billed total with no provenance flag remains unqualified.

Eight unchanged image originals passed first. A separate [source-derived case](pi-image-estimate-witness.test.ts) exercised the unchanged v1.7.0 image adapter and confirmed an estimate of `0.0053` with an explicit `false` flag. It is not an additional verified assertion site in the original source test. The existing native catalogue calculation test, extended to check the flag, failed on `15d9040d37157d03fc739d82b89d8a9ad25aa5be` because Go cleared it after calculating the same amount. Removing that overwrite leaves the existing cost calculation, provider amount, and token-count or split policy intact.

The image adapters, pricing and Durable usage selection passed twice without skips, with vet and Staticcheck v0.8.2. Restoring the deleted assignment is the exact unchanged-base red control.
