package policy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

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
	enabled         bool
	mu              sync.RWMutex
	prepared        rego.PreparedEvalQuery
	policyFile      string
	policyURL       string
	pollTicker      *time.Ticker
	pollDone        chan struct{}
	lastModTime     time.Time
	httpClient      *http.Client
	urlPollInterval time.Duration
}

func NewEngine(ctx context.Context, enabled bool, policyFile string, policyURL string) (*Engine, error) {
	return NewEngineWithHTTP(ctx, enabled, policyFile, policyURL, nil)
}

func NewEngineWithHTTP(ctx context.Context, enabled bool, policyFile string, policyURL string, hc *http.Client) (*Engine, error) {
	return newEngine(ctx, enabled, policyFile, policyURL, hc, 0)
}

func newEngine(ctx context.Context, enabled bool, policyFile string, policyURL string, hc *http.Client, urlPoll time.Duration) (*Engine, error) {
	if !enabled {
		return &Engine{enabled: false}, nil
	}

	content, modTime, err := loadPolicy(ctx, policyFile, policyURL, hc)
	if err != nil {
		return nil, err
	}
	if content == "" {
		content = defaultPolicy
	}

	pq, err := compilePolicy(ctx, content)
	if err != nil {
		return nil, err
	}

	e := &Engine{
		enabled:         true,
		prepared:        pq,
		policyFile:      policyFile,
		policyURL:       policyURL,
		pollDone:        make(chan struct{}),
		httpClient:      hc,
		lastModTime:     modTime,
		urlPollInterval: urlPoll,
	}

	if policyFile != "" {
		interval := pollInterval(policyFile)
		e.pollTicker = time.NewTicker(interval)
		go e.watchFile()
		slog.Info("opa file watcher started", "file", policyFile, "poll_interval", interval)
	}

	if policyURL != "" && isHTTPURL(policyURL) {
		interval := urlPoll
		if interval == 0 {
			interval = 30 * time.Second
		}
		e.pollTicker = time.NewTicker(interval)
		go e.watchURL()
		slog.Info("opa url poller started", "url", policyURL, "poll_interval", interval)
	}

	return e, nil
}

func (e *Engine) Stop() {
	if e.pollTicker != nil {
		e.pollTicker.Stop()
	}
	if e.pollDone != nil {
		select {
		case <-e.pollDone:
		default:
			close(e.pollDone)
		}
	}
}

func (e *Engine) Evaluate(ctx context.Context, input Input) *Decision {
	if !e.enabled {
		metrics.PolicyResults.WithLabelValues("allow").Inc()
		return &Decision{Allowed: true}
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	results, err := e.prepared.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		slog.Error("opa eval error", "error", err)
		metrics.PolicyResults.WithLabelValues("error").Inc()
		return &Decision{Allowed: true}
	}

	if len(results) == 0 || len(results[0].Expressions) == 0 {
		metrics.PolicyResults.WithLabelValues("allow").Inc()
		return &Decision{Allowed: true}
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
			return &Decision{Allowed: boolVal}
		}
		metrics.PolicyResults.WithLabelValues("allow").Inc()
		return &Decision{Allowed: true}
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
		return &Decision{Allowed: true}
	}

	if reason == "" {
		reason = "denied by policy"
	}
	metrics.PolicyResults.WithLabelValues("deny").Inc()
	return &Decision{Allowed: false, Reason: reason}
}

func (e *Engine) watchFile() {
	for {
		select {
		case <-e.pollDone:
			return
		case <-e.pollTicker.C:
			info, err := os.Stat(e.policyFile)
			if err != nil {
				slog.Error("opa file stat error", "file", e.policyFile, "error", err)
				continue
			}
			if info.ModTime().After(e.lastModTime) {
				if err := e.reloadFromFile(); err != nil {
					slog.Error("opa hot-reload failed", "file", e.policyFile, "error", err)
				} else {
					slog.Info("opa policy hot-reloaded", "file", e.policyFile)
				}
			}
		}
	}
}

func (e *Engine) watchURL() {
	for {
		select {
		case <-e.pollDone:
			return
		case <-e.pollTicker.C:
			if err := e.reloadFromURL(); err != nil {
				slog.Error("opa url reload failed", "url", e.policyURL, "error", err)
			}
		}
	}
}

func (e *Engine) reloadFromFile() error {
	data, err := os.ReadFile(e.policyFile) //nosec G304
	if err != nil {
		return fmt.Errorf("read policy file: %w", err)
	}
	return e.applyPolicy(string(data))
}

func (e *Engine) reloadFromURL() error {
	hc := e.httpClient
	if hc == nil {
		hc = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", e.policyURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("fetch policy url: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("policy url returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read policy body: %w", err)
	}
	slog.Info("opa policy fetched from url", "url", e.policyURL, "size", len(body))
	return e.applyPolicy(string(body))
}

func (e *Engine) applyPolicy(content string) error {
	if content == "" {
		return nil
	}
	pq, err := compilePolicy(context.Background(), content)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.prepared = pq
	if e.policyFile != "" {
		info, statErr := os.Stat(e.policyFile)
		if statErr == nil {
			e.lastModTime = info.ModTime()
		}
	}
	e.mu.Unlock()
	return nil
}

func loadPolicy(ctx context.Context, policyFile string, policyURL string, hc *http.Client) (string, time.Time, error) {
	if policyFile != "" {
		data, err := os.ReadFile(policyFile) //nosec G304
		if err != nil {
			return "", time.Time{}, fmt.Errorf("read policy file %s: %w", policyFile, err)
		}
		info, _ := os.Stat(policyFile)
		var modTime time.Time
		if info != nil {
			modTime = info.ModTime()
		}
		return string(data), modTime, nil
	}
	if policyURL != "" && isHTTPURL(policyURL) {
		client := hc
		if client == nil {
			client = http.DefaultClient
		}
		fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(fetchCtx, "GET", policyURL, nil)
		if err != nil {
			return "", time.Time{}, fmt.Errorf("create policy request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", time.Time{}, fmt.Errorf("fetch policy from %s: %w", policyURL, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", time.Time{}, fmt.Errorf("policy url %s returned %d", policyURL, resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return "", time.Time{}, fmt.Errorf("read policy body: %w", err)
		}
		return string(body), time.Now(), nil
	}
	if policyURL != "" {
		return policyURL, time.Time{}, nil
	}
	return "", time.Time{}, nil
}

func compilePolicy(ctx context.Context, content string) (rego.PreparedEvalQuery, error) {
	r := rego.New(
		rego.Query("data.bulwarkai"),
		rego.Module("bulwarkai.rego", content),
		rego.SetRegoVersion(ast.RegoV1),
	)
	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		return rego.PreparedEvalQuery{}, fmt.Errorf("opa policy compile: %w", err)
	}
	return pq, nil
}

func isHTTPURL(s string) bool {
	return len(s) > 7 && (s[:7] == "http://" || s[:8] == "https://")
}

func pollInterval(path string) time.Duration {
	return 5 * time.Second
}
