package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Daviey/bulwarkai/internal/config"
	"github.com/Daviey/bulwarkai/internal/inspector"
	"github.com/Daviey/bulwarkai/internal/policy"
	"github.com/Daviey/bulwarkai/internal/vertex"
	"github.com/Daviey/bulwarkai/internal/webhook"
)

func testConfig() *config.Config {
	return &config.Config{
		Project:             "test-project",
		Location:            "europe-west2",
		FallbackGeminiModel: "gemini-2.5-flash",
		ResponseMode:        "strict",
		Version:             "test",
	}
}

func testServer(cfg *config.Config, vc vertex.VertexCaller) *Server {
	chain := inspector.NewChain()
	return NewServer(cfg, chain, vc, http.DefaultClient, nil, nil, nil)
}

func TestHealthEndpoint(t *testing.T) {
	srv := testServer(testConfig(), nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/health", nil)
	srv.healthHandler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Fatalf("got %q", body["status"])
	}
	if body["mode"] != "strict" {
		t.Fatalf("got %q", body["mode"])
	}
}

func TestReadiness_OK(t *testing.T) {
	demo := vertex.NewDemoClient(testConfig())
	srv := testServer(testConfig(), demo)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/ready", nil)
	srv.readinessHandler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
}

func TestReadiness_PostRejected(t *testing.T) {
	demo := vertex.NewDemoClient(testConfig())
	srv := testServer(testConfig(), demo)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/ready", nil)
	srv.readinessHandler(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", w.Code)
	}
}

