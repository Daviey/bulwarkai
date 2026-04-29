+++
title = "ADR 3: Why three API formats"
weight = 3
+++


Date: 2025-04

## Status

Accepted

## Context

Three client types need access: opencode (OpenAI Chat Completions), Claude Code (Anthropic Messages API), and tools that call Vertex AI directly (Gemini native format). Each speaks a different wire format.

## Decision

Accept all three formats at the proxy level and translate internally to Gemini.

## Consequences

No client-side changes are needed to adopt the proxy. Each client keeps its existing SDK or HTTP integration and just changes the base URL. The translation layer adds complexity (three sets of request parsing, response formatting, and SSE streaming) but this is bounded and well-tested.

The alternative was forcing all clients through one format. This would require either patching opencode or Claude Code (not practical) or building a format-translation layer in each client anyway (defeating the purpose of a centralised proxy).
