# 1.2.0 acceptance checkpoint

Official target: `06a466935536f8c9bcacb2d5efc0fc021be1bdff`; previous target: `abe508e1b89912adde45528136c3221eb69acdd7`. Native working base: `9f4a8848d4c243d06152d37ffb6ffc878df5349f`, with uncommitted implementation/tests. No commit, push, tag, signing or publication authority.

## Toolchain and dependency refresh

Go1.27.2 and Staticcheck0.8.2 are now the validation/CI pins. Module minimum remains Go1.26.0. Latest stable application dependency graph includes AWS SDK/Bedrock/Smithy, websocket, compress, x/image and x/text updates; embedded QuickJS/codemode assets stay pinned. Full-root race passes after updates: durable763.904s/tools33.450s/QuickJS108.519s/consumers28.935s. Normal tests, vet/staticcheck/logging, non-CGO build, module verification and enforced security scan pass. Post-upgrade dirty-snapshot SBOM validates24components and matches a second generation. Root archive matches a forced rebuild; see the reproducibility receipt below. CPU/alloc_space/alloc_objects refreshed and analysed; raw artifacts disposed.

## Verified locally

- AI provider/schema/catalogue contracts and regeneration: [AI crosswalk](ai-crosswalk.md).
- Durable nested tools, structured output, storage failures/request errors, ownership, discovery/models and image delivery/replay: [durable crosswalk](durable-crosswalk.md).
- Codemode family dispositions cover all149 inventoried declarations across eight files, plus seven parameterised malformed-worker cases. Detailed declaration/deterministic cases, durable cases and sandbox/source/discovery/model families have separate mappings. This is native contract coverage, not execution of all upstream TypeScript assertions.
- Official published VM integration exercised worker batching, result ordering, properties, clocks and cancellation. Native QuickJS interpreter/compiler/process tests cover portable execution.
- Production-current full-root race passed: durable755.221s/tools33.156s/QuickJS110.391s/consumers28.760s, Go1.27.1, GOMAXPROCS2, seed1792082342786540542. Full normal/static/logging/non-CGO checks passed. Later additions are tests/documentation only and passed targeted race/vet.
- CPU, alloc_space and alloc_objects analysed, with temporary captures/binaries removed. No unmeasured performance improvement claimed.
- Dirty-snapshot SBOM24components validates and repeats; forced root archive rebuild matches. Full distribution-archive reproducibility remains a release check.
- Both official npm archives have verified integrity, registry signatures, publish attestations and SLSA provenance, constrained to the pinned workflow/tag/source commit. Tampered DSSE payloads reject. [Provenance](provenance.json).

## Post-upgrade reproducibility receipt

Go1.27.2, CGO_ENABLED=0, root archive `-trimpath -buildmode=archive` versus forced `-a` rebuild: identical, SHA256 `a318cf883b7431838442c821b9df0bad2875be4dc41cff703fdc9ca2fb84e857`. Two independent candidate SBOM generations validate and match, SHA256 `9ec717c5d45b3a364d6552566309670d18d362fa35a8f6cac61a53938a299e13`,24components. Version includes dirty source SHA256 `7ba74728d8ed7b8d38222ddfa4d328d83e01f22a357d9354ea76b0aee5808a2f`,1167files, before this documentation update; base VCS revision remains `9f4a8848d4c243d06152d37ffb6ffc878df5349f`. Source digest uses the path/mode/length/bytes inventory described in the durable crosswalk. These checks cover the root archive/SBOM, not hosted distribution archives. Temporary binaries/SBOMs/logs removed after use; intentional release assets untouched.

## Retained native policies

Resource ceilings, schema subset/expansion budgets, host callback authority, image pixel/input ceilings and observer quotas were not weakened. Go image codecs need not produce Photon-identical bytes. Ordinary WASI timezone is UTC. Native call IDs, drains and Wait/Close replace parts of the JS event/promise API. Cosmetic status wording, wall-time text and stack typography do not warrant further implementation work.

The default large-root reconnect baseline can exceed native observer quota. Public reconnect now also passes a real memory/journal test with persisted `MaxPage=8`, reserving4MiB per observer from the unchanged32MiB total budget instead of the default512KiB. `WatchEvents` and its public snapshot retain the large image after reopen, followed by successful provider replay without re-execution. Race count3 for default/error and configured/reconnect cases passed18.860s. This demonstrates supported bounded configuration, not unbounded admission or a change to defaults.

## Unresolved acceptance

1. **Security:**resolved locally by approved Go1.27.2 validation upgrade and dependency refresh. Enforced govulncheck passes with no findings; no exceptions or scanner-level changes. Historical Go1.27.1 analysis is in [security disposition](security-disposition.md).
2. **External platforms:**live paid-provider/auth and deployed Cloudflare/production authenticated worker transport acceptance are unrun. No live-provider acceptance harness is configured; these require target/credentials and paid/deployment scope. The official published VM integration was refreshed after dependency upgrades using Node22.23.1/pi-codemode1.2.0/quickjs-wasi3.6.2: all3 top-level integration tests execute (no skips), race count3 passes3.327s. This verifies local official VM interoperability, not those deployments.
3. **Policy acceptance:**native ordinary UTC, bounded schema/resource/observer behaviour and image codec restrictions are explicit. No blanket identical-semantics claim covers inputs outside native admission limits.
4. **Release operations:**hosted dispatch, source freeze/commit, final distribution SBOM/archive checks, signing and publication require separate authority. Existing refs/assets/stash remain preserved.

The goal is not complete. Further work should address these substantive points rather than repeatedly rerunning unchanged local tests or polishing diagnostic strings.
