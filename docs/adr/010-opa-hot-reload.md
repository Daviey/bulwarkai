# ADR-010: OPA Policy Hot-Reload

## Status

Accepted

## Context

The OPA policy engine loads policy at startup. Changing a policy currently requires a new Cloud Run revision, which takes 30-60 seconds and creates operational friction. Policy changes are often urgent: revoking a user's access, blocking a new model, or tightening rate limits during an incident.

## Decision

The policy engine watches the configured source for changes and reloads automatically:

- `OPA_POLICY_FILE`: polled every 5 seconds for modification time changes.
- `OPA_POLICY_URL` (HTTP): polled every 30 seconds.

When a change is detected, the new content is compiled. If compilation succeeds, the prepared query is swapped in under a read-write lock. If compilation fails, the old policy remains active and an error is logged.

## Consequences

Policy changes take effect within one poll interval without restarts or new revisions. A bad policy cannot block all traffic because failed compilations preserve the existing policy. The polling approach avoids requiring fsnotify or inotify, which are not available in a `scratch` container.

The read-write lock means policy evaluation acquires a read lock (cheap, concurrent) and reload acquires a write lock (exclusive, brief). Reload is infrequent and the write lock is held only for the pointer swap, not during compilation.
