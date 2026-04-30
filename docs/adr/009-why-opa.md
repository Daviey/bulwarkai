# ADR-009: Why OPA for Access Control

## Status

Accepted

## Context

The service needs model-level RBAC and usage policies. Examples: only certain users can access expensive models, streaming is restricted to specific teams, max_tokens limits vary by group. These are access control decisions, not content safety decisions. The inspector chain handles content safety.

## Decision

Embed Open Policy Agent (OPA) as an in-process Go library using the `rego` package. Policies are written in Rego and loaded at startup from a file or inline content.

## Alternatives Considered

1. Hardcoded config maps (env vars). Simpler but not expressive enough. Cannot express "streaming only for team X" or "max_tokens limited by group" without a combinatorial explosion of env vars.

2. OPA sidecar. Adds a network hop (~1ms) and operational complexity (extra container, health checks, resource limits). Policy evaluation is microseconds in-process.

3. OPA SDK package (`v1/sdk`). Requires YAML config, plugin infrastructure, and bundle management. Overkill for loading a single policy file.

4. Custom rule engine. Would be reinventing Rego with worse expressiveness and no ecosystem.

## Consequences

- OPA adds ~18MB to the binary size (the rego package includes the compiler and evaluator).
- Policies are loaded at startup. Changing a policy requires a new Cloud Run revision.
- OPA is off by default. Enabling it is a conscious choice.
- OPA never sees prompt or response text. It only sees identity, model, and request parameters.
- Fail-open on evaluation errors. A broken policy does not block all traffic.
