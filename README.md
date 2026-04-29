# Bulwarkai

[![CI](https://github.com/Daviey/bulwarkai/actions/workflows/ci.yaml/badge.svg)](https://github.com/Daviey/bulwarkai/actions/workflows/ci.yaml)
[![Go Reference](https://pkg.go.dev/badge/github.com/Daviey/bulwarkai.svg)](https://pkg.go.dev/github.com/Daviey/bulwarkai)
[![Go Report Card](https://goreportcard.com/badge/github.com/Daviey/bulwarkai)](https://goreportcard.com/report/github.com/Daviey/bulwarkai)
[![Coverage](https://img.shields.io/badge/coverage-91.5%25-brightgreen)](https://github.com/Daviey/bulwarkai/actions/workflows/ci.yaml)
[![govulncheck](https://img.shields.io/badge/govulncheck-passing-brightgreen)](https://github.com/Daviey/bulwarkai/actions/workflows/ci.yaml)
[![gosec](https://img.shields.io/badge/gosec-SAST-blue)](https://github.com/Daviey/bulwarkai/security/code-scanning?tool=gosec)
[![trivy](https://img.shields.io/badge/trivy-container%20scan-blue)](https://github.com/Daviey/bulwarkai/security/code-scanning?tool=trivy)
[![gitleaks](https://img.shields.io/badge/gitleaks-secret%20scan-blue)](https://github.com/Daviey/bulwarkai/security/secret-scanning)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

<p align="center"><img src="https://bulwarkai.cloud/logo-512.png" width="200" alt="Bulwarkai"></p>

An AI safety proxy that screens every request and response between client applications and Google Vertex AI. Written in Go, deployed on Cloud Run.

**[bulwarkai.cloud](https://bulwarkai.cloud)** -- landing page, documentation, and API reference.

## Why This Exists

This project should not need to exist. The day Google ships built-in prompt/response screening as a first-class Vertex AI feature, this repository gets archived.

Google is catching up. [Agent Gateway](https://docs.cloud.google.com/gemini-enterprise-agent-platform/govern/gateways/agent-gateway-overview) (Private Preview) is the closest thing to Bulwarkai in Google's portfolio. It routes agent traffic through a governed gateway with Model Armor integration. But it is limited to the Gemini Enterprise Agent Platform; it does not proxy general Vertex AI traffic from arbitrary clients like opencode or Claude Code, does not translate API formats, and does not add structured data screening. The table below shows where each gap stands.

Without a network-level proxy, screening is a policy rather than a control. Library-based screening requires every client to integrate correctly; a single misconfigured client bypasses all of it. Bulwarkai enforces at the infrastructure layer. Clients cannot opt out.

Bulwarkai fills the gaps that remain today:

| Gap | Status without Bulwarkai | With Bulwarkai |
|---|---|---|
| Streaming content screening | :x: Model Armor does not enforce on `streamGenerateContent` | :white_check_mark: via [standalone Model Armor API](https://docs.cloud.google.com/model-armor/sanitize-prompts-responses) |
| Structured data detection (SSN, keys) | :x: Model Armor [only covers RAI categories](https://docs.cloud.google.com/vertex-ai/generative-ai/docs/multimodal/configure-safety-filters) | :white_check_mark: regex + DLP |
| Per-user audit identity | :x: service account only | :white_check_mark: forwarded user tokens |
| Model Armor only works on Gemini | :x: [only `generateContent` on Gemini models](https://docs.cloud.google.com/model-armor/model-armor-vertex-integration); standalone API does not cover third-party | :white_check_mark: regex + DLP work on any model |
| No cross-API support | :x: Vertex AI speaks Gemini format only | :white_check_mark: Anthropic, OpenAI, Gemini |
| Client-to-agent proxying | :large_orange_diamond: [Agent Gateway (Preview)](https://docs.cloud.google.com/gemini-enterprise-agent-platform/govern/gateways/agent-gateway-overview) -- Agent Platform only | :white_check_mark: any Vertex AI client |

<details>
<summary>What it protects against</summary>

**Structured data leakage.** A developer pastes a customer record with an SSN. A private key from `.env` gets copied as context. An API key leaks into a code review the model is asked to summarise. The regex inspector catches known patterns in microseconds. The DLP inspector catches statistical matches ("this looks like a phone number") with configurable thresholds.

**Content policy violations.** Harmful content, prompt injection, jailbreaks, malicious URLs. The Model Armor inspector handles these using Google's content safety models. In strict mode, Vertex AI's built-in enforcement provides a second independent layer.

**Credential exposure.** AWS access keys, `sk-` prefixed API keys, email-and-password pairs in the same text. Detected before data leaves the network.

</details>

<details>
<summary>Why user tokens pass through</summary>

The service forwards the user's OAuth access token instead of using a service account. This means Vertex AI audit logs show which human made each request. If three developers route through the same proxy, the audit trail distinguishes between them.

The two-token design exists because Cloud Run IAM requires an OIDC identity token for invocation, while Vertex AI requires an OAuth access token. A single OAuth request with scope `cloud-platform openid email` returns both.

</details>

## What Bulwarkai adds vs native Vertex AI

This table shows how Bulwarkai combines multiple Google services into a single enforcement layer. Model Armor covers Gemini non-streaming traffic, but skips streaming, third-party models, and structured data detection. Bulwarkai fills those gaps with regex, DLP, and the standalone Model Armor API where it works.

Vertex AI's built-in safety settings vary by model and endpoint. What Gemini blocks, Llama might allow. What `generateContent` catches, `streamGenerateContent` skips. There is no single audit trail that covers every request with the same rules, making it difficult to measure compliance consistently. Bulwarkai applies the same inspector chain to every request regardless of model, format, or streaming mode, and logs every screening decision in a structured format.

| Capability | Vertex AI | + Model Armor | + Bulwarkai |
|---|:---:|:---:|:---:|
| Prompt screening for structured data (SSN, credit cards, private keys) | :x: | :x: RAI categories only | :white_check_mark: regex + DLP + Model Armor |
| Response screening for structured data | :x: | :x: RAI categories only | :white_check_mark: regex + DLP + Model Armor |
| Streaming content screening (`streamGenerateContent`) | :x: | :x: [not enforced on streaming](https://docs.cloud.google.com/model-armor/model-armor-vertex-integration) | :white_check_mark: via standalone API |
| Content safety on third-party models (Anthropic, Llama) | :x: | :x: [Model Armor does not work with third-party models](https://docs.cloud.google.com/model-armor/model-armor-vertex-integration) | :white_check_mark: regex + DLP inspectors |
| Consistent safety enforcement across all models | :x: varies by model | :x: Gemini-only inline, skips streaming | :white_check_mark: same chain for every model |
| Unified audit trail for compliance | :x: fragmented by model/endpoint | :x: no standalone audit log | :white_check_mark: structured log for every request |
| Content safety on Gemini `generateContent` | :white_check_mark: | :white_check_mark: inline integration | :white_check_mark: |
| Per-user audit identity in Vertex AI logs | :x: service account | :x: same | :white_check_mark: forwarded user tokens |
| User-Agent filtering | :x: | :x: | :white_check_mark: |
| Email domain allowlist | :x: | :x: | :white_check_mark: |
| API key authentication | :x: | :x: | :white_check_mark: |
| Prompt redaction in logs | :x: | :x: | :white_check_mark: |
| Post-response audit (audit mode) | :x: | :x: | :white_check_mark: |
| Pluggable inspector chain | :x: | :x: | :white_check_mark: |
| Cross-API format support (Anthropic, OpenAI, Gemini) | :x: | :x: | :white_check_mark: |
| Works with opencode | :x: | :x: | :white_check_mark: |
| Works with Claude Code | :x: | :x: | :white_check_mark: |
| Works with any OpenAI-compatible tool | :x: | :x: | :white_check_mark: |
| Works with curl / Gemini native | :white_check_mark: | :white_check_mark: | :white_check_mark: |
| First-party Gemini models | :white_check_mark: | :white_check_mark: | :white_check_mark: |
| Third-party models (Anthropic, Llama, etc.) | :white_check_mark: | :x: | :white_check_mark: |
| Streaming support for code tools | :white_check_mark: | :white_check_mark: | :white_check_mark: |
| Agent-to-agent traffic governance | :x: | :large_orange_diamond: [Agent Gateway (Preview)](https://docs.cloud.google.com/gemini-enterprise-agent-platform/govern/gateways/agent-gateway-overview) -- Agent Platform only | :x: (not in scope) |
| Fail-closed when screening unavailable | :x: | :x: ["skips sanitization and continues"](https://docs.cloud.google.com/model-armor/model-armor-vertex-integration) | :white_check_mark: strict mode blocks on error |

Legend: :x: not available &nbsp; :large_orange_diamond: preview / partial &nbsp; :white_check_mark: available

## Model Armor current state

| Feature | Status | Detail |
|---|:---:|---|
| Gemini `generateContent` screening | :white_check_mark: | [Inline integration](https://docs.cloud.google.com/model-armor/model-armor-vertex-integration) |
| Gemini `streamGenerateContent` screening | :x: | Not enforced. Google is aware. |
| Third-party model screening (Anthropic, Llama) | :x: | Inline integration is Gemini-only |
| Structured data detection (SSN, keys, credentials) | :x: | Only RAI categories (hate, harassment, sexually explicit, dangerous) |
| Fail-open when unavailable | :x: (a gap) | ["Skips sanitization and continues processing"](https://docs.cloud.google.com/model-armor/model-armor-vertex-integration) |
| Standalone `sanitizeUserPrompt` / `sanitizeModelResponse` API | :white_check_mark: | Gemini only in practice. Bulwarkai uses regex + DLP for third-party models. |
| Agent Gateway integration (Preview) | :large_orange_diamond: | [Routes agent traffic through Model Armor](https://docs.cloud.google.com/gemini-enterprise-agent-platform/govern/gateways/agent-gateway-overview), but Agent Platform only |

## Response modes

| | strict | fast (alias: input_only) | audit (alias: buffer) |
|---|:---:|:---:|:---:|
| Prompt screened | :white_check_mark: | :white_check_mark: | :white_check_mark: |
| Response screened | :white_check_mark: | :x: | audit only |
| Streaming | :x: fake (single chunk) | :white_check_mark: real | :white_check_mark: real |
| Model Armor platform enforcement | :white_check_mark: (via `generateContent`) | :x: (streaming bypass) | :x: (streaming bypass) |
| Added latency | ~500ms (prompt + response) | ~200ms (prompt only) | ~200ms (prompt only) |
| Use case | Maximum safety | Lowest latency | Audit trail |
| Gemini models | :white_check_mark: | :white_check_mark: | :white_check_mark: |
| Anthropic models (if enabled in Vertex AI) | :white_check_mark: | :white_check_mark: | :white_check_mark: |

## Deployment impact on controls

| Control | Cloud Run (production) | Local (LOCAL_MODE) | Behind VPC-SC |
|---|:---:|:---:|:---:|
| Authentication required | :white_check_mark: OIDC or API key | :x: skipped | :white_check_mark: OIDC or API key |
| Domain allowlist | :white_check_mark: enforced | :x: no email to check | :white_check_mark: enforced |
| User-Agent filter | :white_check_mark: enforced | :white_check_mark: enforced | :white_check_mark: enforced |
| Vertex AI uses user token | :white_check_mark: forwarded | :white_check_mark: ADC | :white_check_mark: forwarded |
| Inspector chain active | :white_check_mark: | :white_check_mark: | :white_check_mark: |
| Structured audit logs | :white_check_mark: Cloud Logging | :white_check_mark: stdout | :white_check_mark: Cloud Logging |
| Cannot bypass proxy | :x: users can call Vertex AI directly | :x: same machine has ADC creds | :white_check_mark: perimeter blocks direct access |

## Client support

Bulwarkai translates three API formats so existing tools work without modification:

| Client | Format | Native Vertex AI | With Bulwarkai |
|---|---|:---:|:---:|
| opencode | OpenAI Chat Completions | :x: | :white_check_mark: |
| Claude Code | Anthropic Messages | :x: | :white_check_mark: |
| curl / SDK | Gemini native | :white_check_mark: | :white_check_mark: |
| Any OpenAI-compatible tool | OpenAI Chat Completions | :x: | :white_check_mark: |

## Quick Start

### Local bridge (no Cloud Run needed)

Run on your laptop as a persistent safety proxy. All prompts and responses from your AI tools get screened before reaching Vertex AI.

```bash
cp .env.example .env
# set GOOGLE_CLOUD_PROJECT, LOCAL_MODE=true, RESPONSE_MODE=fast
make dev
```

Point your tools at `http://localhost:8080`:

| Tool | Config |
|---|---|
| opencode | `floorServiceUrl: http://localhost:8080` |
| Claude Code | `ANTHROPIC_BASE_URL=http://localhost:8080` |
| curl | `http://localhost:8080/v1/chat/completions` |

Uses your `gcloud` ADC credentials. No OIDC tokens, no IAM, no deployment. All inspectors run normally.

### Cloud Run deployment

```bash
make dev
```

See [docs/deployment.md](docs/deployment.md) for Docker, Terraform, and production setup.

## Testing with EICAR-style strings

Bulwarkai provides safe test strings that trigger each inspector without using real sensitive data. Inspired by the [EICAR test file](https://en.wikipedia.org/wiki/EICAR_test_file) used to verify antivirus software.

```bash
curl https://bulwarkai-XXXXX.run.app/test-strings
```

Returns:

```json
{
  "ssn": "BULWARKAI-TEST-SSN-000-00-0000",
  "credit_card": "BULWARKAI-TEST-CC-0000000000000000",
  "private_key": "BULWARKAI-TEST-KEY-BEGIN RSA PRIVATE KEY-END",
  "aws_key": "BULWARKAI-TEST-AWS-AKIA0000000000000000",
  "api_key": "BULWARKAI-TEST-API-sk-00000000000000000000",
  "credentials": "BULWARKAI-TEST-CRED-test@example.com password"
}
```

Send any of these as a prompt to verify the proxy blocks them:

```bash
curl -X POST https://bulwarkai-XXXXX.run.app/v1/chat/completions \
  -H "X-Api-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"gemini-2.5-flash","max_tokens":1024,"messages":[{"role":"user","content":"BULWARKAI-TEST-SSN-000-00-0000"}]}'
```

All test strings are prefixed with `BULWARKAI-TEST` so they are clearly identifiable in logs.

## Documentation

| Document | Contents |
|---|---|
| [docs/configuration.md](docs/configuration.md) | Every environment variable, defaults, how to disable each feature |
| [docs/swagger.yaml](docs/swagger.yaml) | OpenAPI spec (generated from handler annotations) |
| [docs/deployment.md](docs/deployment.md) | Deployment, local dev, LOCAL_MODE, Terraform, Docker |
| [docs/design.md](docs/design.md) | Architecture (Mermaid diagrams), config, security approach |
| [docs/inspectors.md](docs/inspectors.md) | Inspector interface, adding new ones, testing |
| [docs/operations.md](docs/operations.md) | Monitoring, alerting, scaling, troubleshooting |
| [docs/client-config.md](docs/client-config.md) | opencode plugin, Claude Code, curl examples, geographic restrictions |
| [docs/adr/](docs/adr/) | Architecture Decision Records (8 ADRs) |
| [CONTRIBUTING.md](CONTRIBUTING.md) | How to contribute, code style, project structure |

<details>
<summary>GCP Prerequisites</summary>

### APIs to enable

```bash
gcloud services enable run.googleapis.com \
  artifactregistry.googleapis.com \
  aiplatform.googleapis.com \
  modelarmor.googleapis.com \
  dlp.googleapis.com \
  --project=YOUR_PROJECT_ID
```

### IAM roles

Service account needs: `roles/aiplatform.user`, `roles/modelarmor.user`, `roles/dlp.reader` (if DLP enabled), `roles/run.invoker`.

Users need: `roles/run.invoker` on the Cloud Run service.

### Model Armor template

```bash
gcloud model-armor templates create test-template \
  --project=YOUR_PROJECT_ID \
  --location=europe-west2 \
  --rai-settings-filters='[
    {"filterType":"HATE_SPEECH","confidenceLevel":"HIGH"},
    {"filterType":"DANGEROUS","confidenceLevel":"MEDIUM_AND_ABOVE"},
    {"filterType":"HARASSMENT","confidenceLevel":"HIGH"},
    {"filterType":"SEXUALLY_EXPLICIT","confidenceLevel":"HIGH"}
  ]' \
  --pi-and-jailbreak-filter-settings-enforcement=enabled \
  --malicious-uri-filter-settings-enforcement=enabled
```

Or provision via `terraform/model_armor.tf`.

### Org policies

`constraints/gcp.resourceLocations` may restrict resources to EU regions. All resources default to `europe-west2`. Cloud Build is not used because it creates US-based temporary resources.

`constraints/run.allowedIngress` and restrictions on `allUsers`/`allAuthenticatedUsers` may require IAM principal-based authentication.

</details>
