# ADR 5: Why user tokens pass through

Date: 2025-04

## Status

Accepted

## Context

The service could use its own service account to call Vertex AI. This would be simpler: one set of credentials, no token forwarding, no two-token design.

## Decision

Forward the user's OAuth access token to Vertex AI.

## Consequences

Vertex AI audit logs show which human made each request, not which service account. When three developers route through the same proxy, the audit trail distinguishes between them.

The cost is the two-token design. Cloud Run IAM requires an OIDC identity token for invocation (the `Authorization` header). Vertex AI requires an OAuth access token for API calls. The client must send both. The opencode plugin handles this by requesting a combined-scope token (`cloud-platform openid email`) from Google OAuth, which returns both tokens in one call.

In LOCAL_MODE, the service uses ADC instead of forwarded tokens. This is acceptable for development because audit identity is not a concern on a developer's laptop.
