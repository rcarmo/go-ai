# Bedrock credential precedence reaches the signed request

All four unchanged `bedrock-credentials.test.ts` originals passed after the test, adapter, public dispatch, runner config and npm lock matched pinned Git. The unchanged Go provider package passed all 57 top-level tests. The new run uses the same model ID and the existing public Go adapter and SDK, with synthetic access keys, a private home and a fresh subprocess per case.

A local fixture inspects the effective signer ID and exact request path. Explicit and scoped profiles displace the ambient key. No profile, an ambient-only competing profile, and a proxied request keep the ambient key. The proxy case uses a loopback HTTP proxy and receives the signed request once. No AWS service or provisioned credential is reached. The original mocks a JavaScript SDK constructor; Go determines effective precedence from the wire. Constructor fields, exact secret bytes and the callback type are outside this comparison. The separate [profile proxy test](bedrock-profile-proxy.md) covers the credential chain itself.

The new table and the existing profile proxy test passed twice without skips. Compiled controls that prefer ambient keys over an explicit profile and prefer an ambient profile over static keys both failed at the exact signer comparison. Vet and Staticcheck v0.8.2 passed. No production change was needed.
