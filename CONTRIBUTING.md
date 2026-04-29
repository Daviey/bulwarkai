# Contributing to Bulwarkai

## Prerequisites

Go 1.24+, `gcloud` CLI, authenticated ADC. A `shell.nix` is provided with all tools.

```bash
nix-shell shell.nix
```

## Development workflow

1. Create a feature branch from `main`
2. Make changes
3. Run `make test` -- all tests must pass
4. Run `go vet ./...` and fix any warnings
5. Open a pull request

## Running tests

```bash
make test
```

For coverage:

```bash
go test -count=1 -coverprofile=coverage.out -coverpkg=./internal/... ./...
go tool cover -func=coverage.out
```

All external dependencies are mocked with `httptest.NewServer`. Tests must not make real network calls.

## Code style

- No comments in code. Function and type names should be self-documenting. Package-level documentation goes in `doc.go` files.
- Exported functions get godoc in the `doc.go` file, not inline.
- Follow existing patterns in the codebase. Look at how neighbouring code works before writing new code.
- Fail-open on inspector errors. If a remote backend is down, return nil (pass), log at WARN level.
- Inject `*http.Client` for any outbound HTTP. Tests mock it with `httptest.Server`.

## Adding a new inspector

1. Create a file in `internal/inspector/` implementing the `Inspector` interface:

```go
type myInspector struct {
    endpoint string
    client   *http.Client
}

func NewMyInspector(cfg *config.Config, httpClient *http.Client) *myInspector {
    return &myInspector{endpoint: cfg.Endpoint, client: httpClient}
}

func (m *myInspector) Name() string { return "my_inspector" }

func (m *myInspector) InspectPrompt(ctx context.Context, text, token string) *BlockResult {
    // return nil for pass, &BlockResult{Reason: "..."} for block
    return nil
}

func (m *myInspector) InspectResponse(ctx context.Context, text, token string) *BlockResult {
    return m.InspectPrompt(ctx, text, token)
}
```

2. Add the constructor call in `main.go`:

```go
inspectors = append(inspectors, inspector.NewMyInspector(cfg, httpClient))
```

3. Write tests with `httptest.NewServer` as a mock backend.

4. Add the inspector to the chain in the correct order: fast/cheap inspectors before slow/remote ones.

## Adding a new API format

1. Add prompt extraction in `internal/translate/translate.go`
2. Add response translation (Gemini to your format) in the same file
3. Add a handler method in `internal/handler/` with strict/fast/audit mode support
4. Register the route in `Server.Routes()` in `internal/handler/handler.go`
5. Add SSE streaming support in `internal/streaming/` if the format supports it

## Project structure

```
main.go                          Entry point (46 lines)
internal/
  config/      config.go, doc.go      Environment configuration
  auth/        auth.go, doc.go        Authentication, JWT, domain checks
  inspector/   *.go, doc.go           Screening backends
  vertex/      client.go, doc.go      Vertex AI client
  translate/   translate.go, doc.go   API format translation
  streaming/   streaming.go, doc.go   SSE formatting, chunk parsing
  handler/     *.go, doc.go           HTTP handlers, middleware, modes
main_test.go                     Integration tests
docs/                            Documentation
terraform/                       Infrastructure as code
```

## Commit messages

Short, imperative, focused on why not what:

```
Add DLP inspector for PII detection
Fix streaming truncation on large responses
Extract auth logic into internal package
```

## Org-specific values

Never commit project IDs, organisation domains, or account emails. These go in `.env` (gitignored) or `terraform.tfvars` (gitignored). The `terraform.tfvars.example` and `.env.example` files use placeholder values.
