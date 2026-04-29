+++
title = "Inspectors"
sort_by = "weight"
weight = 4
+++


## Inspector Interface

All inspectors implement the same Go interface:

```go
type Inspector interface {
    Name() string
    InspectPrompt(ctx context.Context, text string, token string) *BlockResult
    InspectResponse(ctx context.Context, text string, token string) *BlockResult
}
```

The chain stops at the first inspector that returns a non-nil `BlockResult`. Inspectors are initialised at startup in this order: regex (always), model_armor (non-strict modes when MODEL_ARMOR_TEMPLATE is set), dlp (opt-in via DLP_API=true).

## Existing Inspectors

### regexInspector

Always active. Pattern-matches against the prompt and response text using compiled regular expressions. Detects SSNs, 16-digit credit card numbers, private keys (RSA/DSA/EC/OpenSSH), AWS access keys, generic API keys (`sk-` prefix), and credential patterns (email + password in same text).

### modelArmorInspector

Calls Google Model Armor's standalone API (`sanitizeUserPrompt` / `sanitizeModelResponse`). Only loaded in non-strict modes because Model Armor's Vertex AI integration already enforces in strict mode via `generateContent`. Fail-open: if Model Armor returns an error, the request passes through.

### dlpInspector

Calls Google Cloud DLP `content:inspect`. Configurable info types and minimum likelihood threshold. Requires `DLP_API=true` environment variable. Fail-open on errors.

## HTTP Client Injection

All outbound HTTP calls (Vertex AI, Model Armor, DLP, OAuth tokeninfo) go through a configurable `*http.Client` on the `Config` struct. In production this is `http.DefaultClient`. Tests inject `httptest.Server` clients to mock external dependencies without hitting real APIs.

```go
func client() *http.Client {
    if cfg.Client != nil {
        return cfg.Client
    }
    return http.DefaultClient
}
```

The `modelArmorInspector` and `dlpInspector` structs hold their own `*http.Client` reference, initialised from `client()` at construction time. The DLP endpoint URL is also configurable (the `endpoint` field on `dlpInspector`), allowing tests to point it at a local mock server.

## Adding a New Inspector

To add a new screening backend:

1. Create a struct that implements the `Inspector` interface. Hold whatever config the backend needs (endpoint URL, API key, thresholds) as fields on the struct.

2. Create a constructor function (`newYourInspector() *yourInspector`) that reads config from the global `cfg` or environment variables. Add any new config fields to the `Config` struct in `main.go`.

3. Add the constructor call to `initInspectors()`. The order matters: the chain stops on the first block, so put fast/cheap inspectors before slow/networked ones. The current order is regex (local, microseconds), model_armor (remote, milliseconds), dlp (remote, milliseconds).

4. Inject an `*http.Client` field on your struct, initialised from `client()` in the constructor. This allows tests to mock HTTP calls.

5. Return `nil` from `InspectPrompt`/`InspectResponse` to indicate pass (no block). Return a `*BlockResult{Reason: "description"}` to block.

6. Follow fail-open semantics: if the backend returns an error or is unreachable, return `nil` (pass) and log the error at `WARN` level. The service should not block all traffic because one inspector is down.

7. Write tests using `httptest.NewServer` as a mock backend. See `TestModelArmorInspectorPromptBlocked` or `TestDLPInspectBlocked` for examples.

Example skeleton:

```go
type myInspector struct {
    endpoint string
    client   *http.Client
}

func newMyInspector() *myInspector {
    return &myInspector{
        endpoint: envOr("MY_INSPECTOR_URL", "https://example.com/api"),
        client:   client(),
    }
}

func (m *myInspector) Name() string { return "my_inspector" }

func (m *myInspector) InspectPrompt(ctx context.Context, text, token string) *BlockResult {
    // call m.endpoint, return BlockResult or nil
    return nil
}

func (m *myInspector) InspectResponse(ctx context.Context, text, token string) *BlockResult {
    return m.InspectPrompt(ctx, text, token)
}
```

## Testing

Tests use the Anthropic SDK (`anthropic-sdk-go`) and OpenAI SDK (`openai-go/v3`) to validate response shapes match what real client libraries expect.

Test categories:
- Inspector unit tests (regex, Model Armor mock, DLP mock)
- Translation tests (Anthropic to Gemini, Gemini to Anthropic/OpenAI, finish reason mapping)
- SDK compliance tests (unmarshal responses into actual SDK types)
- Handler integration tests (full request lifecycle with stubbed Vertex AI)
- Auth tests (JWT extraction, domain allowlist, API key)
- Streaming tests (SSE formatting, chunk parsing, text accumulation)
- Middleware tests (request ID, trace ID, status capture)
- Configuration tests (env parsing, inspector initialisation)
- LOCAL_MODE tests (auth bypass, ADC token resolution, inspector chain still active)

All external dependencies are mocked with `httptest.NewServer`. No test makes real network calls.

```bash
go test -v -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

## Structured Logging

Uses Go's `slog` with JSON output. Every request gets a request ID (from `X-Request-ID` header or auto-generated) and optional trace ID (from `X-Cloud-Trace-Context`). The `requestMiddleware` wraps all handlers with request-scoped loggers that include method, path, status code, and duration.

Blocked requests log at `WARN` level with action prefix `BLOCK_PROMPT`, `BLOCK_RESPONSE`, or `BLOCK_RESPONSE_AUDIT`. Allowed requests log at `INFO` with `ALLOW`, `ALLOW_FAST`, or `ALLOW_AUDIT`.
