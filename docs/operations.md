# Operations

## Monitoring and Alerting

The service exposes Prometheus metrics at `/metrics` and structured JSON logs to stdout. Both are collected by Cloud Monitoring and Cloud Logging when running on Cloud Run.

### Prometheus metrics

All metrics are prefixed with `bulwarkai_`.

#### Request counters

`bulwarkai_requests_total{action, model}`

Counter for every completed request. The `action` label values:

| Label value | Meaning |
|---|---|
| `ALLOW` | Passed all inspectors, full screening |
| `ALLOW_FAST` | Passed prompt screening, response not screened |
| `ALLOW_AUDIT` | Passed, response streamed, post-hoc audit logged |
| `BLOCK_PROMPT` | Prompt blocked by an inspector |
| `BLOCK_RESPONSE` | Response blocked by an inspector (strict mode) |
| `BLOCK_RESPONSE_AUDIT` | Response violation detected after streaming (audit mode) |
| `MODEL_ARMOR_BLOCK` | Vertex AI returned promptFeedback with blockReason |
| `DENY_DOMAIN` | Email domain not in allowlist |
| `DENY_UA` | User-Agent did not match configured regex |
| `DENY_POLICY` | OPA policy engine denied the request |

#### Inspector results

`bulwarkai_inspector_results_total{inspector, direction, result}`

Per-inspector evaluation counter. The `inspector` label is `regex`, `model_armor`, or `dlp`. The `direction` label is `prompt` or `response`. The `result` label:

| Label value | Meaning |
|---|---|
| `pass` | Inspector evaluated the text and found nothing |
| `block` | Inspector detected a violation |
| `error` | Inspector could not evaluate (network error, non-200 response, decode failure). The request was allowed through (fail-open). |

This is the primary metric for building dashboards and alerts. You can break down deny rates and error rates per inspector.

#### Policy engine results

`bulwarkai_policy_results_total{result}`

OPA policy engine evaluation counter. Only recorded when `OPA_ENABLED=true`. The `result` label:

| Label value | Meaning |
|---|---|
| `allow` | Policy evaluated to allow |
| `deny` | Policy evaluated to deny |
| `error` | Policy evaluation failed (bad query, OOM). The request was allowed through (fail-open). |

#### Latency histograms

`bulwarkai_request_duration_seconds{action}`

End-to-end request duration.

`bulwarkai_inspector_duration_seconds{inspector, direction}`

Per-inspector call duration. Buckets: 1ms, 5ms, 10ms, 25ms, 50ms, 100ms, 250ms, 500ms, 1s, 2.5s, 5s.

#### Gauges

`bulwarkai_active_requests`

Number of requests currently being processed.

`bulwarkai_request_body_bytes`

Request body size histogram.

### Alerting examples

These PromQL queries work with Cloud Monitoring Prometheus-style alerts or any Prometheus server scraping the `/metrics` endpoint.

#### Inspector fail-open (any backend down)

```promql
sum by (inspector) (rate(bulwarkai_inspector_results_total{result="error"}[5m])) > 0
```

Fire this on any non-zero rate. A single `error` result means an inspector backend (Model Armor, DLP) is unreachable and requests are passing through unscreened. This is the highest-priority alert.

#### Per-inspector fail-open

```promql
rate(bulwarkai_inspector_results_total{inspector="model_armor",result="error"}[5m]) > 0
```

```promql
rate(bulwarkai_inspector_results_total{inspector="dlp",result="error"}[5m]) > 0
```

Use separate alerts per inspector if you want different escalation paths. Model Armor being down is more urgent than DLP being down because strict mode relies on Model Armor's Vertex AI integration as a backstop.

#### Inspector deny rate

```promql
sum by (inspector) (rate(bulwarkai_inspector_results_total{result="block"}[5m]))
```

Graph this per inspector to see which one catches the most violations. Regex catches known patterns (SSNs, keys). DLP catches structured data. Model Armor catches semantic violations.

