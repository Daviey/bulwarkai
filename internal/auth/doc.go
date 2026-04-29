/*
Package auth handles request authentication for Bulwarkai.

It supports two methods:

 1. JWT Bearer Token: the Authorization header carries an OIDC identity
    token. The email is extracted from the JWT payload and checked against
    the configured domain allowlist. The X-Forwarded-Access-Token header
    carries the OAuth access token used for Vertex AI calls.

 2. API Key: the X-Api-Key header is validated against a configured list.
    Identity is synthetic: apikey@<first-allowed-domain>.

Authenticate returns an Identity with the caller's email and access token,
or writes an error response and returns nil.

CheckUserAgent enforces an optional regex on the User-Agent header.

LOCAL_MODE skips all authentication and returns a fixed "local@localhost"
identity.
*/
package auth
