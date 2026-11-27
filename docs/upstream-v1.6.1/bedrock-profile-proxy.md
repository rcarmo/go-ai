# Bedrock profile credentials use the configured proxy

The unchanged `bedrock-proxy-credentials.test.ts` case passed on Node 26.13.0 and locked Vitest 4.1.12. The test, provider, public dispatch, Vitest config and npm lock matched the pin. The process could reach only its loopback proxy. The unchanged Go Bedrock package baseline also passed before the comparison was added.

Go runs the public adapter in a fresh subprocess with a synthetic profile, private home, invented cached token and a loopback proxy. A rejected `CONNECT sso.invalid:443` produces one terminal error. Using a fresh process prevents prior HTTP proxy or shared-config state from affecting the test. The Go fixture limits SDK attempts to one; the original does not assert retry count. Neither fixture reaches an AWS endpoint or uses provisioned credentials.

The focused Go test passed twice, with vet and Staticcheck. An isolated Go overlay that uses a credential client without a proxy still returned an error, but failed the exact proxy-target check. No production change was required.
