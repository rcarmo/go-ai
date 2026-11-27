# Codemode worker admission and exit

All **231 unchanged originals** in the six v1.7.0 Codemode test files passed in a private verified source stage on Node 26.13.0 and Vitest 4.1.12, without skips. The stage rechecked the root/package manifests, lock, test configuration and all Codemode source and test files against pinned Git. It reused the runner placement and locked QuickJS archive previously verified for [portable batching](codemode-portable-batching.md). This run includes the Node and Bun process cases; it does not imply that those lifecycle mechanics were ported to native Go.

| Suite | Passed |
| --- | ---: |
| Declarations | 19 |
| Deterministic | 30 |
| Portable | 7 |
| Sandbox | 156 |
| Source | 5 |
| Worker lifetime | 14 |

The native comparison exercises two worker-lifetime cases through the public `QuickJSSandbox.Start` and normal terminal event. On unchanged `15d9040d37157d03fc739d82b89d8a9ad25aa5be`, capacity refusal tried precisely once and never called the start hook. A failing start hook received a real exit signal and released its permit once after that signal. The Go public result preserves the `sandbox:` error class in a string but uses a redacted message rather than the raw callback text. The two shape/message assertions remain partial; no raw diagnostic or limit change was made.

The selection passed twice with vet and Staticcheck v0.8.2. The compiled controls repeat the acquisition attempt or release the permit before the actual exit signal. Both failed exactly the corresponding counter or ordering assertion. Native construction-failure, JS worker ownership and process timing cases are outside this ledger.
