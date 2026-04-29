# Operations

## Monitoring and Alerting

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

### Recommended alerts

Alert on these conditions in Cloud Logging:

`BLOCK_PROMPT` or `BLOCK_RESPONSE` count exceeds threshold in a rolling window. A spike in blocks might indicate a compromised account or a developer processing bulk sensitive data.

`BLOCK_RESPONSE_AUDIT` count above zero. In audit mode, violations are logged but the response was already sent to the client. Any non-zero count means sensitive data leaked.

`DENY_DOMAIN` occurrences. Someone outside the organisation is trying to access the service.

Error-level logs from the service. These indicate Vertex AI outages, Model Armor failures, or internal errors.

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
severity>=ERROR
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

The binary is ~7MB. Cold start on Cloud Run is typically under 1 second. The service has no startup initialisation beyond reading environment variables and compiling regex patterns. Model Armor and DLP inspectors do not pre-warm connections.

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

`dlp non-ok response` or `model armor error` entries at WARN level indicate the inspector backend returned an error. The service treats these as pass (fail-open). Check network connectivity from Cloud Run to the backend, and verify the service account has the correct IAM roles (`roles/modelarmor.user`, `roles/dlp.reader`).

### Debug logging

Set `LOG_LEVEL=debug` to see additional detail including request bodies, Vertex AI responses, and inspector decisions. Do not use debug logging in production for extended periods; it logs full prompt and response text.

## Known Issues

Model Armor does not enforce on streaming endpoints (`streamGenerateContent` / `streamRawPredict`). Only `generateContent` (non-streaming) gets screened. Google is aware of this gap. This is why `strict` mode uses non-streaming calls.

Anthropic models are not enabled in Vertex AI Model Garden for all organisations. The `/v1/messages` endpoint works for testing via curl but cannot be used with real Anthropic models until org permissions are updated.

The opencode client sends two requests per run: one for the user's task and one for title generation using a small model. Without `small_model` configured in opencode, it defaults to a model that doesn't exist, causing 404 errors on the second request.
