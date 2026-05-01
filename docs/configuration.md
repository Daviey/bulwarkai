# Configuration Reference

Every setting is an environment variable. There are no config files.

The service reads env vars at startup. Changing any value requires a restart (or a new Cloud Run revision).

## Required

| Variable | Type | Default | Description |
|---|---|---|---|
| `GOOGLE_CLOUD_PROJECT` | string | none | GCP project ID. Used for Vertex AI, Model Armor, DLP, and token validation. |
| `ALLOWED_DOMAINS` | comma-separated string | none | Email domain allowlist. Only users with a matching domain in their OIDC token can use the service. Leave empty to disable domain checking entirely. |

## Region

| Variable | Type | Default | Description |
|---|---|---|---|
| `GOOGLE_CLOUD_LOCATION` | string | `europe-west2` | Region for Vertex AI calls. Also used as the default region for Model Armor and DLP when their own `_LOCATION` vars are not set. |

## Model

| Variable | Type | Default | Description |
|---|---|---|---|
| `FALLBACK_GEMINI_MODEL` | string | `gemini-2.5-flash` | Model used when a request does not specify one. Must be a valid Gemini model name recognised by Vertex AI. |
| `RESPONSE_MODE` | string | `strict` | Controls how responses are screened. Options below. |

`RESPONSE_MODE` values:

| Value | Aliases | Behaviour |
|---|---|---|
| `strict` | | Both prompt and response are screened before the client receives anything. Non-streaming. Vertex AI's built-in Model Armor is active because the call goes through `generateContent`. |
| `fast` | `input_only` | Prompt is screened, response is streamed to the client without screening. Lower latency at the cost of no response inspection. |
| `audit` | `buffer` | Prompt is screened, response is streamed to the client, but the full response is also screened after delivery. Violations are logged (`BLOCK_RESPONSE_AUDIT`) but the data has already been sent. |

The aliases `input_only` and `buffer` are accepted and mapped internally to `fast` and `audit`.

## Model Armor

| Variable | Type | Default | Description |
|---|---|---|---|
| `MODEL_ARMOR_TEMPLATE` | string | `test-template` | Template name for the Model Armor standalone API (`sanitizeUserPrompt` / `sanitizeModelResponse`). Set to an empty string to disable the Model Armor inspector in fast and audit modes. In strict mode, Vertex AI's built-in Model Armor handles screening regardless of this setting. |
| `MODEL_ARMOR_LOCATION` | string | value of `GOOGLE_CLOUD_LOCATION` | Region for the Model Armor endpoint. Falls back to `GOOGLE_CLOUD_LOCATION` if not set. |

Model Armor is loaded as an inspector when `MODEL_ARMOR_TEMPLATE` is non-empty and `RESPONSE_MODE` is not `strict`. In strict mode the Vertex AI `generateContent` call itself enforces Model Armor, so a separate inspector is redundant.

## Cloud DLP

| Variable | Type | Default | Description |
|---|---|---|---|
| `DLP_API` | string | unset | Set to `"true"` to enable the DLP inspector. Any other value (or unset) disables it. This is a boolean toggle, not an endpoint URL. |
| `DLP_LOCATION` | string | value of `GOOGLE_CLOUD_LOCATION` | Region for DLP `content:inspect` calls. Falls back to `GOOGLE_CLOUD_LOCATION` if not set. |
| `DLP_INFO_TYPES` | comma-separated string | `US_SOCIAL_SECURITY_NUMBER,CREDIT_CARD_NUMBER,EMAIL_ADDRESS,PHONE_NUMBER,PERSON_NAME,STREET_ADDRESS,DATE_OF_BIRTH` | Info types the DLP inspector checks for. Full list of valid types: https://cloud.google.com/sensitive-data-protection/docs/infotypes-reference |
| `DLP_MIN_LIKELIHOOD` | string | `LIKELY` | Minimum likelihood for DLP findings. Findings below this threshold are ignored. Options: `VERY_UNLIKELY`, `UNLIKELY`, `POSSIBLE`, `LIKELY`, `VERY_LIKELY`. |
| `DLP_ENDPOINT` | string | `https://dlp.googleapis.com` | Override for the DLP API base URL. Primarily used for testing with a mock server. |

