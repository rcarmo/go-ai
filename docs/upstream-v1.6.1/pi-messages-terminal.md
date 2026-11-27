# Pi Messages preserves terminal usage and backend model

All 11 original `pi-messages.test.ts` cases passed unchanged after byte verification. The unchanged Go provider package also passed. The matched native witnesses then showed that the first partial was `stop`, the gateway's cost and model were lost on both terminal paths, and the public `ToolChoice` option did not reach the request body. Exact event observations, header handling, failure diagnostics, missing-key and premature-EOF checks, and built-in registration passed on the base.

Go starts the partial message at `pending`, accepts the represented cost components and optional response model on both `done` and `error`, and uses the public tool choice when no explicit metadata override is supplied. The requested model, response ID, reasoning level and content remain distinct. The known API string matches; Go does not enforce the TypeScript built-in API name union. The exact Go request options match, with an array of text blocks where the upstream Go-inexpressible fixture uses a bare string.

The complete native package passed twice without skips. Four compiled controls caught dropping the cost, model, typed tool-choice projection or pending state. Vet and Staticcheck v0.8.2 passed. No remote request was made.
