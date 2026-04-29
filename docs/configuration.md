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

## Authentication

| Variable | Type | Default | Description |
|---|---|---|---|
| `API_KEYS` | comma-separated string | none | Static API keys accepted via the `X-Api-Key` header. When set, requests can authenticate with either a valid API key or a JWT bearer token. Leave empty to require JWT bearer tokens only. |
| `USER_AGENT_REGEX` | string (Go regex) | none | Go regex pattern for User-Agent header enforcement. Requests with a non-matching User-Agent are rejected with a `DENY_UA` log action. Leave empty to disable User-Agent checking. Example: `^opencode/.*$` |

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

Inspectors are loaded at startup based on configuration. The chain runs in this order:

1. Regex inspector: always loaded, cannot be disabled.
2. Model Armor inspector: loaded when `MODEL_ARMOR_TEMPLATE` is non-empty and `RESPONSE_MODE` is not `strict`.
3. DLP inspector: loaded when `DLP_API` is `"true"`.

The chain stops at the first inspector that returns a block result.

## Disabling Features

| Feature | How to disable |
|---|---|
| Domain allowlist | Leave `ALLOWED_DOMAINS` empty |
| User-Agent filtering | Leave `USER_AGENT_REGEX` empty |
| Model Armor (fast/audit mode) | Set `MODEL_ARMOR_TEMPLATE` to an empty string |
| DLP | Leave `DLP_API` unset or set to anything other than `"true"` |
| All authentication | Set `LOCAL_MODE=true` (development only) |

Note: Model Armor cannot be disabled in strict mode because it is enforced by Vertex AI's `generateContent` endpoint, not by the Bulwarkai inspector chain.

## Request Size Limit

The service enforces a 10 MB request body limit. Requests exceeding this are rejected with HTTP 413.

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
