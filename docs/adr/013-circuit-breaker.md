# ADR-013: Circuit Breaker for Vertex AI

## Status

Accepted

## Context

When Vertex AI is degraded or unavailable (quota exceeded, regional outage, network partition), every request through Bulwarkai fails with a 502 error. Each failed request still consumes a full request lifecycle: JSON parsing, inspector chain evaluation, HTTP connection setup, and timeout wait. Under sustained failures, this wastes resources and adds latency to every request.

The Cloud Run request timeout is 300 seconds. If Vertex AI is slow but not down, requests pile up and exhaust instance concurrency.

## Decision

Add a circuit breaker around Vertex AI calls. The breaker tracks consecutive failures and opens after a threshold (5 consecutive failures). When open, requests are rejected immediately with a clear error message, skipping all downstream processing.

After a reset timeout (30 seconds), the breaker transitions to half-open and allows a single probe request. If the probe succeeds, the breaker closes and normal traffic resumes. If the probe fails, the breaker reopens.

The circuit breaker is embedded in the `vertex.Client` struct. It wraps all four call methods: `CallJSON`, `CallJSONForModel`, `CallStream`, and `CallStreamRaw`.

## Consequences

When Vertex AI is down, clients receive fast rejections instead of slow timeouts. The breaker state is exposed in the `/health` endpoint and as a Prometheus gauge (`bulwarkai_circuit_breaker_state`).

The breaker uses consecutive failures rather than a time-windowed rate. This means a single burst of failures opens the circuit. A time-windowed approach would be more forgiving but adds complexity for little benefit in this use case.

The breaker does not distinguish between transient errors (429 quota) and permanent errors (401 auth). Auth errors should arguably not count toward the failure threshold. This refinement is deferred.