## Policy Engine (OPA)

| Variable | Type | Default | Description |
|---|---|---|---|
| `OPA_ENABLED` | string | unset | Set to `"true"` to enable the OPA policy engine. Any other value (or unset) disables it. |
| `OPA_POLICY_FILE` | string | none | Path to a Rego policy file on disk. Loaded at startup and watched for changes. When the file changes, the policy is recompiled and swapped in atomically. |
| `OPA_POLICY_URL` | string | none | URL to fetch Rego policy content from. Supports any HTTP(S) URL (GCS signed URLs, S3 presigned URLs, internal config servers). The URL is polled every 30 seconds and the policy is reloaded when the response changes. Can also be used for inline Rego content (non-URL strings are compiled directly). |

When both `OPA_POLICY_FILE` and `OPA_POLICY_URL` are set, the file takes precedence.

Hot-reload behavior: if the new policy fails to compile, the old policy remains active and an error is logged. This prevents a syntax error in a policy file from blocking all traffic.

## Rate Limiting

| Variable | Type | Default | Description |
|---|---|---|---|
| `RATE_LIMIT` | int | `0` | Maximum number of requests per user per time window. Set to 0 to disable rate limiting. |
| `RATE_LIMIT_WINDOW` | string (Go duration) | `1m` | Time window for rate limit counting. Must be a valid Go duration string (e.g. `30s`, `5m`, `1h`). |

Rate limiting is per email address. Requests that exceed the limit receive HTTP 429. API key requests are rate-limited under the shared `apikey@domain` identity.

## Webhook Notifications

| Variable | Type | Default | Description |
|---|---|---|---|
| `WEBHOOK_URL` | string | none | URL to send HTTP POST notifications for BLOCK and DENY events. When set, every block event triggers an async JSON payload to this URL. Leave empty to disable webhook notifications. |
| `WEBHOOK_SECRET` | string | none | Secret token sent in the `X-Webhook-Secret` header with each notification. Use this to verify that payloads come from Bulwarkai. |

The webhook payload is a JSON object with fields: `timestamp`, `action`, `model`, `email`, `reason`, `request_id`, `prompt`. Notifications are sent asynchronously from a buffered queue (256 events). If the queue is full, events are dropped and a warning is logged. The webhook client has a 10-second timeout per request. Server errors (5xx) and network errors trigger up to 3 retries with exponential backoff (500ms base, 2x factor, 10s cap). Client errors (4xx) are not retried.

## Authentication

| Variable | Type | Default | Description |
|---|---|---|---|
| `API_KEYS` | comma-separated string | none | Static API keys accepted via the `X-Api-Key` header. When set, requests can authenticate with either a valid API key or a JWT bearer token. Leave empty to require JWT bearer tokens only. |
| `USER_AGENT_REGEX` | string (Go regex) | none | Go regex pattern for User-Agent header enforcement. Requests with a non-matching User-Agent are rejected with a `DENY_UA` log action. Leave empty to disable User-Agent checking. Example: `^opencode/.*$` |

## CORS

| Variable | Type | Default | Description |
|---|---|---|---|
| `CORS_ORIGIN` | string | none | Value for the `Access-Control-Allow-Origin` response header. When set, the service responds to OPTIONS preflight requests and adds CORS headers to all responses. Leave empty to disable CORS headers. Example: `https://bulwarkai.cloud` |

When enabled, the service handles OPTIONS requests with a 204 No Content response and sets `Access-Control-Allow-Methods`, `Access-Control-Allow-Headers`, and `Access-Control-Max-Age` headers.

## Circuit Breaker

