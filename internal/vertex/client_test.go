package vertex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Daviey/bulwarkai/internal/config"
)

func testVCfg(ts *httptest.Server) *config.Config {
	cfg := &config.Config{
		Project:             "test-project",
		Location:            "europe-west2",
		FallbackGeminiModel: "gemini-2.5-flash",
	}
	if ts != nil {
		cfg.VertexBase = ts.URL
	} else {
		cfg.VertexBase = "https://europe-west2-aiplatform.googleapis.com/v1"
	}
	return cfg
}

func TestClient_CallJSON_OK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("got auth %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("x-goog-user-project") != "test-project" {
			t.Errorf("got project %q", r.Header.Get("x-goog-user-project"))
		}
		w.Write([]byte(`{"result":"ok"}`))
	}))
	defer ts.Close()

	c := NewClient(testVCfg(ts), ts.Client())
	data, err := c.CallJSON(context.Background(), map[string]interface{}{"prompt": "hi"}, "test-token", false)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	if result["result"] != "ok" {
		t.Fatalf("got %v", result)
	}
}

func TestClient_CallJSON_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer ts.Close()

	c := NewClient(testVCfg(ts), ts.Client())
	_, err := c.CallJSON(context.Background(), nil, "token", false)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClient_CallJSONForModel_OK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"model":"custom-model"}`))
	}))
	defer ts.Close()

	c := NewClient(testVCfg(ts), ts.Client())
	data, err := c.CallJSONForModel(context.Background(), nil, "token", "custom-model", false)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	if result["model"] != "custom-model" {
		t.Fatalf("got %v", result)
	}
}

func TestClient_CallStream_OK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"chunk":1}`))
	}))
	defer ts.Close()

	c := NewClient(testVCfg(ts), ts.Client())
	rc, err := c.CallStream(context.Background(), nil, "token")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"chunk":1}` {
		t.Fatalf("got %q", data)
	}
}

func TestClient_CallStream_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`rate limited`))
	}))
	defer ts.Close()

	c := NewClient(testVCfg(ts), ts.Client())
	_, err := c.CallStream(context.Background(), nil, "token")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClient_CallStreamRaw_OK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"raw":true}`))
	}))
	defer ts.Close()

	c := NewClient(testVCfg(ts), ts.Client())
	rc, err := c.CallStreamRaw(context.Background(), nil, "token", "my-model", "streamGenerateContent")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
}

func TestClient_ResolveToken_AccessToken(t *testing.T) {
	c := NewClient(testVCfg(nil), nil)
	if got := c.resolveToken("explicit"); got != "explicit" {
		t.Fatalf("got %q", got)
	}
}

func TestClient_ResolveToken_ADC(t *testing.T) {
	c := NewClient(testVCfg(nil), nil)
	c.SetADCTokenFunc(func() string { return "adc-token" })
	if got := c.resolveToken(""); got != "adc-token" {
		t.Fatalf("got %q", got)
	}
}

func TestClient_ResolveToken_None(t *testing.T) {
	c := NewClient(testVCfg(nil), nil)
	if got := c.resolveToken(""); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestClient_BuildVertexURL(t *testing.T) {
	cfg := &config.Config{
		VertexBase:          "https://europe-west2-aiplatform.googleapis.com/v1",
		FallbackGeminiModel: "gemini-2.5-flash",
	}
	c := NewClient(cfg, nil)

	got := c.buildVertexURL(false)
	want := "https://europe-west2-aiplatform.googleapis.com/v1/publishers/google/models/gemini-2.5-flash:generateContent"
	if got != want {
		t.Fatalf("got %q", got)
	}

	got = c.buildVertexURL(true)
	want = "https://europe-west2-aiplatform.googleapis.com/v1/publishers/google/models/gemini-2.5-flash:streamGenerateContent"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestDemoClient_ImplementsInterface(t *testing.T) {
	var _ VertexCaller = (*DemoClient)(nil)
	var _ VertexCaller = (*Client)(nil)
}

func TestVertexError_Error(t *testing.T) {
	ve := newVertexError(429, "quota exceeded")
	if ve.Error() != "vertex returned 429: quota exceeded" {
		t.Fatalf("got %q", ve.Error())
	}
	if ve.StatusCode != 429 {
		t.Fatalf("got %d", ve.StatusCode)
	}
}

func TestVertexError_TruncatesBody(t *testing.T) {
	longBody := strings.Repeat("x", 1000)
	ve := newVertexError(500, longBody)
	if len(ve.Body) != 500 {
		t.Fatalf("expected 500 chars, got %d", len(ve.Body))
	}
}

func TestShouldTripBreaker(t *testing.T) {
	if !shouldTripBreaker(newVertexError(500, "internal")) {
		t.Error("500 should trip breaker")
	}
	if !shouldTripBreaker(newVertexError(0, "network")) {
		t.Error("network error (0) should trip breaker")
	}
	if shouldTripBreaker(newVertexError(400, "bad request")) {
		t.Error("400 should not trip breaker")
	}
	if shouldTripBreaker(newVertexError(401, "unauthorized")) {
		t.Error("401 should not trip breaker")
	}
	if shouldTripBreaker(newVertexError(429, "quota")) {
		t.Error("429 should not trip breaker")
	}
}
