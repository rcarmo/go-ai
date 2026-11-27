# Radius image discovery on pi v1.7.0

All **ten unchanged Radius originals** passed on Node `v26.13.0` and Vitest `4.1.12`, with no failures or skips. The run used a private copy of the verified AI stage and a fresh network namespace. The source test, Vitest configuration and lockfile matched Git at `fb0fe823539f6d04850f6e34748c01f01f212e1f` before the native test edit. The test SHA-256 was `d9fcfe123af9c0d27d5e69e4c1508d66b773b45b3219552bbe91d87fb7a5a315`.

## Measured native boundary

The v1.7.0 Radius delta accepts `images` and `decision` alongside the text `models` list. Image entries must include `image` in their outputs. Unknown modalities are filtered, keeping the order of supported values. Radius exposes the surviving images by type using `pi-image` and the configured base URL, excludes them from the chat list, and restores them on a cache-only refresh.

On the unchanged Go base `cafdd14701059395ce918c1500a425489dcdc1be`, a real loopback `GET /v1/config` containing the original extra image row refreshed exactly the two expected `pi-messages` chat models. A fresh runtime using the same store kept those models without another request. **Neither public Go image registry contained `nano-banana` after this refresh and cache read.** The [executable red probe](probe-radius-image-discovery.sh) confirms the missing typed lookup after validating the text and request receipts; the failure is not an accepted production test.

The existing Pi Image adapter works once it receives an explicit typed model. The supplemental Radius test supplies the original Nano Banana metadata, checks the exact `POST /v1/image` request and synthetic bearer header, and gets the expected PNG, `stop` and `gen-img-radius-1`. The loopback server saw one config request and one image request. The source discovers that image model; the control supplies it by hand. It does not close discovery, cache, modality filtering or configured routing parity.

## Handoff

- `oauth.RadiusGatewayConfig` decodes only `baseUrl` and text `models`; `RefreshModelEntry` publishes only `[]*goai.Model`.
- `goai.ModelsStoreEntry` and `ModelRuntime` have no kind-separated persisted or refreshed image/decision snapshot. Their cache-only path reads the text list. The legacy root and `images` image registries do not consume Radius refresh results.
- Integrate typed image and decision entries into the shared runtime/cache first, including replacement, 304 retention and cache-only reuse. Then wire the Radius parser into that contract and use the exact typed discovered image in the existing adapter test. Keep separate mock cases for the valid fixture and supported-modality filtering. Do not register images from OAuth decoding as a side effect or expose image IDs through the chat list.

No production, authentication, catalogue or root image adapter changes are in this packet. Five focused Radius tests passed twice with shuffle seed 42 on Go `1.27.3`, CGO disabled. Vet and Staticcheck `0.8.2` passed for `./oauth`.

## Reproduce the selections

Take a private copy of the verified v1.7.0 AI stage and compare `packages/ai/test/radius-provider.test.ts`, `packages/ai/vitest.config.ts` and `package-lock.json` against pinned Git. Then run:

```sh
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3
(
  cd "$oracle/packages/ai"
  flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn \
    node ../../node_modules/vitest/vitest.mjs run test/radius-provider.test.ts --pool=forks --maxWorkers=1
)
flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn sh -ec 'ip link set lo up; exec "$@"' sh \
  go test ./oauth -v -run '^Test(RadiusRuntime|RadiusDynamicRefreshUsesETagAndReusesCacheOnNotModified|RadiusOAuthUsesGatewayEndpointsDirectly)' \
  -count=2 -shuffle=42 -p 1 -timeout 65s
bash docs/upstream-v1.7.0/probe-radius-image-discovery.sh
```
