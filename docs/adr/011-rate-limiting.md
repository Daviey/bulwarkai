# ADR-011: Rate Limiting

## Status

Accepted

## Context

Without rate limiting, a single user (or a compromised API key) can send unlimited requests to Vertex AI through the proxy. This creates cost risk and potential for abuse. The proxy needs a way to cap per-user request rates.

## Decision

A fixed-window rate limiter is implemented in-process using a `sync.Mutex` and a map of email-to-counter pairs. The limiter is enabled via `RATE_LIMIT` (max requests per window) and `RATE_LIMIT_WINDOW` (time duration). When disabled (default), no overhead is incurred.

Rate-limited requests receive HTTP 429. The limiter runs as middleware before authentication processing.

## Consequences

This approach works for single-instance Cloud Run deployments. For multi-instance deployments, the in-process map is not shared across instances, so limits are per-instance rather than global. A future iteration could use an external store (Redis, Memorystore) for shared rate limiting.

The fixed-window approach allows brief bursts at window boundaries (up to 2x the limit in the worst case across adjacent windows). A sliding-window or token-bucket implementation would be more precise but adds complexity that is not justified for the initial implementation.
