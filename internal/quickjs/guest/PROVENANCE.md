# QuickJS guest inputs

- Engine source: `rcarmo/gi`, local revision `9b48b91f29b808a979d2267cb067ac7b5f464ff9`, `internal/codemode/engine.go`. MIT notice: `../LICENSE.gi`. Adapted package/asset paths and deterministic clock/random inputs.
- WASM: copied from that revision's `internal/codemode/vendor/quickjs.wasm`, recorded there as QuickJS-WASI 3.6.2. MIT notice: `LICENSE.quickjs-wasi`. SHA256: `d4c9375f2b1ca4dc95f72c8aa2982a7a9951ac8011490d79c6582df732b4bbd9`.
- Prelude: `PRELUDE_SOURCE` extracted with Bun from official upstream `06a466935536f8c9bcacb2d5efc0fc021be1bdff`, `packages/codemode/src/runtime/prelude-source.ts` (1.2.0). MIT notice: `LICENSE.pi-codemode`. SHA256: `57c8dc8c0e812117d82167fa751bc368f249d5aaee56d321066fc5280c785281`.

On 2026-10-14, the published `quickjs-wasi@3.6.2` npm archive passed its SHA512 integrity check: `sha512-FCqGtGOrMgzUiIrMNMA2YnsOxCNwo31dzqXvclXUC6xeT35NJLKXQJsvbeCTjvoFAwZgEAPg8U6+KAPDGXn8Mg==`. Its `quickjs.wasm` SHA256 matches the embedded asset above. Registry repository: `https://github.com/vercel-labs/quickjs-wasi`. Package signatures and an independent rebuild have not been verified. The downloaded archive, metadata and extracted probe files were deleted after analysis.

These hashes identify embedded assets; generated JS/image output acceptance does not use hashes. Guest assets are intentional installed source inputs, not disposable build output.

On 2026-10-15, verified pi-codemode1.3.0 archive (official71023aec1546d6d677a65669c5665c0927ce0dd0) PRELUDE_SOURCE is byte-identical to the embedded1.2.0 source above. quickjs-wasi dependency remains3.6.2. Asset hashes/fingerprint therefore remain unchanged; SBOM prelude provenance now names1.3.0. This asset-integrity comparison does not gate generated-signal equivalence.