#### Inspector deny rate as percentage of total evaluations

```promql
sum by (inspector) (rate(bulwarkai_inspector_results_total{result="block"}[5m]))
/
sum by (inspector) (rate(bulwarkai_inspector_results_total[5m]))
* 100
```

A sudden spike in deny percentage for a single inspector (especially regex) might indicate a compromised account sending bulk sensitive data, or a new pattern in model responses that was not there before.

#### OPA policy deny rate

```promql
rate(bulwarkai_policy_results_total{result="deny"}[5m])
```

```promql
rate(bulwarkai_policy_results_total{result="error"}[5m]) > 0
```

The `error` counter fires when OPA evaluation fails. A broken Rego policy would cause this.

#### Audit mode data leakage

```promql
rate(bulwarkai_requests_total{action="BLOCK_RESPONSE_AUDIT"}[5m]) > 0
```

In audit mode, `BLOCK_RESPONSE_AUDIT` means sensitive data was detected in the response *after* it was already sent to the client. Any non-zero rate is a data leakage incident.

#### Overall block rate

```promql
sum(rate(bulwarkai_requests_total{action=~"BLOCK.*"}[5m]))
```

Compare against total request rate:

```promql
sum(rate(bulwarkai_requests_total{action=~"BLOCK.*"}[5m]))
/
sum(rate(bulwarkai_requests_total[5m]))
* 100
```

A baseline of 0.1-1% blocks is normal (users occasionally paste sensitive data). A sudden jump to 5%+ warrants investigation.

#### Per-model block rate

```promql
sum by (model) (rate(bulwarkai_requests_total{action=~"BLOCK.*"}[5m]))
```

If one model has a much higher block rate than others, its responses may contain sensitive patterns. This is common with models that generate code samples (API keys, credentials in example code).

#### Inspector latency percentile

```promql
histogram_quantile(0.99, sum by (le, inspector) (rate(bulwarkai_inspector_duration_seconds{direction="prompt"}[5m])))
```

Regex should be under 1ms at p99. Model Armor is typically 100-300ms. DLP is typically 200-500ms. If p99 latency spikes above 2 seconds, the backend may be throttling or network latency has increased.

#### Request volume by user

```promql
sum by (model) (rate(bulwarkai_requests_total[1h]))
```

Correlate with log queries (see below) to identify which users are generating blocks.

### Dashboard layout

A useful Cloud Monitoring dashboard includes:

Panel 1: Request rate by action (stacked bar chart)
```
sum by (action) (rate(bulwarkai_requests_total[5m]))
```

Panel 2: Inspector results by result type (stacked bar chart)
```
sum by (inspector, result) (rate(bulwarkai_inspector_results_total[5m]))
```

Panel 3: Inspector error rate (single stat, alert on non-zero)
```
sum(rate(bulwarkai_inspector_results_total{result="error"}[5m]))
```

Panel 4: p50/p95/p99 request latency (line chart)
```
histogram_quantile(0.5, sum(rate(bulwarkai_request_duration_seconds_bucket[5m])) by (le))
histogram_quantile(0.95, sum(rate(bulwarkai_request_duration_seconds_bucket[5m])) by (le))
histogram_quantile(0.99, sum(rate(bulwarkai_request_duration_seconds_bucket[5m])) by (le))
```

Panel 5: Active requests (gauge)
```
bulwarkai_active_requests
```

Panel 6: Policy engine results (if OPA enabled)
```
sum by (result) (rate(bulwarkai_policy_results_total[5m]))
```

### Log actions

Every request produces a structured log entry. The `action` field tells you what happened:

