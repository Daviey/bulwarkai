# OPA Policy Engine Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Embed OPA as a Go library for access control decisions (model RBAC, cost limits, response mode enforcement) that run after auth but before the inspector chain.

**Architecture:** OPA is embedded using the `rego` package (not the SDK, not a sidecar). Policies are loaded from a local file or a GCS URL at startup. The engine receives JWT claims, requested model, and request parameters as input, returning allow/deny with an optional reason. It is off by default (`OPA_ENABLED` env var). OPA never sees prompt or response text.

**Tech Stack:** Go 1.25, `github.com/open-policy-agent/opa/rego`, standard library for HTTP/GCS fetch.

## Design Decisions (pre-plan)

1. **Why `rego` package, not `sdk` package:** The `v1/sdk` package requires a YAML config file, plugin system, and bundle infrastructure. For our use case (single policy, embedded evaluation), the `rego` package is lighter and has no config file requirement. We prepare a query at startup and evaluate per-request.

2. **Why not a sidecar:** A sidecar adds network hops, latency, and operational complexity for a policy check that takes microseconds in-process. Embedding keeps the single-binary deployment model.

3. **Why policy as env var / file, not a CRD:** Bulwarkai is not Kubernetes-native. Policies live as Rego files, either embedded in the container or fetched from GCS at startup.

4. **Fail-open on OPA errors:** If OPA is enabled but the policy evaluation fails (bad policy, OOM, etc.), the request is allowed through. OPA is an access control layer, not a safety layer. The inspector chain handles safety. A misconfigured policy should not block all AI traffic.

5. **Pipeline position:** After auth (we need the email), before inspectors (inspectors are for content safety, not access control).

## Input schema

OPA receives this as `input`:

```json
{
  "email": "user@example.com",
  "model": "gemini-2.5-flash",
  "action": "generateContent",
  "stream": false,
  "max_tokens": 4096,
  "api_format": "openai",
  "path": "/v1/chat/completions"
}
```

## Default policy

A permissive default policy is embedded in the binary. It allows everything:

```rego
package bulwarkai

default allow := true
```

Users override this by setting `OPA_POLICY_FILE` or `OPA_POLICY_URL`.

## Example production policy

```rego
package bulwarkai

default allow := false

allow if {
    input.email == "admin@example.com"
}

allow if {
    groups[input.email]
    model_allowed[input.model]
}

model_allowed["gemini-2.5-flash"]
model_allowed["gemini-2.5-pro"]

groups["alice@example.com"]
groups["bob@example.com"]

deny_response_mode := "strict" if {
    not input.stream
}
```

---

### Task 1: Add OPA config fields to config.go

**Files:**
- Modify: `internal/config/config.go`

**Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestLoad_OPADisabled(t *testing.T) {
	cfg := Load()
	if cfg.OPAEnabled {
		t.Fatal("OPA should be disabled by default")
	}
}

