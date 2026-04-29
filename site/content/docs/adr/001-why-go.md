+++
title = "ADR 1: Why Go"
weight = 1
+++


Date: 2025-04

## Status

Accepted

## Context

The first version was Node.js. It worked for prototyping but had problems as a security proxy: V8 startup time added latency to Cloud Run cold starts, loose error handling in async chains made it easy to swallow inspector failures silently, and the dependency tree (`node_modules`) made supply-chain audits impractical for a security-critical service.

## Decision

Rewrite in Go.

## Consequences

Single static binary, no runtime dependencies. `scratch` Docker image at 7MB with zero OS packages. Cold start under 1 second on Cloud Run. Explicit error handling forces every failure path to be visible in the code. The Go module has three direct dependencies: `google/uuid`, `anthropic-sdk-go` (tests only), `openai-go` (tests only), and `oauth2/google` (ADC support).

The trade-off is more boilerplate for JSON manipulation compared to JavaScript's dynamic typing. The `map[string]interface{}` pattern for translating API formats is verbose but explicit about what the code expects.
