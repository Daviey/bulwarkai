package policy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/Daviey/bulwarkai/internal/metrics"
	"github.com/open-policy-agent/opa/ast"
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
	enabled  bool
	mu       sync.RWMutex
	prepared rego.PreparedEvalQuery
}

func NewEngine(ctx context.Context, enabled bool, policyFile string, policyContent string) (*Engine, error) {
	if !enabled {
		return &Engine{enabled: false}, nil
	}

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

	r := rego.New(
		rego.Query("data.bulwarkai"),
		rego.Module("bulwarkai.rego", content),
		rego.SetRegoVersion(ast.RegoV1),
	)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		return nil, fmt.Errorf("opa policy compile: %w", err)
	}

	return &Engine{enabled: true, prepared: pq}, nil
}

func (e *Engine) Evaluate(ctx context.Context, input Input) (*Decision, error) {
	if !e.enabled {
		metrics.PolicyResults.WithLabelValues("allow").Inc()
		return &Decision{Allowed: true}, nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	results, err := e.prepared.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		slog.Error("opa eval error", "error", err)
		metrics.PolicyResults.WithLabelValues("error").Inc()
		return &Decision{Allowed: true}, nil
	}

	if len(results) == 0 || len(results[0].Expressions) == 0 {
		metrics.PolicyResults.WithLabelValues("allow").Inc()
		return &Decision{Allowed: true}, nil
	}

	obj, ok := results[0].Expressions[0].Value.(map[string]interface{})
	if !ok {
		boolVal, ok := results[0].Expressions[0].Value.(bool)
		if ok {
			if boolVal {
				metrics.PolicyResults.WithLabelValues("allow").Inc()
			} else {
				metrics.PolicyResults.WithLabelValues("deny").Inc()
			}
			return &Decision{Allowed: boolVal}, nil
		}
		metrics.PolicyResults.WithLabelValues("allow").Inc()
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
		metrics.PolicyResults.WithLabelValues("allow").Inc()
		return &Decision{Allowed: true}, nil
	}

	if reason == "" {
		reason = "denied by policy"
	}
	metrics.PolicyResults.WithLabelValues("deny").Inc()
	return &Decision{Allowed: false, Reason: reason}, nil
}
