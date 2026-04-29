# ADR 007: Response mode naming

Date: 2026-04-30

## Context

The three response modes were originally named `strict`, `fast`, and `buffer`. The name `buffer` was ambiguous (buffer what?) and did not convey that the mode streams responses in real time while auditing them after the fact.

## Decision

Rename `buffer` to `audit` to describe what the mode actually does: audit the response after streaming. Add `input_only` as an alias for `fast` since some users think of that mode as "screen input only, pass output through."

Old names are accepted as aliases via `normalizeMode()` in `config.go` and mapped to the canonical names. No breaking change.

## Consequences

- `audit` is the canonical name, `buffer` is a backwards-compatible alias
- `fast` is the canonical name, `input_only` is a backwards-compatible alias
- Logs emit `ALLOW_AUDIT` (not `ALLOW_BUFFER`)
- The `/health` endpoint reports the canonical name