| Variable | Type | Default | Description |
|---|---|---|---|
| `CB_MAX_FAILURES` | int | `5` | Number of consecutive Vertex AI failures before the circuit breaker opens. |
| `CB_RESET_TIMEOUT` | string (Go duration) | `30s` | Time to wait before transitioning from open to half-open state. |

When the circuit breaker is open, all Vertex AI calls are rejected immediately. This prevents slow timeouts from piling up when Vertex AI is degraded.

## Server

| Variable | Type | Default | Description |
|---|---|---|---|
| `PORT` | string | `8080` | HTTP listen port. |
| `LOG_LEVEL` | string | `info` | Log verbosity. Options: `debug`, `info`, `warn`, `error`. |
| `LOG_PROMPT_MODE` | string | `truncate` | How prompt text appears in log output. Options below. |
| `LOCAL_MODE` | string | unset | Set to `"true"` to skip all authentication and use Application Default Credentials for Vertex AI. Never use in production. |

`LOG_PROMPT_MODE` values:

| Value | Behaviour |
|---|---|
| `truncate` | Logs the first N characters followed by `...`. Default safe option. |
| `hash` | Logs a SHA-256 prefix instead of the prompt text. |
| `full` | Logs the prompt verbatim. Use with caution in production. |
| `none` | Omits prompt text from logs entirely. |

## Inspector Loading

The full pipeline runs in this order:

1. Authentication (domain check, JWT validation, API key)
2. Policy engine (OPA, if enabled): access control based on identity and model
3. Regex inspector: always loaded, cannot be disabled
4. Model Armor inspector: loaded when `MODEL_ARMOR_TEMPLATE` is non-empty and `RESPONSE_MODE` is not `strict`
5. DLP inspector: loaded when `DLP_API` is `"true"`

The inspector chain stops at the first block result.

## Disabling Features

| Feature | How to disable |
|---|---|
| Domain allowlist | Leave `ALLOWED_DOMAINS` empty |
| User-Agent filtering | Leave `USER_AGENT_REGEX` empty |
| Model Armor (fast/audit mode) | Set `MODEL_ARMOR_TEMPLATE` to an empty string |
| DLP | Leave `DLP_API` unset or set to anything other than `"true"` |
| OPA policy engine | Leave `OPA_ENABLED` unset or set to anything other than `"true"` |
| All authentication | Set `LOCAL_MODE=true` (development only) |

Note: Model Armor cannot be disabled in strict mode because it is enforced by Vertex AI's `generateContent` endpoint, not by the Bulwarkai inspector chain.

## Request Size Limit

The service enforces a configurable request body size limit via `MAX_BODY_SIZE` (default 10 MB). Requests exceeding this are rejected with HTTP 413.

## Example Configurations

### Production (strict mode, all inspectors)

```bash
GOOGLE_CLOUD_PROJECT=my-project
ALLOWED_DOMAINS=mycompany.com,subsidiary.com
GOOGLE_CLOUD_LOCATION=europe-west2
RESPONSE_MODE=strict
MODEL_ARMOR_TEMPLATE=production-template
DLP_API=true
LOG_LEVEL=info
LOG_PROMPT_MODE=hash
USER_AGENT_REGEX=^opencode/.*$
```

### Production (fast mode, Model Armor only)

```bash
GOOGLE_CLOUD_PROJECT=my-project
ALLOWED_DOMAINS=mycompany.com
GOOGLE_CLOUD_LOCATION=europe-west2
RESPONSE_MODE=fast
MODEL_ARMOR_TEMPLATE=production-template
LOG_LEVEL=info
LOG_PROMPT_MODE=truncate
```

### Local development

```bash
GOOGLE_CLOUD_PROJECT=my-project
ALLOWED_DOMAINS=
GOOGLE_CLOUD_LOCATION=europe-west2
LOCAL_MODE=true
LOG_LEVEL=debug
LOG_PROMPT_MODE=full
```