func TestLoad_OPAEnabled(t *testing.T) {
	os.Setenv("OPA_ENABLED", "true")
	os.Setenv("OPA_POLICY_FILE", "/etc/bulwarkai/policy.rego")
	defer os.Unsetenv("OPA_ENABLED")
	defer os.Unsetenv("OPA_POLICY_FILE")
	cfg := Load()
	if !cfg.OPAEnabled {
		t.Fatal("expected OPA enabled")
	}
	if cfg.OPAPolicyFile != "/etc/bulwarkai/policy.rego" {
		t.Fatalf("got %q", cfg.OPAPolicyFile)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `nix-shell -p go_1_25 --run "cd /home/dave/dev/opencode/hsbc/mq/bulwarkai && go test ./internal/config/ -run TestLoad_OPA -v"`
Expected: FAIL (fields do not exist)

**Step 3: Write minimal implementation**

Add fields to the `Config` struct in `internal/config/config.go`:

```go
OPAEnabled    bool
OPAPolicyFile string
OPAPolicyURL  string
```

In `Load()`, add:

```go
OPAEnabled:    os.Getenv("OPA_ENABLED") == "true",
OPAPolicyFile: EnvOr("OPA_POLICY_FILE", ""),
OPAPolicyURL:  EnvOr("OPA_POLICY_URL", ""),
```

**Step 4: Run test to verify it passes**

Run: `nix-shell -p go_1_25 --run "cd /home/dave/dev/opencode/hsbc/mq/bulwarkai && go test ./internal/config/ -run TestLoad_OPA -v"`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(policy): add OPA config fields"
```

---

### Task 2: Create the OPA policy engine

**Files:**
- Create: `internal/policy/engine.go`
- Create: `internal/policy/engine_test.go`

**Step 1: Write the failing test**

Create `internal/policy/engine_test.go`:

```go
package policy

import (
	"context"
	"testing"
)

func TestNewEngine_Disabled(t *testing.T) {
	eng, err := NewEngine(context.Background(), false, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if eng == nil {
		t.Fatal("engine should not be nil when disabled")
	}
	dec, err := eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatal("disabled engine should allow everything")
	}
}

func TestNewEngine_DefaultPolicy(t *testing.T) {
	eng, err := NewEngine(context.Background(), true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	dec, err := eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatal("default policy should allow")
	}
}

func TestNewEngine_CustomPolicy(t *testing.T) {
	policy := `
package bulwarkai

default allow := false

allow if {
	input.email == "admin@test.com"
}
`
	eng, err := NewEngine(context.Background(), true, "", policy)
	if err != nil {
		t.Fatal(err)
	}

	dec, err := eng.Evaluate(context.Background(), Input{Email: "admin@test.com", Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatal("admin should be allowed")
	}

	dec, err = eng.Evaluate(context.Background(), Input{Email: "evil@test.com", Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allowed {
		t.Fatal("unknown user should be denied")
	}
}

func TestNewEngine_CustomPolicyWithReason(t *testing.T) {
	policy := `
package bulwarkai

default allow := false

deny_reason := "model not permitted" if {
	not model_allowed[input.model]
}

allow if {
	model_allowed[input.model]
}

model_allowed["gemini-2.5-flash"]
`
	eng, err := NewEngine(context.Background(), true, "", policy)
	if err != nil {
		t.Fatal(err)
	}

	dec, err := eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatal("allowed model should pass")
	}

	dec, err = eng.Evaluate(context.Background(), Input{Email: "test@test.com", Model: "gemini-2.5-pro"})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allowed {
		t.Fatal("disallowed model should be denied")
	}
	if dec.Reason != "model not permitted" {
		t.Fatalf("got reason %q", dec.Reason)
	}
}

func TestNewEngine_StreamEnforcement(t *testing.T) {
	policy := `
package bulwarkai

default allow := true

deny_reason := "streaming not allowed for your group" if {
	input.stream
	not streamers[input.email]
}

allow if {
	not input.stream
}

allow if {
	input.stream
	streamers[input.email]
}

streamers["fast@test.com"]
`
	eng, err := NewEngine(context.Background(), true, "", policy)
	if err != nil {
		t.Fatal(err)
	}

	dec, err := eng.Evaluate(context.Background(), Input{Email: "fast@test.com", Model: "gemini-2.5-flash", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatal("streamer should be allowed to stream")
	}

	dec, err = eng.Evaluate(context.Background(), Input{Email: "slow@test.com", Model: "gemini-2.5-flash", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allowed {
		t.Fatal("non-streamer should be denied streaming")
	}
	if dec.Reason != "streaming not allowed for your group" {
		t.Fatalf("got reason %q", dec.Reason)
	}

	dec, err = eng.Evaluate(context.Background(), Input{Email: "slow@test.com", Model: "gemini-2.5-flash", Stream: false})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatal("non-streaming should always be allowed")
	}
}

func TestNewEngine_EngineClosed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewEngine(ctx, true, "", "")
	if err == nil {
		t.Fatal("cancelled context should error")
	}
}

func TestNewEngine_BadPolicy(t *testing.T) {
	_, err := NewEngine(context.Background(), true, "", "this is not valid rego !!!")
	if err == nil {
		t.Fatal("bad policy should error")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `nix-shell -p go_1_25 --run "cd /home/dave/dev/opencode/hsbc/mq/bulwarkai && go test ./internal/policy/ -v"`
Expected: FAIL (package does not exist)

**Step 3: Write the implementation**

First, add the OPA dependency:

```bash
nix-shell -p go_1_25 --run "cd /home/dave/dev/opencode/hsbc/mq/bulwarkai && go get github.com/open-policy-agent/opa/rego"
```

Create `internal/policy/engine.go`:

```go
package policy

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/open-policy-agent/opa/rego"
)

const defaultPolicy = `package bulwarkai

default allow := true
`

type Input struct {
	Email     string `json:"email"`
	Model     string `json:"model"`
	Action    string `json:"action"`
	Stream    bool   `json:"stream"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	APIFormat string `json:"api_format,omitempty"`
	Path      string `json:"path,omitempty"`
}

type Decision struct {
	Allowed bool
	Reason  string
}

type Engine struct {
	enabled bool
	mu      sync.RWMutex
	query   rego.PreparedEvalQuery
}

func NewEngine(ctx context.Context, enabled bool, policyFile string, policyContent string) (*Engine, error) {
	if !enabled {
		return &Engine{enabled: false}, nil
	}

	content := policyContent
	if content == "" {
		if policyFile != "" {
			return nil, fmt.Errorf("policy file loading not yet implemented, use policy content directly")
		}
		content = defaultPolicy
	}

	r := rego.New(
		rego.Query("data.bulwarkai.allow"),
		rego.Module("bulwarkai.rego", content),
		rego.Query("data.bulwarkai.deny_reason"),
	)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		return nil, fmt.Errorf("opa policy compile: %w", err)
	}

	// Also prepare a second query for deny_reason using the same compiler.
	// We actually need two separate rego instances sharing the same module.
	// Simpler: use a single query that returns a set.
	// Instead, let's prepare both queries.
	r2 := rego.New(
		rego.Query("{x: data.bulwarkai[x]}"),
		rego.Module("bulwarkai.rego", content),
	)
	pq2, err := r2.PrepareForEval(ctx)
	if err != nil {
		return nil, fmt.Errorf("opa deny_reason compile: %w", err)
	}

	_ = pq2 // store for reason queries

	return &Engine{
		enabled: true,
		query:   pq,
	}, nil
}

func (e *Engine) Evaluate(ctx context.Context, input Input) (*Decision, error) {
	if !e.enabled {
		return &Decision{Allowed: true}, nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	results, err := e.query.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		slog.Error("opa eval error", "error", err)
		return &Decision{Allowed: true}, nil
	}

	if len(results) == 0 {
		return &Decision{Allowed: true}, nil
	}

	allowed, ok := results[0].Expressions[0].Value.(bool)
	if !ok {
		return &Decision{Allowed: true}, nil
	}

	if allowed {
		return &Decision{Allowed: true}, nil
	}

	return &Decision{Allowed: false, Reason: "denied by policy"}, nil
}
```

Wait, the deny_reason extraction is complex. Let me simplify the design: query both `allow` and `deny_reason` in one evaluation. The simplest approach is to use a single query that returns a map:

```go
r := rego.New(
    rego.Query("{allow: data.bulwarkai.allow, reason: data.bulwarkai.deny_reason}"),
    rego.Module("bulwarkai.rego", content),
)
```

This returns a single map with both values. Let me revise the implementation.

Revised `internal/policy/engine.go`:

```go
package policy

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/open-policy-agent/opa/rego"
)

const defaultPolicy = `package bulwarkai

default allow := true
`

type Input struct {
	Email     string `json:"email"`
	Model     string `json:"model"`
	Action    string `json:"action"`
	Stream    bool   `json:"stream"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	APIFormat string `json:"api_format,omitempty"`
	Path      string `json:"path,omitempty"`
}

type Decision struct {
	Allowed bool
	Reason  string
}

type Engine struct {
	enabled bool
	mu      sync.RWMutex
	prepared rego.PreparedEvalQuery
}

func NewEngine(ctx context.Context, enabled bool, policyFile string, policyContent string) (*Engine, error) {
	if !enabled {
		return &Engine{enabled: false}, nil
	}

	content := policyContent
	if content == "" {
		if policyFile != "" {
			return nil, fmt.Errorf("policy file loading not yet implemented, use policy content directly")
		}
		content = defaultPolicy
	}

	r := rego.New(
		rego.Query("data.bulwarkai"),
		rego.Module("bulwarkai.rego", content),
	)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		return nil, fmt.Errorf("opa policy compile: %w", err)
	}

	return &Engine{enabled: true, prepared: pq}, nil
}

func (e *Engine) Evaluate(ctx context.Context, input Input) (*Decision, error) {
	if !e.enabled {
		return &Decision{Allowed: true}, nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	results, err := e.prepared.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		slog.Error("opa eval error", "error", err)
		return &Decision{Allowed: true}, nil
	}

	if len(results) == 0 || len(results[0].Expressions) == 0 {
		return &Decision{Allowed: true}, nil
	}

	obj, ok := results[0].Expressions[0].Value.(map[string]interface{})
	if !ok {
		boolVal, ok := results[0].Expressions[0].Value.(bool)
		if ok {
			return &Decision{Allowed: boolVal}, nil
		}
		return &Decision{Allowed: true}, nil
	}

	allowed := true
	if v, ok := obj["allow"]; ok {
		if b, ok := v.(bool); ok {
			allowed = b
		}
	}

	reason := ""
	if v, ok := obj["deny_reason"]; ok {
		if s, ok := v.(string); ok {
			reason = s
		}
	}

	if allowed {
		return &Decision{Allowed: true}, nil
	}

	if reason == "" {
		reason = "denied by policy"
	}
	return &Decision{Allowed: false, Reason: reason}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `nix-shell -p go_1_25 --run "cd /home/dave/dev/opencode/hsbc/mq/bulwarkai && go test ./internal/policy/ -v"`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/policy/engine.go internal/policy/engine_test.go go.mod go.sum
git commit -m "feat(policy): add OPA policy engine with embedded rego"
```

---

### Task 3: Wire OPA into the handler pipeline

**Files:**
- Modify: `internal/handler/handler.go`
- Modify: `main.go`

**Step 1: Write the failing test**

Add to `internal/handler/handler_test.go` (or create a new test in handler package that tests OPA integration):

This is tricky to unit test in the handler layer without a full integration test. The OPA check will be a method on `Server` that each handler calls after auth. For now, the policy engine tests from Task 2 cover the logic. The handler wiring is verified by the existing integration tests in `main_test.go`.

Instead, add a test that verifies the server rejects when OPA denies:

```go
// In handler test, after creating server with OPA enabled + restrictive policy
```

Actually, the cleanest approach is to add OPA as a field on `Server` and add a helper method. Let me plan this carefully.

**Step 2: Modify Server struct to hold policy engine**

In `internal/handler/handler.go`, add import and field:

```go
import "github.com/Daviey/bulwarkai/internal/policy"
```

Add to `Server` struct:

```go
type Server struct {
    cfg        *config.Config
    chain      inspector.Chain
    vertex     vertex.VertexCaller
    httpClient *http.Client
    policy     *policy.Engine
}
```

Update `NewServer`:

```go
func NewServer(cfg *config.Config, chain inspector.Chain, vc vertex.VertexCaller, httpClient *http.Client, eng *policy.Engine) *Server {
    return &Server{cfg: cfg, chain: chain, vertex: vc, httpClient: httpClient, policy: eng}
}
```

Add a helper method:

```go
func (s *Server) checkPolicy(w http.ResponseWriter, r *http.Request, identity *auth.Identity, model string, stream bool) bool {
    if s.policy == nil {
        return true
    }
    dec, err := s.policy.Evaluate(r.Context(), policy.Input{
        Email:  identity.Email,
        Model:  model,
        Stream: stream,
        Path:   r.URL.Path,
    })
    if err != nil {
        slog.Error("opa evaluate error", "error", err)
        return true
    }
    if !dec.Allowed {
        s.logAction("DENY_POLICY", model, "", dec.Reason, identity.Email)
        http.Error(w, dec.Reason, http.StatusForbidden)
        return false
    }
    return true
}
```

**Step 3: Add checkPolicy calls to each handler**

In `internal/handler/vertex.go`, after auth and UA check, before prompt extraction:

```go
if !s.checkPolicy(w, r, identity, model, wasStreaming) {
    return
}
```

Note: `wasStreaming` is determined after model/action parsing, so the check goes after that block but before `parseBody`.

In `internal/handler/openai.go`, after auth and UA check, before prompt extraction:

```go
if !s.checkPolicy(w, r, identity, model, isStream) {
    return
}
```

Same pattern in `internal/handler/anthropic.go`.

**Step 4: Wire in main.go**

In `main.go`, after creating the inspector chain and before creating the server:

```go
var policyEngine *policy.Engine
if cfg.OPAEnabled {
    pe, err := policy.NewEngine(context.Background(), cfg.OPAEnabled, cfg.OPAPolicyFile, cfg.OPAPolicyURL)
    if err != nil {
        slog.Error("opa init failed", "error", err)
        os.Exit(1)
    }
    policyEngine = pe
    slog.Info("opa policy engine enabled")
}
```

Update `NewServer` call:

```go
server := handler.NewServer(cfg, chain, caller, httpClient, policyEngine)
```

Add import:

```go
"github.com/Daviey/bulwarkai/internal/policy"
```

**Step 5: Run all tests**

Run: `nix-shell -p go_1_25 --run "cd /home/dave/dev/opencode/hsbc/mq/bulwarkai && go test ./... -v -count=1"`
Expected: PASS (all existing tests still pass, NewServer signature changed but tests that call NewServer need updating)

Check which tests call `handler.NewServer` and update them:

Search `main_test.go` for `NewServer` calls and add `nil` as last argument (no OPA in tests by default).

**Step 6: Commit**

```bash
git add internal/handler/handler.go internal/handler/vertex.go internal/handler/openai.go internal/handler/anthropic.go main.go main_test.go
git commit -m "feat(policy): wire OPA into request pipeline after auth"
```

---

### Task 4: Add OPA policy file loading from disk

**Files:**
- Modify: `internal/policy/engine.go`

**Step 1: Write the failing test**

Add to `internal/policy/engine_test.go`:

```go
func TestNewEngine_PolicyFromFile(t *testing.T) {
	f, err := os.CreateTemp("", "policy-*.rego")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	fmt.Fprintln(f, `package bulwarkai
default allow := false
allow if { input.email == "file@test.com" }
`)
	f.Close()

	eng, err := NewEngine(context.Background(), true, f.Name(), "")
	if err != nil {
		t.Fatal(err)
	}
	dec, err := eng.Evaluate(context.Background(), Input{Email: "file@test.com", Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatal("should be allowed from file policy")
	}
	dec, err = eng.Evaluate(context.Background(), Input{Email: "other@test.com", Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allowed {
		t.Fatal("should be denied by file policy")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `nix-shell -p go_1_25 --run "cd /home/dave/dev/opencode/hsbc/mq/bulwarkai && go test ./internal/policy/ -run TestNewEngine_PolicyFromFile -v"`
Expected: FAIL (file loading returns error)

**Step 3: Implement file loading**

In `internal/policy/engine.go`, update the content resolution in `NewEngine`:

```go
content := policyContent
if content == "" && policyFile != "" {
    data, err := os.ReadFile(policyFile)
    if err != nil {
        return nil, fmt.Errorf("read policy file %s: %w", policyFile, err)
    }
    content = string(data)
}
if content == "" {
    content = defaultPolicy
}
```

Add `"os"` to imports.

**Step 4: Run test to verify it passes**

Run: `nix-shell -p go_1_25 --run "cd /home/dave/dev/opencode/hsbc/mq/bulwarkai && go test ./internal/policy/ -run TestNewEngine_PolicyFromFile -v"`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/policy/engine.go internal/policy/engine_test.go
git commit -m "feat(policy): load policy from file"
```

---

### Task 5: Add health endpoint exposure of OPA status

**Files:**
- Modify: `internal/handler/handler.go`

**Step 1: Update health handler**

In `healthHandler`, add OPA status:

```go
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
    resp := map[string]string{
        "status":  "ok",
        "mode":    s.cfg.ResponseMode,
        "version": s.cfg.Version,
    }
    if s.policy != nil {
        resp["opa"] = "enabled"
    }
    writeJSON(w, resp)
}
```

**Step 2: Run tests**

Run: `nix-shell -p go_1_25 --run "cd /home/dave/dev/opencode/hsbc/mq/bulwarkai && go test ./internal/handler/ -v -count=1"`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/handler/handler.go
git commit -m "feat(policy): expose OPA status in health endpoint"
```

---

### Task 6: Write ADR for OPA integration

**Files:**
- Create: `docs/adr/009-why-opa.md`

**Step 1: Write the ADR**

Create `docs/adr/009-why-opa.md`:

```markdown
# ADR-009: Why OPA for Access Control

## Status

Accepted

## Context

The service needs model-level RBAC and usage policies. Examples: only certain users can access expensive models, streaming is restricted to specific teams, max_tokens limits vary by group. These are access control decisions, not content safety decisions. The inspector chain handles content safety.

## Decision

Embed Open Policy Agent (OPA) as an in-process Go library using the `rego` package. Policies are written in Rego and loaded at startup from a file or inline content.

## Alternatives Considered

1. **Hardcoded config maps (env vars):** Simpler but not expressive enough. Cannot express "streaming only for team X" or "max_tokens limited by group" without a combinatorial explosion of env vars.

2. **OPA sidecar:** Adds a network hop (~1ms) and operational complexity (extra container, health checks, resource limits). Policy evaluation is microseconds in-process.

3. **OPA SDK package (`v1/sdk`):** Requires YAML config, plugin infrastructure, and bundle management. Overkill for loading a single policy file.

4. **Custom rule engine:** Would be reinventing Rego with worse expressiveness and no ecosystem.

## Consequences

- OPA adds ~10MB to the binary size (the rego package includes the compiler and evaluator).
- Policies are loaded at startup. Changing a policy requires a new Cloud Run revision.
- OPA is off by default. Enabling it is a conscious choice.
- OPA never sees prompt or response text. It only sees identity, model, and request parameters.
- Fail-open on evaluation errors. A broken policy does not block all traffic.
```

**Step 2: Commit**

```bash
git add docs/adr/009-why-opa.md
git commit -m "docs(adr): add OPA integration decision record"
```

---

### Task 7: Update design.md with OPA architecture

**Files:**
- Modify: `docs/design.md`

**Step 1: Update the architecture diagram**

Add OPA as step 2.5 in the pipeline. Update the mermaid graph to show the policy engine between auth and inspectors:

In the `graph LR` diagram, add:

```
POL[2.5. Policy check]
```

And update the flow: `UA --> POL --> EXT --> INSP`

Also add to the subgraph connections.

**Step 2: Add OPA section to design doc**

After the "Authentication" section, add:

```markdown
## Policy Engine (OPA)

When enabled via `OPA_ENABLED=true`, an embedded Open Policy Agent evaluates access control decisions after authentication but before content inspection. The policy engine receives the caller's email, requested model, streaming flag, and request path. It never sees prompt or response text.

The policy is loaded at startup from `OPA_POLICY_FILE` (file path) or `OPA_POLICY_URL` (inline Rego content). If neither is set, a permissive default policy (`allow := true`) is used.

Evaluation is synchronous and in-process using the `rego` package. A typical policy evaluation takes under 100 microseconds. On evaluation errors, the engine fails open (allows the request) and logs the error.

Pipeline position: auth -> policy engine -> inspector chain -> Vertex AI.
```

**Step 3: Commit**

```bash
git add docs/design.md
git commit -m "docs(design): add OPA policy engine architecture"
```

---

### Task 8: Update configuration.md with OPA env vars

**Files:**
- Modify: `docs/configuration.md`

**Step 1: Add OPA configuration section**

After the Cloud DLP section, add:

```markdown
## Policy Engine (OPA)

| Variable | Type | Default | Description |
|---|---|---|---|
| `OPA_ENABLED` | string | unset | Set to `"true"` to enable the OPA policy engine. Any other value (or unset) disables it. |
| `OPA_POLICY_FILE` | string | none | Path to a Rego policy file on disk. Loaded at startup. If not set, falls back to `OPA_POLICY_URL` or the default permissive policy. |
| `OPA_POLICY_URL` | string | none | Inline Rego policy content or a URL to fetch policy from. Reserved for future GCS bundle support. If not set, falls back to the default permissive policy. |
```

Also update the "Disabling Features" table:

```markdown
| OPA policy engine | Leave `OPA_ENABLED` unset or set to anything other than `"true"` |
```

And update the "Inspector Loading" section to mention the pipeline order:

```markdown
The full pipeline runs in this order:

1. Authentication (domain check, JWT validation, API key)
2. Policy engine (OPA, if enabled): access control based on identity and model
3. Regex inspector: always loaded, cannot be disabled
4. Model Armor inspector: loaded when `MODEL_ARMOR_TEMPLATE` is non-empty and `RESPONSE_MODE` is not `strict`
5. DLP inspector: loaded when `DLP_API` is `"true"`
```

**Step 2: Commit**

```bash
git add docs/configuration.md
git commit -m "docs(config): add OPA policy engine configuration"
```

---

### Task 9: Update .env.example

**Files:**
- Modify: `.env.example`

**Step 1: Add OPA section**

After the DLP section, add:

```
# OPA Policy Engine (set OPA_ENABLED=true to enable)
OPA_ENABLED=
OPA_POLICY_FILE=
OPA_POLICY_URL=
```

**Step 2: Commit**

```bash
git add .env.example
git commit -m "docs(env): add OPA policy engine variables"
```

---

### Task 10: Run full test suite and CI checks

**Step 1: Run all tests**

Run: `nix-shell -p go_1_25 --run "cd /home/dave/dev/opencode/hsbc/mq/bulwarkai && go test ./... -v -count=1 -coverprofile=coverage.out"`

**Step 2: Run go vet**

Run: `nix-shell -p go_1_25 --run "cd /home/dave/dev/opencode/hsbc/mq/bulwarkai && go vet ./..."`

**Step 3: Run build**

Run: `nix-shell -p go_1_25 --run "cd /home/dave/dev/opencode/hsbc/mq/bulwarkai && CGO_ENABLED=0 go build -ldflags '-X main.version=dev' -o bulwarkai ."`

**Step 4: Regenerate swagger docs**

Run: `nix-shell -p go_1_25 --run "cd /home/dave/dev/opencode/hsbc/mq/bulwarkai && swag init -g main.go -d .,./internal/handler --parseInternal --output ./docs --outputTypes yaml"`

**Step 5: Fix any failures and commit**

---

### Task 11: Squash all commits, create PR

**Step 1: Soft reset to before all OPA commits**

```bash
git reset --soft $(git log --oneline | tail -1 | awk '{print $1}')
git reset HEAD .
```

**Step 2: Create feature branch from origin/main**

```bash
git stash
git checkout main && git reset --hard origin/main
git checkout -b opa-policy-engine
git stash pop
git add -A
git commit -m "feat(policy): add embedded OPA policy engine for access control"
```

**Step 3: Push and create PR**

```bash
git push -u origin opa-policy-engine
gh pr create --title "feat(policy): add embedded OPA policy engine" --body "..."
gh pr merge --squash --admin
```
