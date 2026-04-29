# ADR 4: Why fail-open on inspector errors

Date: 2025-04

## Status

Accepted

## Context

Inspectors call external services (Model Armor, DLP). These services can return errors, time out, or be unreachable. When an inspector fails, the service must decide whether to block the request (fail-closed) or let it through (fail-open).

## Decision

Fail-open. If an inspector cannot reach its backend, the request passes through.

## Consequences

The service does not become a single point of failure for all AI access. A DLP outage does not block every developer from using Vertex AI.

The risk is that a request passes through unscreened during an outage. This is mitigated by two factors:

1. In `strict` mode, Model Armor's built-in Vertex AI integration provides independent enforcement on `generateContent` calls. Even if the standalone API is down, the platform-level screening still works.

2. All pass-through events are logged at WARN level. Monitoring can alert on spikes in inspector errors.

The alternative (fail-closed) would make the proxy a critical dependency. If Model Armor has a regional outage, every AI tool in the organisation stops working. For a stopgap proxy that exists to fill a gap in Google's platform, adding that level of criticality is not justified.
