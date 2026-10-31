## Vertex key trimming and placeholders

Vertex keys now use the pinned helper's whitespace and placeholder rules. An explicit placeholder still selects ADC-style routing and masks an environment key; credential precedence has not changed.

Reference: `81f3cd412758ebdd92324c7d63b669a189408d0a`, `packages/ai/src/api/google-vertex.ts`, `resolveApiKey` and `isPlaceholderApiKey`. A Bun probe extracted and transpiled those actual functions, bound the source's credentials-marker constant and executed twelve inputs. This helper reads `options.apiKey` only. It does not implement an explicit-marker-to-environment fallback. No SDK package, auth implementation, credential file or live endpoint ran.

`vertex_marker_main_test.go` adds twelve URL classifications and nine registered HTTP cases. Fifteen fail on unchanged 7346429; six controls pass. All twenty-one pass after production changes:

| Inputs/contracts | Result |
| --- | --- |
| `<`, `<token`, `<>`, `<a>b>` | Ordinary literal keys, not placeholders |
| `<authenticated>`, `gcp-vertex-credentials`, `<a\nb>` | ADC-style marker; no key query |
| Whitespace around markers/real keys | Trim before classification and transport |
| BOM / NEL | Trim BOM, retain NEL, matching ECMAScript trim |
| Whitespace-only helper input | Empty helper key; ADC-style URL |
| Option, model and environment real keys | Normalised selected value; original caller inputs unchanged |
| Explicit placeholder plus a real environment key | Keep placeholder mode; never substitute the environment key |
| Environment placeholder | Keep marker mode |
| Custom provider alias with a canonical Vertex environment key also present | Use the alias-scoped key, never the canonical provider's key |

The implementation normalises the selected Vertex key locally after `ResolveAPIKey`, repeats that normalisation at the direct URL-helper boundary and replaces the broad leading-`<` check with `^<[^>]+>$`. It reuses the existing ECMAScript whitespace predicate. Generic option → model → provider-scoped environment precedence and Gemini handling are unchanged.

HTTP marker cases provide synthetic caller Authorization explicitly. They verify routing, key non-disclosure and caller-header ownership; they do not acquire ADC tokens. The native model-key and alias-isolation cases are compatibility controls beyond the direct source helper.

## Remaining credential work

The provider still does not acquire ADC tokens, load key files, request scopes or refresh credentials. Empty/trimmed-empty keys still hit the native stream's missing-key preflight; helper-level ADC URL selection does not establish the source's default-ADC stream behaviour. This needs a credential-adapter implementation and isolated authentication tests before full convergence. Higher-level source credential discovery/registry wiring, full constructor/header coverage and SDK auth-header/endpoint composition also need review. The nine-case upstream file stays development-verified-partial.

## Development checks

Go 1.27.3, `GOMAXPROCS=4`: root normal, Google race, root vet, Google-scoped staticcheck v0.8.2 and `git diff --check` pass. The source probe ran in memory and produced no disposable files. No dependency, catalogue or release-ref changes. Full convergence, the 23 root staticcheck findings outside Google, acceptance and profiling are incomplete.
