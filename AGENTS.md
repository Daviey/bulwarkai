# AGENTS.md

## Project

Bulwarkai is an AI safety proxy that screens all traffic between client applications and Google Vertex AI for sensitive data and content policy violations. Written in Go 1.25, deployed to Cloud Run.

**Repo**: `github.com/Daviey/bulwarkai` (public)
**Module path**: `github.com/Daviey/bulwarkai`

## Commands

```bash
make dev          # run locally
make test         # run tests with coverage
make test-html    # tests with HTML coverage report
make build        # docker build
make push         # push to Artifact Registry
make sast         # run gosec
make scan         # gosec + govulncheck
make docs         # regenerate OpenAPI spec from annotations
make health       # curl localhost:8080/health
```

Go version: **1.25**. Use `nix-shell -p go_1_25` when needing Go tooling locally.

## CI

All PRs must pass these checks (see `.github/workflows/ci.yaml`):

- **Test**: `go test -count=1 -coverprofile=coverage.out ./...`
- **Vet**: `go vet ./...`
- **SAST (gosec)**: `gosec -exclude G101,G304,G704,G705 ./...` (continue-on-error, uploads SARIF)
- **Vulnerability Check**: `govulncheck ./...`
- **Container Scan (trivy)**: filesystem scan
- **Secret Scan**: gitleaks
- **Build**: static binary, verifies `file` output contains "statically linked"
- **Docs Freshness**: regenerates `docs/swagger.yaml` via `swag init` and diffs for drift

Gosec rules G101, G304, G704, G705 are excluded as false positives for this codebase. The `@latest` version of gosec ignores `//nosec` annotations, so exclusions are via `-exclude` flag only.

## Branch and merge rules

- `main` is branch-protected: requires CI pass, linear history, squash merge
- All changes via PRs. No direct pushes to `main`.
- Single squashed commit per PR. Use `git commit --amend --no-edit && git push --force` for changes on feature branches.
- No approval required (set to 0). `--admin` flag available if needed.
- Always `git checkout main && git pull origin main && git reset --hard origin/main` before creating new branches.

## Architecture

```
main.go                          # entry point, wiring
internal/
  auth/auth.go                   # JWT/API key authentication, domain checks
  circuitbreaker/circuitbreaker.go # three-state circuit breaker for Vertex AI
  config/config.go               # env var loading
  handler/
    handler.go                   # Server, Routes, middleware, checkPolicy, CORS
    mode.go                      # logAction, writeJSON, parseBody, webhook notify
    vertex.go                    # Vertex AI native format passthrough
    openai.go                    # OpenAI Chat Completions handler
    anthropic.go                  # Anthropic Messages API handler
  inspector/
    inspector.go                 # Chain, concurrent screening, BlockResult
    regex.go                     # regex patterns (SSN, CC, keys, credentials)
    modelarmor.go                # Model Armor standalone API
    dlp.go                       # Cloud DLP content:inspect
  metrics/metrics.go             # Prometheus counters and histograms
  policy/engine.go               # OPA policy engine (rego package, hot-reload)
  ratelimit/ratelimit.go         # fixed-window per-email rate limiter
  streaming/streaming.go         # SSE helpers for Anthropic/OpenAI formats
  translate/translate.go         # format translation between API formats
  vertex/client.go               # Vertex AI HTTP client (with circuit breaker)
  vertex/demo.go                 # DemoClient (canned responses)
  webhook/webhook.go             # async block event notifications (retry with backoff)
```

## Key design decisions

- **Cloud-agnostic**: standard library where possible, zero vendor lock-in
- **Modular inspector architecture**: `Inspector` interface with pluggable backends
- **Fail-open on inspector errors**: network failures in Model Armor/DLP do not block traffic. Logged at ERROR level. Alert on `bulwarkai_inspector_results_total{result="error"}`.
- **Fail-open on policy errors**: OPA evaluation errors allow the request through.
- **User tokens all the way through**: `X-Forwarded-Access-Token` forwarded to Vertex AI for audit identity
- **Request pipeline order**: auth, then rate limit, then OPA policy, then inspector chain, then Vertex AI
- **OPA hot-reload**: file polled every 5s, HTTP URL every 30s. Bad recompilations preserve old policy.
- **No org-specific values in git**: project names, domains, emails only in `.env`
- **No SVGs**: all icons must be PNGs
- **Brand**: Bulwarkai (not BulwarkAI)
- **No em dashes** in prose

