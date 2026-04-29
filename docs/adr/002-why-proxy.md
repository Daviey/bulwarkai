# ADR 2: Why a proxy, not a library

Date: 2025-04

## Status

Accepted

## Context

Bulwarkai could have been a shared library that each client application imports (Go module, npm package, Python wheel). Clients would call the library before making Vertex AI requests.

## Decision

Build a network proxy that sits between clients and Vertex AI.

## Consequences

Enforcement is outside the client's control. No client can bypass screening by misconfiguring the library or skipping an update. A single deployment point means one place to update inspection rules, one place to audit, one IAM boundary.

The cost is operational: another service to deploy, monitor, and maintain. Cloud Run absorbs most of this (auto-scaling, health checks, logging). The proxy adds 50-500ms latency depending on the response mode and inspector chain.

This is a standard pattern for security controls in regulated environments. Google's own documentation recommends a similar architecture using Apigee as the proxy layer.
