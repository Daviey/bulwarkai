# AGENTS.md

## Commands

```bash
make test          # Go tests with coverage (must pass before committing)
make dev           # Run locally with `go run`
make docs          # Regenerate swagger.yaml from handler annotations (swag init)
make build         # Docker build (scratch image, CGO_ENABLED=0)
make scan          # gosec + govulncheck
```

Run a single Go test:
```bash
go test -run TestFunctionName -v ./...
go test -run TestFunctionName -v ./internal/config/...
```

Terraform tests (OpenTofu, no GCP credentials needed):
```bash
tofu -chdir=terraform init -backend=false
tofu -chdir=terraform validate
tofu -chdir=terraform test
```

## Before committing

`go vet ./...` and `make test` must pass. CI runs: test, vet, gosec, govulncheck, trivy, gitleaks, build (statically linked), docs freshness, terraform validate+test.

## Architecture

Single-binary Go proxy on Cloud Run. All code lives under `internal/`:

- `config/` — env var loading
- `auth/` — JWT extraction, domain allowlist, API key validation
- `inspector/` — `Inspector` interface + chain (`regex.go`, `modelarmor.go`, `dlp.go`). Chain runs concurrently, stops on first block. Fail-open on errors.
- `handler/` — HTTP routes for Anthropic (`/v1/messages`), OpenAI (`/v1/chat/completions`), Vertex AI passthrough
- `translate/` — API format translation (Anthropic/OpenAI <-> Gemini)
- `vertex/` — Vertex AI HTTP client
- `streaming/` — SSE formatting
- `policy/` — Embedded OPA engine (off by default)
- `metrics/` — Prometheus counters

`main.go` is ~109 lines: loads config, wires inspectors into handler, starts HTTP server.

## Code style

- No inline comments. Names must be self-documenting. Package docs go in `doc.go`.
- Inject `*http.Client` for all outbound HTTP. Tests mock with `httptest.NewServer`.
- Fail-open on inspector errors (return nil, log WARN).
- Tests must not make real network calls.

## Terraform

Flat config in `terraform/` (not a module). Key conditionals:

- `dlp_enabled` — gates DLP IAM binding and Cloud Run env vars
- `vpc_sc_enabled` — gates VPC-SC perimeter, access level, org data source
- `api_keys` — gates secret version creation
- `user_agent_regex` — gates Cloud Run env var
- `allowed_iam_members` — gates invoker IAM bindings (count-based)

`access_policy_name` and `org_id` have empty-string defaults so `tofu validate` works without VPC-SC. `terraform.tfvars` is gitignored; use `terraform.tfvars.example` as template.

Tests live in `terraform/tests/*.tftest.hcl` using `mock_provider "google"` — no credentials required.

## Gotchas

- Swagger (`docs/swagger.yaml`) is generated from handler annotations. If you change handler signatures, run `make docs` or CI will fail on docs freshness.
- Go module is `github.com/Daviey/bulwarkai` (note capital D).
- Docker image is `scratch` — no OS packages, no shell. Binary only + TLS certs.
- `DEMO_MODE=true` returns canned responses for zero-cost testing (no Vertex AI calls).
- `LOCAL_MODE=true` skips auth, uses ADC. For local dev only.
- Region defaults to `europe-west2`. Cloud Build is not used because it creates US resources regardless of config.
- `shell.nix` provides all dev tools (Go, tofu, swag, gosec, etc.).
