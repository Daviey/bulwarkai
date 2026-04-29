# ADR 6: Why scratch Docker runtime

Date: 2025-04

## Status

Accepted

## Context

Docker images typically use `alpine` or `debian-slim` as a base runtime image. These include a package manager, shell, and OS utilities.

## Decision

Use `scratch` as the runtime image. The Go binary is compiled with `CGO_ENABLED=0` and static linking flags `-ldflags="-s -w"`. Only the binary and `ca-certificates.crt` are copied from the builder stage.

## Consequences

7MB image. Zero OS packages, which eliminates an entire class of container vulnerability (OS package CVEs). No shell, no package manager, no attack surface beyond the binary itself.

The trade-off is debuggability: you cannot shell into a scratch container. For a Cloud Run service with structured logging, this is not a problem. If debugging is needed, run the binary locally with `LOCAL_MODE=true`.