| Action | Level | Meaning |
|---|---|---|
| `ALLOW` | INFO | Request passed all inspectors, full screening |
| `ALLOW_FAST` | INFO | Request passed prompt screening, response not screened |
| `ALLOW_AUDIT` | INFO | Request passed, response streamed, post-hoc audit logged separately |
| `BLOCK_PROMPT` | WARN | Prompt blocked by an inspector |
| `BLOCK_RESPONSE` | WARN | Response blocked by an inspector (strict mode) |
| `BLOCK_RESPONSE_AUDIT` | WARN | Response violation detected after streaming (audit mode) |
| `MODEL_ARMOR_BLOCK` | WARN | Vertex AI returned promptFeedback with blockReason |
| `DENY_DOMAIN` | WARN | Authenticated user's email domain not in allowlist |
| `DENY_UA` | WARN | User-Agent did not match configured regex |
| `DENY_POLICY` | WARN | OPA policy engine denied the request |

Inspector errors are logged separately at ERROR level with `inspector error (fail-open)`.

### Recommended alerts

Alert on these conditions:

Inspector fail-open (any `result="error"` on `bulwarkai_inspector_results_total`). This is the highest priority alert. It means an inspector backend is down and requests are passing through without screening.

`BLOCK_RESPONSE_AUDIT` count above zero. In audit mode, violations are logged but the response was already sent to the client. Any non-zero count means sensitive data leaked.

`BLOCK_PROMPT` or `BLOCK_RESPONSE` spike. A sudden increase might indicate a compromised account or a developer processing bulk sensitive data.

`DENY_DOMAIN` occurrences. Someone outside the organisation is trying to access the service.

OPA `result="error"` on `bulwarkai_policy_results_total`. The policy engine is failing and allowing all traffic through.

### Log query examples

All blocked requests in the last hour:

```
resource.type="cloud_run_revision"
resource.labels.service_name="bulwarkai"
jsonPayload.action=~"BLOCK.*"
timestamp>"2025-01-01T00:00:00Z"
```

Requests from a specific user:

```
resource.type="cloud_run_revision"
resource.labels.service_name="bulwarkai"
jsonPayload.email="user@example.com"
```

Inspector errors (fail-open events):

```
resource.type="cloud_run_revision"
resource.labels.service_name="bulwarkai"
jsonPayload.message="inspector error (fail-open)"
```

OPA policy denials:

```
resource.type="cloud_run_revision"
resource.labels.service_name="bulwarkai"
jsonPayload.action="DENY_POLICY"
```

### Request tracing

Each request gets a `request_id` in the log entries. Pass `X-Request-ID: <value>` in the request to use a specific ID, or the service generates a UUID. When Cloud Run receives a request, it sets `X-Cloud-Trace-Context`, which the service extracts as `trace_id`. Use these to correlate logs across the request lifecycle.

## Scaling and Rate Limits

### Cloud Run configuration

The Terraform configures the service with 2 vCPU and 1Gi memory per instance. This handles the JSON parsing, regex matching, and HTTP forwarding without memory pressure. Vertex AI calls are the bottleneck (1-10 seconds per request depending on model and prompt size).

Cloud Run scales instances based on concurrent requests. The default max concurrency is 80. With strict mode (non-streaming, synchronous), each request holds a connection for the full Vertex AI round-trip. Expect roughly 10-20 concurrent requests per instance in strict mode.

### Timeouts

Cloud Run request timeout is set to 300 seconds in the Terraform config. This accommodates large prompts on slower models. The Go HTTP client has no explicit timeout; it inherits Cloud Run's request deadline via context cancellation.

Strict mode adds latency from the response screening pass (typically under 50ms for regex, 200-500ms for Model Armor or DLP). The total added latency is: prompt screening (before forwarding) + response screening (after receiving).

### Cold starts

The binary is ~25MB (including the OPA rego runtime). Cold start on Cloud Run is typically under 1 second. The service has no startup initialisation beyond reading environment variables, compiling regex patterns, and (if OPA is enabled) compiling the Rego policy. Model Armor and DLP inspectors do not pre-warm connections.

### Vertex AI quotas

Vertex AI has per-project quotas for `generateContent` and `streamGenerateContent` requests per minute. The Bulwarkai does not batch or queue requests; each client request maps to one Vertex AI call. If you hit the quota, Vertex AI returns 429 errors and the Bulwarkai returns 502 to the client. Monitor `server_side_error_count` in Cloud Run metrics and request quota increases from Google Cloud Console.

