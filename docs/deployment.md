# Deployment

Built with a multi-stage Dockerfile (`golang:1.25-alpine` for build, `scratch` for runtime). Pushed to Artifact Registry and deployed to Cloud Run. Cloud Build is not used because it creates resources in the US, which may conflict with org policies restricting resources to EU regions. The `cloudbuild.googleapis.com` API is enabled in Terraform because Artifact Registry depends on it for image operations, even though Cloud Build itself is not used for CI/CD.

```bash
docker build -t europe-west2-docker.pkg.dev/YOUR_PROJECT_ID/bulwarkai/bulwarkai .
docker push europe-west2-docker.pkg.dev/YOUR_PROJECT_ID/bulwarkai/bulwarkai
gcloud run deploy bulwarkai --image europe-west2-docker.pkg.dev/YOUR_PROJECT_ID/bulwarkai/bulwarkai --region europe-west2
```

## Local Development

The service runs locally using your `gcloud` ADC credentials. No Cloud Run needed for development.

### Prerequisites

You need `go` (1.25+), `gcloud` CLI, and authenticated ADC:

```bash
gcloud auth application-default login
gcloud auth application-default set-quota-project YOUR_PROJECT_ID
```

### With nix-shell

A `shell.nix` provides go, docker, gcloud, terraform, curl, and jq:

```bash
nix-shell shell.nix
```

### Quick start

```bash
cp .env.example .env
# edit .env if needed (defaults work with ADC)
make dev
```

This starts the service on `http://localhost:8080`. The env vars in `.env` configure it. `LOG_LEVEL=debug` gives verbose output.

### Running tests

```bash
make test
```

Or for an HTML coverage report:

```bash
make test-html
open coverage.html
```

### Docker local

To run the exact container image locally:

```bash
make run-local
```

Builds the Docker image, starts it on port 8080 with env vars from `.env`. Ctrl+C stops it.

### Testing endpoints

With `LOCAL_MODE=true`, you don't need real tokens:

```bash
# health check
make health

# OpenAI format (no auth headers needed in LOCAL_MODE)
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gemini-2.5-flash","max_tokens":1024,"messages":[{"role":"user","content":"Hello"}]}'

# Test SSN blocking
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gemini-2.5-flash","max_tokens":1024,"messages":[{"role":"user","content":"My SSN is 123-45-6789"}]}'
```

Without `LOCAL_MODE`, pass real tokens:

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $(gcloud auth print-identity-token)" \
  -H "X-Forwarded-Access-Token: $(gcloud auth print-access-token)" \
  -H "Content-Type: application/json" \
  -d '{"model":"gemini-2.5-flash","max_tokens":1024,"messages":[{"role":"user","content":"Hello"}]}'
```

### LOCAL_MODE

Set `LOCAL_MODE=true` in `.env` to skip all authentication and use your `gcloud` ADC credentials for Vertex AI calls. The service treats every request as coming from `local@localhost`, ignores `Authorization` and `X-Forwarded-Access-Token` headers, and obtains its own access token from Application Default Credentials to call Vertex AI.

This exists for development and testing on a laptop. Do not enable it in production.

What LOCAL_MODE changes:

| Behaviour | Cloud Run | LOCAL_MODE |
|---|---|---|
| Authentication | OIDC token or API key required | Skipped, identity is `local@localhost` |
| Vertex AI token | Forwarded from `X-Forwarded-Access-Token` | Obtained from ADC (`gcloud auth application-default`) |
| Domain allowlist | Enforced | Not enforced (no email to check) |
| Inspectors | Run as normal | Run as normal |

### Local client configuration

When Bulwarkai runs on localhost with `LOCAL_MODE=true`, configure clients to point at `http://localhost:8080`. The service ignores auth headers, so clients can send whatever they want (or nothing).

**opencode** (`~/.config/opencode/opencode.json`):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "model": "google-vertex/gemini-2.5-flash",
  "small_model": "google-vertex/gemini-2.5-flash",
  "plugin": [
    ["file:///path/to/opencode-plugin-model-armor.ts", {
      "provider": "google-vertex",
      "floorServiceUrl": "http://localhost:8080",
      "location": "europe-west2",
      "project": "your-project-id"
    }]
  ]
}
```

The plugin patches `globalThis.fetch` to inject auth headers on requests to the Bulwarkai URL. In LOCAL_MODE the service ignores these headers, but the plugin still needs to be loaded so that opencode uses the Bulwarkai URL instead of calling Vertex AI directly.

**Claude Code**:

```bash
ANTHROPIC_BASE_URL="http://localhost:8080" \
ANTHROPIC_API_KEY="any-value" \
  npx @anthropic-ai/claude-code