func TestReadiness_NoVertex(t *testing.T) {
	srv := testServer(testConfig(), nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/ready", nil)
	srv.readinessHandler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
}

func TestTestStringsEndpoint(t *testing.T) {
	srv := testServer(testConfig(), nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/test-strings", nil)
	srv.testStringsHandler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["ssn"] != inspector.TestSSN {
		t.Fatalf("got %q", body["ssn"])
	}
	if body["credit_card"] != inspector.TestCreditCard {
		t.Fatalf("got %q", body["credit_card"])
	}
}

func TestServeOpenAIModels(t *testing.T) {
	srv := testServer(testConfig(), nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/models", nil)
	srv.ServeOpenAIModels(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if body["object"] != "list" {
		t.Fatalf("got %v", body["object"])
	}
}

func TestServeOpenAIModels_PostRejected(t *testing.T) {
	srv := testServer(testConfig(), nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/models", nil)
	srv.ServeOpenAIModels(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", w.Code)
	}
}

func TestServeOpenAIModelDetail(t *testing.T) {
	srv := testServer(testConfig(), nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/models/gemini-2.5-flash", nil)
	srv.ServeOpenAIModelDetail(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if body["id"] != "gemini-2.5-flash" {
		t.Fatalf("got %v", body["id"])
	}
}

func TestServeOpenAIModelDetail_NotFound(t *testing.T) {
	srv := testServer(testConfig(), nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/v1/models/nonexistent", nil)
	srv.ServeOpenAIModelDetail(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d", w.Code)
	}
}

func TestParseBody_Valid(t *testing.T) {
	srv := testServer(testConfig(), nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"key":"value"}`))
	var target map[string]string
	if !srv.parseBody(w, r, &target) {
		t.Fatal("expected true")
	}
	if target["key"] != "value" {
		t.Fatalf("got %v", target)
	}
}

func TestParseBody_InvalidJSON(t *testing.T) {
	srv := testServer(testConfig(), nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(`not json`))
	var target map[string]string
	if srv.parseBody(w, r, &target) {
		t.Fatal("expected false")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d", w.Code)
	}
}

func TestParseBody_TooLarge(t *testing.T) {
	srv := testServer(testConfig(), nil)
	big := `{"data": "` + strings.Repeat("x", 10*1024*1024+100) + `"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(big))
	var target map[string]string
	if srv.parseBody(w, r, &target) {
		t.Fatal("expected false")
	}
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d", w.Code)
	}
}

func TestParseBody_CustomMaxSize(t *testing.T) {
	cfg := testConfig()
	cfg.MaxBodySize = 100
	srv := testServer(cfg, nil)
	body := `{"data": "` + strings.Repeat("x", 200) + `"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(body))
	var target map[string]string
	if srv.parseBody(w, r, &target) {
		t.Fatal("expected false for oversized body")
	}
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d", w.Code)
	}
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, map[string]string{"hello": "world"})

	if w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("got %q", w.Header().Get("Content-Type"))
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["hello"] != "world" {
		t.Fatalf("got %v", body)
	}
}

func TestRoutes_Middleware(t *testing.T) {
	srv := testServer(testConfig(), vertex.NewDemoClient(testConfig()))
	handler := srv.Routes()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/health", nil)
	r.Header.Set("X-Request-ID", "test-123")
	handler.ServeHTTP(w, r)

	if w.Header().Get("X-Request-ID") != "test-123" {
		t.Fatalf("got %q", w.Header().Get("X-Request-ID"))
	}
	if w.Header().Get("X-Bulwarkai") != "test" {
		t.Fatalf("got %q", w.Header().Get("X-Bulwarkai"))
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("got %q", w.Header().Get("X-Content-Type-Options"))
	}
}

func TestRoutes_GeneratesRequestID(t *testing.T) {
	srv := testServer(testConfig(), vertex.NewDemoClient(testConfig()))
	handler := srv.Routes()

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/health", nil)
	handler.ServeHTTP(w, r)

	if w.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected request ID to be generated")
	}
}

func TestDemoMode_Anthropic(t *testing.T) {
	cfg := testConfig()
	cfg.DemoMode = true
	cfg.LocalMode = true
	demo := vertex.NewDemoClient(cfg)
	srv := testServer(cfg, demo)

	w := httptest.NewRecorder()
	body := `{"model":"gemini-2.5-flash","max_tokens":1024,"messages":[{"role":"user","content":"hello"}]}`
	r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	srv.ServeAnthropic(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestDemoMode_OpenAI(t *testing.T) {
	cfg := testConfig()
	cfg.DemoMode = true
	cfg.APIKeys = map[string]bool{"test": true}
	demo := vertex.NewDemoClient(cfg)
	srv := testServer(cfg, demo)

	w := httptest.NewRecorder()
	body := `{"model":"gemini-2.5-flash","max_tokens":1024,"messages":[{"role":"user","content":"hello"}]}`
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Api-Key", "test")

	srv.ServeOpenAI(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestDemoMode_VertexStream(t *testing.T) {
	cfg := testConfig()
	cfg.DemoMode = true
	cfg.LocalMode = true
	demo := vertex.NewDemoClient(cfg)
	srv := testServer(cfg, demo)

	w := httptest.NewRecorder()
	body := `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`
	r := httptest.NewRequest("POST", "/models/gemini-2.5-flash:streamGenerateContent", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	srv.ServeVertexCompat(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	resp := w.Body.String()
	if resp == "" {
		t.Fatal("expected stream data")
	}
}

func TestDemoClient_CallJSON(t *testing.T) {
	demo := vertex.NewDemoClient(testConfig())
	data, err := demo.CallJSON(nil, nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected data")
	}
}

func TestDemoClient_CallJSONForModel(t *testing.T) {
	demo := vertex.NewDemoClient(testConfig())
	data, err := demo.CallJSONForModel(nil, nil, "", "gemini-2.5-flash", false)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	candidates, ok := result["candidates"].([]interface{})
	if !ok || len(candidates) == 0 {
		t.Fatal("expected candidates")
	}
}

func TestDemoClient_CallStream(t *testing.T) {
	demo := vertex.NewDemoClient(testConfig())
	rc, err := demo.CallStream(nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected stream data")
	}
}

func TestDemoClient_CallStreamRaw(t *testing.T) {
	demo := vertex.NewDemoClient(testConfig())
	rc, err := demo.CallStreamRaw(nil, nil, "", "gemini-2.5-flash", "streamGenerateContent")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected stream data")
	}
}

type stubLimiter struct {
	allowed bool
	calls   int
}

func (s *stubLimiter) Allow(key string) bool {
	s.calls++
	return s.allowed
}

func TestRateLimitMiddleware_Rejects(t *testing.T) {
	rl := &stubLimiter{allowed: false}
	cfg := testConfig()
	chain := inspector.NewChain()
	srv := NewServer(cfg, chain, nil, http.DefaultClient, nil, nil, rl)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/health", nil)
	r.Header.Set("Authorization", "Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJlbWFpbCI6InVzZXJAZXhhbXBsZS5jb20iLCJlbWFpbF92ZXJpZmllZCI6dHJ1ZX0.fake")
	srv.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	if rl.calls != 1 {
		t.Fatalf("expected 1 rate limit check, got %d", rl.calls)
	}
}

func TestRateLimitMiddleware_Allows(t *testing.T) {
	rl := &stubLimiter{allowed: true}
	cfg := testConfig()
	chain := inspector.NewChain()
	srv := NewServer(cfg, chain, nil, http.DefaultClient, nil, nil, rl)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/health", nil)
	srv.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRateLimitMiddleware_Nil(t *testing.T) {
	cfg := testConfig()
	chain := inspector.NewChain()
	srv := NewServer(cfg, chain, nil, http.DefaultClient, nil, nil, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/health", nil)
	srv.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with nil limiter, got %d", w.Code)
	}
}

func TestHealthEndpoint_WithComponents(t *testing.T) {
	cfg := testConfig()
	chain := inspector.NewChain()
	pe, _ := policy.NewEngine(context.Background(), true, "", "package bulwarkai\n\ndefault allow := true\n")
	defer pe.Stop()
	wh := webhook.NewNotifier("http://example.com/webhook", "secret", 16)
	srv := NewServer(cfg, chain, nil, http.DefaultClient, pe, wh, &stubLimiter{allowed: true})

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/health", nil)
	srv.healthHandler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	opa, ok := body["opa"].(map[string]interface{})
	if !ok || opa["status"] != "enabled" {
		t.Errorf("expected opa.status=enabled, got %v", body["opa"])
	}
	if body["webhook"] != "enabled" {
		t.Errorf("expected webhook=enabled, got %q", body["webhook"])
	}
	if body["rate_limit"] != "enabled" {
		t.Errorf("expected rate_limit=enabled, got %q", body["rate_limit"])
	}
	if opa["info"] == nil || opa["info"] == "" {
		t.Error("expected opa.info field")
	}
}

func TestRequestMiddleware_LogsEmail(t *testing.T) {
	cfg := testConfig()
	cfg.LocalMode = true
	chain := inspector.NewChain()
	demo := vertex.NewDemoClient(cfg)
	srv := NewServer(cfg, chain, demo, http.DefaultClient, nil, nil, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/health", nil)
	srv.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCORSMiddleware_WithOrigin(t *testing.T) {
	cfg := testConfig()
	cfg.CORSOrigin = "https://bulwarkai.cloud"
	chain := inspector.NewChain()
	srv := NewServer(cfg, chain, nil, http.DefaultClient, nil, nil, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("OPTIONS", "/health", nil)
	r.Header.Set("Origin", "https://bulwarkai.cloud")
	srv.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "https://bulwarkai.cloud" {
		t.Errorf("expected CORS origin header, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
	if w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("expected Allow-Methods header")
	}
}

func TestCORSMiddleware_NoOrigin(t *testing.T) {
	cfg := testConfig()
	chain := inspector.NewChain()
	srv := NewServer(cfg, chain, nil, http.DefaultClient, nil, nil, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/health", nil)
	srv.Routes().ServeHTTP(w, r)

	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("expected no CORS header when CORS_ORIGIN empty, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSMiddleware_PassThrough(t *testing.T) {
	cfg := testConfig()
	cfg.CORSOrigin = "https://bulwarkai.cloud"
	chain := inspector.NewChain()
	srv := NewServer(cfg, chain, nil, http.DefaultClient, nil, nil, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/health", nil)
	r.Header.Set("Origin", "https://bulwarkai.cloud")
	srv.Routes().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "https://bulwarkai.cloud" {
		t.Errorf("expected CORS origin on GET too, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
}