## Troubleshooting

### 401 Unauthorized

The bearer token was rejected. Common causes:

The token is expired. OIDC identity tokens from `gcloud auth print-identity-token` expire after 1 hour. The opencode plugin caches tokens for 50 minutes and refreshes automatically. For manual curl, generate a fresh token each time.

The token is an access token, not an identity token. Cloud Run IAM requires OIDC identity tokens in the `Authorization` header. OAuth access tokens go in `X-Forwarded-Access-Token` instead. Check that you are using `print-identity-token` for the `Authorization` header.

The service account or user does not have `roles/run.invoker` on the Cloud Run service. Grant it via Terraform (`allowed_iam_members` variable) or `gcloud run services add-iam-policy-binding`.

### 403 Domain not allowed

The email extracted from the token is not in `ALLOWED_DOMAINS`. The log entry shows `DENY_DOMAIN` with the rejected domain. Add the domain to the env var or Terraform variable.

### 403 Denied by policy

The OPA policy engine returned deny. Check the log entry for `DENY_POLICY` with the reason string. The reason comes from the `deny_reason` rule in the Rego policy. Check the policy file configured in `OPA_POLICY_FILE` or `OPA_POLICY_URL`.

### 400 Bad Request

The request body could not be parsed as JSON, or required fields were missing. The Anthropic endpoint requires `messages` (array) and `max_tokens` (integer). The OpenAI endpoint requires `messages` (array). Check that `Content-Type: application/json` is set.

### 502 Bad Gateway

Vertex AI returned an error. Check the service logs for `vertex json error` or `vertex stream error`. Common causes:

The model name is wrong. Use `gemini-2.5-flash` or whichever model is available in your project and region.

The user's access token does not have `aiplatform.user` permissions. The Bulwarkai forwards the user's token, so the user needs this role, not just the service account.

Vertex AI quota exceeded. Check the `Quotas` page in Google Cloud Console.

### Empty response from streaming

Gemini 2.5 Flash uses thinking tokens. If `max_tokens` is set too low (e.g. 256), the model spends all tokens on internal reasoning and returns empty content. Set `max_tokens` to 4096 or higher.

### Requests work locally but fail from opencode/Claude Code

Check that the client is configured to point at the Bulwarkai URL, not directly at Vertex AI. The opencode plugin overrides the base URL. If the plugin is not loaded, opencode will call Vertex AI directly and bypass all screening.

Also check that `small_model` is configured in opencode. Without it, the title generation agent uses a model that may not exist, causing a 404 on the second request. The user's main request succeeds, but the title generation fails silently.

### Inspector errors in logs

`inspector error (fail-open)` entries at ERROR level indicate the inspector backend returned an error or was unreachable. The service treats these as pass. Check network connectivity from Cloud Run to the backend, and verify the service account has the correct IAM roles (`roles/modelarmor.user`, `roles/dlp.reader`).

The `bulwarkai_inspector_results_total{result="error"}` metric counts these events per inspector. Alert on any non-zero rate.

### Debug logging

Set `LOG_LEVEL=debug` to see additional detail including request bodies, Vertex AI responses, and inspector decisions. Do not use debug logging in production for extended periods; it logs full prompt and response text.

## Known Issues

Model Armor does not enforce on streaming endpoints (`streamGenerateContent` / `streamRawPredict`). Only `generateContent` (non-streaming) gets screened. Google is aware of this gap. This is why `strict` mode uses non-streaming calls.

Anthropic models are not enabled in Vertex AI Model Garden for all organisations. The `/v1/messages` endpoint works for testing via curl but cannot be used with real Anthropic models until org permissions are updated.

The opencode client sends two requests per run: one for the user's task and one for title generation using a small model. Without `small_model` configured in opencode, it defaults to a model that doesn't exist, causing 404 errors on the second request.
