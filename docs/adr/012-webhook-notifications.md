# ADR-012: Webhook Notifications

## Status

Accepted

## Context

Block events are logged to Cloud Logging, but teams often need real-time notifications when sensitive data is blocked. Polling logs introduces latency and requires log-scanning infrastructure. A push-based notification model gets the event to the right system immediately.

## Decision

An asynchronous webhook notifier sends HTTP POST requests for every BLOCK and DENY event to a configurable URL (`WEBHOOK_URL`). Notifications are queued in a buffered channel (256 events) and processed by a background goroutine. If the queue is full, events are dropped and a warning is logged.

The webhook payload is a JSON object with: timestamp, action, model, email, reason, request_id, and redacted prompt. A shared secret (`WEBHOOK_SECRET`) is sent in the `X-Webhook-Secret` header for verification.

## Consequences

Webhook delivery is fire-and-forget: no retries, no ordering guarantees. This keeps the implementation simple and prevents webhook backpressure from affecting request latency. The buffered queue absorbs brief spikes. If the downstream system is down for an extended period, events are dropped after the queue fills.

The webhook runs on the same Cloud Run instance as the proxy. If the instance is replaced (new revision, scaling event), in-flight webhook deliveries are drained during the 30-second shutdown grace period. Events in the queue at termination time are sent before the process exits.