```

The `ANTHROPIC_API_KEY` can be any non-empty string. Bulwarkai ignores it in LOCAL_MODE. If you have an API key configured in `API_KEYS`, use that value instead.

**curl** (simplest test):

```bash
curl -s http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gemini-2.5-flash","max_tokens":1024,"messages":[{"role":"user","content":"What is 2+2?"}]}'
```

### Local vs Cloud Run differences

Running locally with `LOCAL_MODE=true`, the service uses your personal ADC credentials for Vertex AI. There is no Cloud Run IAM check, no OIDC token validation, and no Cloud Trace context. The domain allowlist is not enforced because there is no email to check. Structured logs still go to stdout, and all inspectors run as normal.

Without `LOCAL_MODE`, local runs still expect real OIDC and access tokens in the request headers. This is useful for testing the full auth flow before deploying.

## Terraform

Infrastructure is defined in the `terraform/` directory. It creates all the GCP resources the service needs.

### What it provisions

| Resource | Purpose |
|---|---|
| `google_service_account.bulwarkai` | Service account for Cloud Run to call Vertex AI, Model Armor, DLP |
| `google_artifact_registry_repository` | Docker image repository in the deploy region |
| `google_model_armor_template.floor` | Model Armor template with RAI filters, prompt injection, malicious URI detection |
| `google_cloud_run_v2_service.bulwarkai` | The Bulwarkai, ingress restricted to internal + CLB, VPC connector for outbound, secrets from Secret Manager |
| `google_compute_network` / `google_compute_subnetwork` | Dedicated VPC for the service |
| Direct VPC Egress | Cloud Run routes all outbound traffic through the VPC subnet directly, no connector needed |
| `google_compute_global_address` / `google_service_networking_connection` | Private Service Access for Vertex AI (private endpoints, no public internet) |
| `google_secret_manager_secret` | API keys stored in Secret Manager, not env vars |
| `google_kms_key_ring` / `google_kms_crypto_key` | CMEK encryption for Artifact Registry |
| `google_artifact_registry_repository` (x2) | Separate prod and non-prod container registries |
| `google_binary_authorization_policy` | Only images from the project registry can deploy |
| `google_access_context_manager_service_perimeter` | VPC-SC perimeter restricting Vertex AI, Model Armor, DLP to this project only |
| IAM bindings | Custom `vertexAIBulwarkaiInvoker` role (predict + streamPredict only), `modelarmor.user`, `secretmanager.secretAccessor` on the service account; `run.invoker` for specified members; `compute.networkUser` for Cloud Run VPC agent |

### API enablement

The Terraform also enables required APIs: `run.googleapis.com`, `artifactregistry.googleapis.com`, `aiplatform.googleapis.com`, `modelarmor.googleapis.com`, `dlp.googleapis.com`, `secretmanager.googleapis.com`, `vpcaccess.googleapis.com`, `compute.googleapis.com`, `cloudkms.googleapis.com`, `binaryauthorization.googleapis.com`, `containeranalysis.googleapis.com`, `accesscontextmanager.googleapis.com`, `servicenetworking.googleapis.com`.

### Setup

1. Create the Terraform state bucket:

```bash
gsutil mb -l europe-west2 gs://bulwarkai-tfstate
```

2. Copy and edit the variables file:

```bash
cd terraform
cp terraform.tfvars.example terraform.tfvars
# edit terraform.tfvars with your values
```

3. Initialise and apply:

```bash
terraform init
terraform plan
terraform apply
```

4. Build and deploy the image:

```bash
make build push
```

The Cloud Run service is configured to pull from the Artifact Registry repo that Terraform created. The first `terraform apply` creates the service with the `latest` tag. Subsequent deploys push a new image and Cloud Run picks it up on the next request (or use `gcloud run services update-traffic` for explicit revision promotion).

### Variables

| Variable | Default | Description |
|---|---|---|
| `project_id` | `YOUR_PROJECT_ID` | GCP project |
| `region` | `europe-west2` | All resources in this region |
| `allowed_domains` | (none) | Email domain allowlist |
| `response_mode` | `strict` | Screening mode |
| `fallback_model` | `gemini-2.5-flash` | Default model |
| `model_armor_template` | `test-template` | Model Armor template ID |
| `api_keys` | `""` | Comma-separated API keys |
| `user_agent_regex` | `""` | User-Agent regex |
| `dlp_enabled` | `false` | Enable DLP inspector |
| `dlp_info_types` | `US_SOCIAL_SECURITY_NUMBER,...` | DLP info types |
| `allowed_iam_members` | `[]` | IAM members who can invoke the service |

### Outputs

`service_url`: the Cloud Run service URL (e.g. `https://bulwarkai-XXXXX.run.app`)

## Rollback

Cloud Run keeps revisions. To roll back to the previous revision:

```bash
gcloud run services update-traffic bulwarkai \
  --to-revisions bulwarkai-00001-xxx=100 \
  --region europe-west2
```

List available revisions:

```bash
gcloud run revisions list --service bulwarkai --region europe-west2
```

Split traffic between revisions for canary testing:

```bash
gcloud run services update-traffic bulwarkai \
  --to-revisions bulwarkai-00001-xxx=90,bulwarkai-00002-yyy=10 \
  --region europe-west2
```

## Secrets

API keys are stored in Google Secret Manager and mounted as environment variables at runtime via the Terraform configuration. The `secrets.tf` file creates the secret and grants the service account `roles/secretmanager.secretAccessor`.

To set the initial value:

```bash
echo -n "key1,key2" | gcloud secrets versions add bulwarkai-api-keys --data-file=-
```

To rotate, create a new version:

```bash
echo -n "new-key1,new-key2" | gcloud secrets versions add bulwarkai-api-keys --data-file=-
```

The Cloud Run service references `latest` so the new value takes effect on the next request without a redeploy.