## Environment variables

See `.env.example` for the full list. Key ones:

| Variable | Purpose |
|---|---|
| `GOOGLE_CLOUD_PROJECT` | GCP project ID |
| `ALLOWED_DOMAINS` | comma-separated email domain allowlist |
| `RESPONSE_MODE` | `strict`, `fast`, or `audit` |
| `OPA_ENABLED` | `true` to enable OPA policy engine |
| `OPA_POLICY_FILE` | path to Rego file (hot-reloaded every 5s) |
| `OPA_POLICY_URL` | HTTP URL or inline Rego (URL polled every 30s) |
| `RATE_LIMIT` | per-email request limit per window (0 = disabled) |
| `WEBHOOK_URL` | URL for block event notifications |
| `LOCAL_MODE` | `true` to skip auth (dev only) |
| `DEMO_MODE` | `true` for canned responses, no Vertex AI calls |

## Writing style

- Follow the `avoid-ai-tropes` skill: no em dashes, no "delve/leverage/robust/landscape", no bold-first bullets, no tricolon escalation, no negative parallelism
- **NO EM DASHES AT ALL** in any file
- No comments in Go code unless explicitly asked

## Code style

- `CGO_ENABLED=0` static binary, `scratch` Docker image
- `slog` for structured JSON logging
- Prometheus metrics via `promauto`
- OpenAPI spec generated from code annotations via `swaggo/swag`
- `handler.NewServer(cfg, chain, caller, httpClient, policyEngine, webhookNotifier, rateLimiter)` (7 args, order matters)
- `policy.Engine.Evaluate()` returns `*Decision` only (errors handled internally, fail-open)
- `policy.NewEngineWithHTTP(ctx, enabled, policyFile, policyURL, httpClient)` is the full constructor. `NewEngine` wraps it with `nil` httpClient.
- Circuit breaker wraps Vertex AI client calls (5 failures / 30s timeout, hardcoded). Open state returns 503.
- Webhook notifier retries failed deliveries with exponential backoff (up to 3 attempts).
- CORS middleware enabled via `CORS_ORIGIN` env var.
- No vendor-specific SDKs in the hot path. The Vertex AI client uses plain `net/http`.

## Terraform

Located in `terraform/`. Region: `europe-west2`. All resources EU-only due to org policy.

Key resources:
- `cloud_run.tf`: Cloud Run v2 service with Direct VPC Egress, CMEK, Binary Authorization
- `opa_policy.tf`: GCS config bucket for OPA policy storage
- `monitoring.tf`: alert policies (inspector fail-open, high deny rate, latency) + dashboard
- `model_armor.tf`: Model Armor template with `ignore_partial_invocation_failures = false`
- `vpc_sc.tf`: VPC Service Controls perimeter

Cloud Build is blocked (creates US resources). Use local `docker build` + push to Artifact Registry.

## Docs

- `docs/` is the source of truth
- `scripts/sync-docs.sh` generates `site/content/docs/` from `docs/` with Zola front matter
- Site uses Zola 0.22.1, syntax highlighting `github-dark`
- Design: warm parchment cream (`#F5F0E8`), gold accents (`#C9A84C`), castle/medieval theme
- Hero strapline: "Your AI castle has no moat."

## Known issues

- Model Armor does NOT enforce on streaming endpoints (`streamGenerateContent`). Google is aware.
- Model Armor does NOT work with third-party models (Anthropic, Llama). This is why Bulwarkai exists.
- `gosec @latest` installs a dev version that ignores `//nosec` annotations. Workaround: `-exclude` flag.
- Cloud Build blocked by EU-only org policy.
- OPA hot-reload file tests can flake under `go test ./...` due to CPU contention with parallel test packages. They pass reliably when run individually (`go test ./internal/policy/`).
