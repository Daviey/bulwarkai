package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Daviey/bulwarkai/internal/auth"
	"github.com/Daviey/bulwarkai/internal/config"
	"github.com/Daviey/bulwarkai/internal/handler"
	"github.com/Daviey/bulwarkai/internal/inspector"
	"github.com/Daviey/bulwarkai/internal/streaming"
	"github.com/Daviey/bulwarkai/internal/translate"
	"github.com/Daviey/bulwarkai/internal/vertex"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go/v3"
)

func ptr[T any](v T) *T { return &v }

func stubVertexServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func stubVertexOK(responseJSON string) *httptest.Server {
	return stubVertexServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(responseJSON))
	})
}

func stubVertexStream(data string) *httptest.Server {
	return stubVertexServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(data))
	})
}

func makeGeminiResponse(text, finishReason string) string {
	return fmt.Sprintf(`{
		"candidates": [{
			"content": {"role": "model", "parts": [{"text": %q}]},
			"finishReason": %q
		}],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 5,
			"totalTokenCount": 15
		}
	}`, text, finishReason)
}

func newTestServer(vertexTS *httptest.Server) *handler.Server {
	testCfg := &config.Config{
		Project:             "test-project",
		Location:            "europe-west2",
		AllowedDomains:      []string{"example.com", "test.com"},
		FallbackGeminiModel: "gemini-2.5-flash",
		ResponseMode:        "strict",
		ModelArmorTemplate:  "test-template",
		ModelArmorLocation:  "europe-west2",
		ModelArmorEndpoint:  "https://unused.example.com",
		APIKeys:             map[string]bool{"test-api-key": true},
		TokenInfoURL:        "https://oauth2.googleapis.com",
		Version:             "test",
	}
	var httpClient *http.Client
	if vertexTS != nil {
		testCfg.VertexBase = vertexTS.URL
		httpClient = vertexTS.Client()
	} else {
		testCfg.VertexBase = "https://unused.example.com/v1"
		httpClient = http.DefaultClient
	}
	chain := inspector.NewChain(inspector.NewRegexInspector())
	vc := vertex.NewClient(testCfg, httpClient)
	return handler.NewServer(testCfg, chain, vc, httpClient, nil)
}

func newCustomServer(cfg *config.Config, httpClient *http.Client) *handler.Server {
	chain := inspector.NewChain(inspector.NewRegexInspector())
	vc := vertex.NewClient(cfg, httpClient)
	return handler.NewServer(cfg, chain, vc, httpClient, nil)
}

func validAuthHeaders() http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	h.Set("X-Forwarded-Access-Token", "fake-access-token")
	h.Set("Content-Type", "application/json")
	return h
}

func makeTestJWT(email string) string {
	header := base64Encode(`{"alg":"RS256","typ":"JWT"}`)
	payload := base64Encode(fmt.Sprintf(`{"email":"%s","email_verified":true}`, email))
	return header + "." + payload + ".fakesig"
}

func base64Encode(s string) string {
	return strings.TrimRight(
		strings.ReplaceAll(
			strings.ReplaceAll(
				toBase64([]byte(s)), "+", "-"), "/", "_"), "=")
}

func toBase64(data []byte) string {
	var buf bytes.Buffer
	encoder := base64.NewEncoder(base64.StdEncoding, &buf)
	encoder.Write(data)
	encoder.Close()
	return buf.String()
}

func testGeminiStreamChunks() string {
	return "[{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Hi\"}]}}]},\n" +
		"{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\" there\"}]}," +
		"\"finishReason\":\"STOP\"}]}]\n"
}

func testGeminiStreamChunksNoFinish() string {
	return "[{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Hi\"}]}}]},\n" +
		"{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\" there\"}]}}]}]\n"
}

func inspectorNames(inspectors []inspector.Inspector) []string {
	names := make([]string, len(inspectors))
	for i, insp := range inspectors {
		names[i] = insp.Name()
	}
	return names
}

func TestRegexInspectorPromptSSN(t *testing.T) {
	insp := inspector.NewRegexInspector()
	tests := []struct {
		input   string
		blocked bool
	}{
		{"My SSN is 123-45-6789", true},
		{"SSN: 987.65.4321", true},
		{"SSN: 987 65 4321", false},
		{"Hello world", false},
		{"The number 123 is fine", false},
	}
	for _, tt := range tests {
		br := insp.InspectPrompt(context.Background(), tt.input, "")
		if (br != nil) != tt.blocked {
			t.Errorf("InspectPrompt(%q) blocked=%v, want %v", tt.input, br != nil, tt.blocked)
		}
	}
}

func TestRegexInspectorPromptCreditCard(t *testing.T) {
	insp := inspector.NewRegexInspector()
	br := insp.InspectPrompt(context.Background(), "card 1234567890123456", "")
	if br == nil {
		t.Error("expected credit card to be blocked")
	}
	if br != nil && !strings.Contains(br.Reason, "Credit card") {
		t.Errorf("expected credit card reason, got: %s", br.Reason)
	}
}

func TestRegexInspectorPromptPrivateKey(t *testing.T) {
	insp := inspector.NewRegexInspector()
	br := insp.InspectPrompt(context.Background(), "-----BEGIN RSA PRIVATE KEY-----\nabc", "")
	if br == nil {
		t.Error("expected private key to be blocked")
	}
}

func TestRegexInspectorPromptAWSKey(t *testing.T) {
	insp := inspector.NewRegexInspector()
	br := insp.InspectPrompt(context.Background(), "key=AKIAIOSFODNN7EXAMPLE", "")
	if br == nil {
		t.Error("expected AWS key to be blocked")
	}
}

func TestRegexInspectorPromptAPIKey(t *testing.T) {
	insp := inspector.NewRegexInspector()
	br := insp.InspectPrompt(context.Background(), "sk-abcdefghijklmnopqrstuvwxyz123456", "")
	if br == nil {
		t.Error("expected API key to be blocked")
	}
}

func TestRegexInspectorPromptCredentials(t *testing.T) {
	insp := inspector.NewRegexInspector()
	br := insp.InspectPrompt(context.Background(), "my email is user@example.com and password is secret", "")
	if br == nil {
		t.Error("expected credentials to be blocked")
	}
}

func TestRegexInspectorResponseSSN(t *testing.T) {
	insp := inspector.NewRegexInspector()
	br := insp.InspectResponse(context.Background(), "Your SSN is 123-45-6789", "")
	if br == nil {
		t.Error("expected SSN in response to be blocked")
	}
}

func TestRegexInspectorResponsePrivateKey(t *testing.T) {
	insp := inspector.NewRegexInspector()
	br := insp.InspectResponse(context.Background(), "-----BEGIN OPENSSH PRIVATE KEY-----\nxxx", "")
	if br == nil {
		t.Error("expected private key in response to be blocked")
	}
}

func TestRegexInspectorName(t *testing.T) {
	insp := inspector.NewRegexInspector()
	if insp.Name() != "regex" {
		t.Errorf("expected name 'regex', got %q", insp.Name())
	}
}

func TestInspectorChainOrder(t *testing.T) {
	os.Setenv("DLP_API", "true")
	defer os.Unsetenv("DLP_API")

	testCfg := &config.Config{
		Project:            "test-project",
		Location:           "europe-west2",
		ResponseMode:       "strict",
		ModelArmorTemplate: "test-template",
		ModelArmorLocation: "europe-west2",
	}
	var insps []inspector.Inspector
	insps = append(insps, inspector.NewRegexInspector())
	if testCfg.ModelArmorTemplate != "" && testCfg.ResponseMode != "strict" {
		insps = append(insps, inspector.NewModelArmorInspector(testCfg, http.DefaultClient))
	}
	if os.Getenv("DLP_API") == "true" {
		insps = append(insps, inspector.NewDLPInspector(testCfg, http.DefaultClient))
	}

	if len(insps) != 2 {
		t.Fatalf("expected 2 inspectors (regex + dlp in strict with DLP_API), got %d", len(insps))
	}
	if insps[0].Name() != "regex" {
		t.Errorf("expected first inspector to be regex, got %s", insps[0].Name())
	}
}

func TestInspectorChainStopsOnFirstBlock(t *testing.T) {
	chain := inspector.NewChain(inspector.NewRegexInspector())
	br := chain.ScreenPrompt(context.Background(), "My SSN is 123-45-6789", "token")
	if br == nil {
		t.Error("expected prompt to be blocked")
	}
	if !strings.Contains(br.Reason, "SSN") {
		t.Errorf("expected SSN reason, got: %s", br.Reason)
	}
}

func TestInspectorChainAllowsCleanPrompt(t *testing.T) {
	chain := inspector.NewChain(inspector.NewRegexInspector())
	br := chain.ScreenPrompt(context.Background(), "What is 2+2?", "token")
	if br != nil {
		t.Errorf("expected clean prompt to pass, got blocked: %s", br.Reason)
	}
}

func TestInspectorChainEmptyPrompt(t *testing.T) {
	chain := inspector.NewChain(inspector.NewRegexInspector())
	br := chain.ScreenPrompt(context.Background(), "", "token")
	if br != nil {
		t.Error("expected empty prompt to pass")
	}
}

func TestTranslateAnthropicToGemini(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Hello"},
			map[string]interface{}{"role": "assistant", "content": "Hi there"},
			map[string]interface{}{"role": "user", "content": "How are you?"},
		},
	}
	gc := map[string]interface{}{"maxOutputTokens": float64(100)}
	result := translate.TranslateToGemini(body, "You are helpful", gc)

	contents, ok := result["contents"].([]interface{})
	if !ok || len(contents) != 5 {
		t.Fatalf("expected 5 content blocks (2 system + 3 messages), got %d", len(contents))
	}

	sysUser := contents[0].(map[string]interface{})
	if sysUser["role"] != "user" {
		t.Error("expected first system block to have role 'user'")
	}
	sysModel := contents[1].(map[string]interface{})
	if sysModel["role"] != "model" {
		t.Error("expected second system block to have role 'model'")
	}

	userMsg := contents[2].(map[string]interface{})
	if userMsg["role"] != "user" {
		t.Error("expected user message role")
	}
	assistantMsg := contents[3].(map[string]interface{})
	if assistantMsg["role"] != "model" {
		t.Error("expected assistant message to map to 'model' role")
	}

	genConfig, ok := result["generationConfig"].(map[string]interface{})
	if !ok {
		t.Fatal("expected generationConfig")
	}
	if genConfig["maxOutputTokens"] != float64(100) {
		t.Error("expected maxOutputTokens to be preserved")
	}
}

func TestTranslateAnthropicToGeminiNoSystem(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Hello"},
		},
	}
	result := translate.TranslateToGemini(body, "", nil)
	contents := result["contents"].([]interface{})
	if len(contents) != 1 {
		t.Fatalf("expected 1 content block (no system), got %d", len(contents))
	}
}

func TestTranslateAnthropicContentBlocks(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "Hello"},
					map[string]interface{}{"type": "image", "source": map[string]interface{}{}},
				},
			},
		},
	}
	result := translate.TranslateToGemini(body, "", nil)
	contents := result["contents"].([]interface{})
	if len(contents) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(contents))
	}
	parts := contents[0].(map[string]interface{})["parts"].([]interface{})
	if len(parts) != 1 {
		t.Fatalf("expected 1 part (text only), got %d", len(parts))
	}
}

func TestTranslateGeminiToAnthropic(t *testing.T) {
	vertexResp := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"role":  "model",
					"parts": []interface{}{map[string]interface{}{"text": "Hello world"}},
				},
				"finishReason": "STOP",
			},
		},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     float64(10),
			"candidatesTokenCount": float64(5),
		},
	}

	msg := translate.TranslateGeminiToAnthropic(vertexResp, "gemini-2.5-flash")

	if msg["type"] != "message" {
		t.Error("expected type=message")
	}
	if msg["role"] != "assistant" {
		t.Error("expected role=assistant")
	}
	if msg["stop_reason"] != "end_turn" {
		t.Error("expected stop_reason=end_turn for STOP")
	}
	if msg["model"] != "gemini-2.5-flash" {
		t.Error("expected model preserved")
	}

	id, ok := msg["id"].(string)
	if !ok || !strings.HasPrefix(id, "msg_") {
		t.Error("expected id to start with msg_")
	}

	content, ok := msg["content"].([]interface{})
	if !ok || len(content) != 1 {
		t.Fatal("expected content array with 1 block")
	}
	block := content[0].(map[string]interface{})
	if block["type"] != "text" {
		t.Error("expected content block type=text")
	}
	if block["text"] != "Hello world" {
		t.Error("expected text preserved")
	}
}

func TestTranslateGeminiToAnthropicFinishReasons(t *testing.T) {
	tests := []struct {
		finish   string
		expected string
	}{
		{"STOP", "end_turn"},
		{"MAX_TOKENS", "max_tokens"},
		{"SAFETY", "stop_sequence"},
	}
	for _, tt := range tests {
		vertexResp := map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content":      map[string]interface{}{"role": "model", "parts": []interface{}{map[string]interface{}{"text": "x"}}},
					"finishReason": tt.finish,
				},
			},
			"usageMetadata": map[string]interface{}{},
		}
		msg := translate.TranslateGeminiToAnthropic(vertexResp, "model")
		if msg["stop_reason"] != tt.expected {
			t.Errorf("finishReason=%q → stop_reason=%q, want %q", tt.finish, msg["stop_reason"], tt.expected)
		}
	}
}

func TestTranslateGeminiToOpenAI(t *testing.T) {
	vertexResp := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"role":  "model",
					"parts": []interface{}{map[string]interface{}{"text": "Hello world"}},
				},
				"finishReason": "STOP",
			},
		},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     float64(10),
			"candidatesTokenCount": float64(5),
			"totalTokenCount":      float64(15),
		},
	}

	msg := translate.TranslateGeminiToOpenAI(vertexResp, "gemini-2.5-flash")

	if msg["object"] != "chat.completion" {
		t.Error("expected object=chat.completion")
	}
	if msg["model"] != "gemini-2.5-flash" {
		t.Error("expected model preserved")
	}

	id, ok := msg["id"].(string)
	if !ok || !strings.HasPrefix(id, "chatcmpl-") {
		t.Error("expected id to start with chatcmpl-")
	}

	choices, ok := msg["choices"].([]interface{})
	if !ok || len(choices) != 1 {
		t.Fatal("expected 1 choice")
	}
	choice := choices[0].(map[string]interface{})
	if choice["finish_reason"] != "stop" {
		t.Error("expected finish_reason=stop")
	}
	if choice["index"] != 0 {
		t.Error("expected index=0")
	}
	message := choice["message"].(map[string]interface{})
	if message["role"] != "assistant" {
		t.Error("expected assistant role")
	}
	if message["content"] != "Hello world" {
		t.Error("expected content preserved")
	}

	usage := msg["usage"].(map[string]interface{})
	if usage["prompt_tokens"] != 10 {
		t.Errorf("expected prompt_tokens=10, got %v", usage["prompt_tokens"])
	}
	if usage["completion_tokens"] != 5 {
		t.Errorf("expected completion_tokens=5, got %v", usage["completion_tokens"])
	}
	if usage["total_tokens"] != 15 {
		t.Errorf("expected total_tokens=15, got %v", usage["total_tokens"])
	}
}

func TestTranslateGeminiToOpenAIFinishReasons(t *testing.T) {
	tests := []struct {
		finish   string
		expected string
	}{
		{"STOP", "stop"},
		{"MAX_TOKENS", "length"},
		{"SAFETY", "content_filter"},
	}
	for _, tt := range tests {
		vertexResp := map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content":      map[string]interface{}{"role": "model", "parts": []interface{}{map[string]interface{}{"text": "x"}}},
					"finishReason": tt.finish,
				},
			},
			"usageMetadata": map[string]interface{}{},
		}
		msg := translate.TranslateGeminiToOpenAI(vertexResp, "model")
		choices := msg["choices"].([]interface{})
		choice := choices[0].(map[string]interface{})
		if choice["finish_reason"] != tt.expected {
			t.Errorf("finishReason=%q → finish_reason=%v, want %q", tt.finish, choice["finish_reason"], tt.expected)
		}
	}
}

func TestAnthropicSDKCompliance(t *testing.T) {
	vertexResp := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"role":  "model",
					"parts": []interface{}{map[string]interface{}{"text": "Hello from Claude proxy"}},
				},
				"finishReason": "STOP",
			},
		},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     float64(42),
			"candidatesTokenCount": float64(7),
		},
	}

	msg := translate.TranslateGeminiToAnthropic(vertexResp, "gemini-2.5-flash")
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var anthropicMsg anthropic.Message
	if err := json.Unmarshal(data, &anthropicMsg); err != nil {
		t.Fatalf("failed to unmarshal into Anthropic SDK type: %v\nJSON: %s", err, string(data))
	}

	if anthropicMsg.ID == "" {
		t.Error("expected non-empty ID")
	}
	if anthropicMsg.Type != "message" {
		t.Errorf("expected type=message, got %q", anthropicMsg.Type)
	}
	if anthropicMsg.Role != "assistant" {
		t.Errorf("expected role=assistant, got %q", anthropicMsg.Role)
	}
	if len(anthropicMsg.Content) == 0 {
		t.Fatal("expected content blocks")
	}
	textBlock := anthropicMsg.Content[0]
	if textBlock.Text != "Hello from Claude proxy" {
		t.Errorf("expected text preserved, got %q", textBlock.Text)
	}
	if anthropicMsg.StopReason != anthropic.StopReasonEndTurn {
		t.Errorf("expected stop_reason=end_turn, got %q", anthropicMsg.StopReason)
	}
	if anthropicMsg.Usage.InputTokens != 42 {
		t.Errorf("expected input_tokens=42, got %d", anthropicMsg.Usage.InputTokens)
	}
	if anthropicMsg.Usage.OutputTokens != 7 {
		t.Errorf("expected output_tokens=7, got %d", anthropicMsg.Usage.OutputTokens)
	}
}

func TestOpenAISDKCompliance(t *testing.T) {
	vertexResp := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"role":  "model",
					"parts": []interface{}{map[string]interface{}{"text": "Hello from OpenAI proxy"}},
				},
				"finishReason": "STOP",
			},
		},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     float64(30),
			"candidatesTokenCount": float64(8),
			"totalTokenCount":      float64(38),
		},
	}

	msg := translate.TranslateGeminiToOpenAI(vertexResp, "gemini-2.5-flash")
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var openaiMsg openai.ChatCompletion
	if err := json.Unmarshal(data, &openaiMsg); err != nil {
		t.Fatalf("failed to unmarshal into OpenAI SDK type: %v\nJSON: %s", err, string(data))
	}

	if openaiMsg.ID == "" {
		t.Error("expected non-empty ID")
	}
	if openaiMsg.Object != "chat.completion" {
		t.Errorf("expected object=chat.completion, got %q", openaiMsg.Object)
	}
	if len(openaiMsg.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(openaiMsg.Choices))
	}
	choice := openaiMsg.Choices[0]
	if choice.Message.Content != "Hello from OpenAI proxy" {
		t.Errorf("expected content preserved, got %q", choice.Message.Content)
	}
	if choice.FinishReason != "stop" {
		t.Errorf("expected finish_reason=stop, got %q", choice.FinishReason)
	}
	if openaiMsg.Usage.PromptTokens != 30 {
		t.Errorf("expected prompt_tokens=30, got %d", openaiMsg.Usage.PromptTokens)
	}
	if openaiMsg.Usage.CompletionTokens != 8 {
		t.Errorf("expected completion_tokens=8, got %d", openaiMsg.Usage.CompletionTokens)
	}
}

func TestExtractEmailFromJWT(t *testing.T) {
	tests := []struct {
		token string
		email string
	}{
		{makeTestJWT("user@example.com"), "user@example.com"},
		{makeTestJWT("admin@test.com"), "admin@test.com"},
		{"not.a.jwt", ""},
		{"", ""},
		{"a.b", ""},
	}
	for _, tt := range tests {
		result := auth.ExtractEmailFromJWT(tt.token)
		if result != tt.email {
			t.Errorf("ExtractEmailFromJWT(%q) = %q, want %q", tt.token[:min(len(tt.token), 20)], result, tt.email)
		}
	}
}

func TestDomainAllowlist(t *testing.T) {
	domains := []string{"example.com", "test.com"}

	tests := []struct {
		email string
		ok    bool
	}{
		{"user@example.com", true},
		{"admin@test.com", true},
		{"user@evil.com", false},
		{"user@MAPLEQUAD.COM", false},
		{"invalid-email", false},
	}
	for _, tt := range tests {
		parts := strings.SplitN(tt.email, "@", 2)
		allowed := len(parts) == 2 && config.Contains(domains, parts[1])
		if allowed != tt.ok {
			t.Errorf("domain check for %q = %v, want %v", tt.email, allowed, tt.ok)
		}
	}
}

func TestAPIKeyAuth(t *testing.T) {
	testCfg := &config.Config{
		AllowedDomains: []string{"example.com"},
		APIKeys:        map[string]bool{"valid-key": true},
	}

	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("X-Api-Key", "valid-key")
	rec := httptest.NewRecorder()

	identity, ok := auth.Authenticate(testCfg, http.DefaultClient, rec, req)
	if !ok {
		t.Fatal("expected API key auth to succeed")
	}
	if identity.Email != "apikey@example.com" {
		t.Errorf("expected apikey email, got %q", identity.Email)
	}
}

func TestAPIKeyAuthInvalid(t *testing.T) {
	testCfg := &config.Config{
		AllowedDomains: []string{"example.com"},
		APIKeys:        map[string]bool{"valid-key": true},
	}

	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("X-Api-Key", "invalid-key")
	rec := httptest.NewRecorder()

	_, ok := auth.Authenticate(testCfg, http.DefaultClient, rec, req)
	if ok {
		t.Error("expected invalid API key to fail")
	}
}

func TestMissingAuth(t *testing.T) {
	testCfg := &config.Config{AllowedDomains: []string{"example.com"}, APIKeys: map[string]bool{}}

	req := httptest.NewRequest("POST", "/v1/messages", nil)
	rec := httptest.NewRecorder()

	_, ok := auth.Authenticate(testCfg, http.DefaultClient, rec, req)
	if ok {
		t.Error("expected missing auth to fail")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestParseGeminiChunk(t *testing.T) {
	tests := []struct {
		line   string
		text   string
		finish string
	}{
		{
			`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Hello"}]}}]}`,
			"Hello", "",
		},
		{
			`data: {"candidates":[{"content":{"role":"model","parts":[{"text":""}]},"finishReason":"STOP"}]}`,
			"", "STOP",
		},
		{
			`[{"candidates":[{"content":{"role":"model","parts":[{"text":"Hi"}]}}]},`,
			"Hi", "",
		},
		{
			`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Bye"}]}}]}`,
			"Bye", "",
		},
		{
			"",
			"", "",
		},
		{
			"[]",
			"", "",
		},
		{
			"[",
			"", "",
		},
		{
			"]",
			"", "",
		},
		{
			`bare json without wrapper`,
			"", "",
		},
	}
	for _, tt := range tests {
		text, finish, _ := streaming.ParseGeminiChunk(tt.line)
		if text != tt.text || finish != tt.finish {
			t.Errorf("ParseGeminiChunk(%q) = (%q, %q), want (%q, %q)",
				tt.line, text, finish, tt.text, tt.finish)
		}
	}
}

func TestStreamGeminiAsAnthropic(t *testing.T) {
	chunks := "[{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Hello\"}]}}]},\n" +
		"{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\" world\"}]}}]},\n" +
		"{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"\"}]}," +
		"\"finishReason\":\"STOP\"}]}]\n"

	rc := io.NopCloser(strings.NewReader(chunks))
	var buf bytes.Buffer
	accumulated := streaming.StreamGeminiAsAnthropic(&buf, rc, "gemini-2.5-flash", nil)

	if accumulated != "Hello world" {
		t.Errorf("expected accumulated 'Hello world', got %q", accumulated)
	}

	output := buf.String()
	if !strings.Contains(output, "event: message_start") {
		t.Error("expected message_start event")
	}
	if !strings.Contains(output, "event: content_block_delta") {
		t.Error("expected content_block_delta events")
	}
	if !strings.Contains(output, "event: message_stop") {
		t.Error("expected message_stop event")
	}
	if !strings.Contains(output, `"text_delta"`) {
		t.Error("expected text_delta type in SSE")
	}
}

func TestStreamGeminiAsOpenAI(t *testing.T) {
	chunks := "[{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Hi\"}]}}]},\n" +
		"{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"!\"}]}}]},\n" +
		"{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"\"}]}," +
		"\"finishReason\":\"STOP\"}]}]\n"

	rc := io.NopCloser(strings.NewReader(chunks))
	var buf bytes.Buffer
	accumulated := streaming.StreamGeminiAsOpenAI(&buf, rc, "gemini-2.5-flash", nil)

	if accumulated != "Hi!" {
		t.Errorf("expected accumulated 'Hi!', got %q", accumulated)
	}

	output := buf.String()
	if !strings.Contains(output, `"chat.completion.chunk"`) {
		t.Error("expected chat.completion.chunk object type")
	}
	if !strings.Contains(output, `[DONE]`) {
		t.Error("expected [DONE] sentinel")
	}
}

func TestStreamCapture(t *testing.T) {
	chunks := "[{\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"captured\"}]}}]}]\n"
	rc := io.NopCloser(strings.NewReader(chunks))
	var buf bytes.Buffer
	var captured string
	streaming.StreamGeminiAsAnthropic(&buf, rc, "model", &captured)
	if captured != "captured" {
		t.Errorf("expected capture, got %q", captured)
	}
}

func TestExtractGenerationConfig(t *testing.T) {
	body := map[string]interface{}{
		"max_tokens":        float64(4096),
		"temperature":       float64(0.7),
		"top_p":             float64(0.9),
		"top_k":             float64(40),
		"frequency_penalty": float64(0.1),
		"presence_penalty":  float64(0.2),
		"stop_sequences":    []interface{}{"END", "STOP"},
	}

	gc := translate.ExtractGenerationConfig(body)

	if gc["maxOutputTokens"] != float64(4096) {
		t.Errorf("expected maxOutputTokens=4096, got %v", gc["maxOutputTokens"])
	}
	if gc["temperature"] != float64(0.7) {
		t.Errorf("expected temperature=0.7, got %v", gc["temperature"])
	}
	if gc["topP"] != float64(0.9) {
		t.Errorf("expected topP=0.9, got %v", gc["topP"])
	}
	if gc["topK"] != float64(40) {
		t.Errorf("expected topK=40, got %v", gc["topK"])
	}
	if gc["frequencyPenalty"] != float64(0.1) {
		t.Errorf("expected frequencyPenalty=0.1")
	}
	if gc["presencePenalty"] != float64(0.2) {
		t.Errorf("expected presencePenalty=0.2")
	}
	stopSeqs, ok := gc["stopSequences"].([]interface{})
	if !ok || len(stopSeqs) != 2 {
		t.Errorf("expected 2 stop sequences, got %v", gc["stopSequences"])
	}
}

func TestExtractGenerationConfigStopString(t *testing.T) {
	body := map[string]interface{}{
		"stop": "END",
	}
	gc := translate.ExtractGenerationConfig(body)
	stopSeqs, ok := gc["stopSequences"].([]string)
	if !ok || len(stopSeqs) != 1 || stopSeqs[0] != "END" {
		t.Errorf("expected stop string to become stopSequences, got %v", gc["stopSequences"])
	}
}

func TestExtractGenerationConfigEmpty(t *testing.T) {
	gc := translate.ExtractGenerationConfig(map[string]interface{}{})
	if len(gc) != 0 {
		t.Errorf("expected empty config, got %v", gc)
	}
}

func TestExtractAnthropicPrompt(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Hello"},
			map[string]interface{}{"role": "assistant", "content": "Hi"},
			map[string]interface{}{"role": "user", "content": "How are you?"},
		},
	}
	prompt := translate.ExtractAnthropicPrompt(body)
	if prompt != "Hello Hi How are you?" {
		t.Errorf("expected joined prompt, got %q", prompt)
	}
}

func TestExtractAnthropicPromptContentBlocks(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "part1"},
					map[string]interface{}{"type": "text", "text": "part2"},
				},
			},
		},
	}
	prompt := translate.ExtractAnthropicPrompt(body)
	if prompt != "part1 part2" {
		t.Errorf("expected 'part1 part2', got %q", prompt)
	}
}

func TestExtractOpenAIPromptGeminiContents(t *testing.T) {
	body := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role":  "user",
				"parts": []interface{}{map[string]interface{}{"text": "Hello from Gemini"}},
			},
		},
	}
	prompt := translate.ExtractOpenAIPrompt(body)
	if prompt != "Hello from Gemini" {
		t.Errorf("expected 'Hello from Gemini', got %q", prompt)
	}
}

func TestExtractPromptEmpty(t *testing.T) {
	prompt := translate.ExtractAnthropicPrompt(map[string]interface{}{})
	if prompt != "" {
		t.Errorf("expected empty prompt, got %q", prompt)
	}
}

func TestExtractMessageTextString(t *testing.T) {
	msg := map[string]interface{}{"content": "simple text"}
	result := translate.ExtractMessageText(msg)
	if result != "simple text" {
		t.Errorf("expected 'simple text', got %q", result)
	}
}

func TestExtractMessageTextContentBlocks(t *testing.T) {
	msg := map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "block1"},
			"plain string block",
			map[string]interface{}{"type": "image"},
		},
	}
	result := translate.ExtractMessageText(msg)
	if result != "block1 plain string block" {
		t.Errorf("expected 'block1 plain string block', got %q", result)
	}
}

func TestVertexCompatModelExtraction(t *testing.T) {
	tests := []struct {
		path          string
		expectedModel string
	}{
		{"/models/gemini-2.5-flash:generateContent", "gemini-2.5-flash"},
		{"/models/gemini-2.5-flash:streamGenerateContent", "gemini-2.5-flash"},
		{"/models/gemini-2.5-pro:generateContent", "gemini-2.5-pro"},
		{"/models/custom-model:streamGenerateContent", "custom-model"},
	}
	for _, tt := range tests {
		idx := strings.Index(tt.path, "/models/")
		if idx < 0 {
			continue
		}
		modelAndAction := tt.path[idx+len("/models/"):]
		colonIdx := strings.LastIndex(modelAndAction, ":")
		model := modelAndAction
		if colonIdx >= 0 {
			model = modelAndAction[:colonIdx]
		}
		if model != tt.expectedModel {
			t.Errorf("path %q → model %q, want %q", tt.path, model, tt.expectedModel)
		}
	}
}

func TestHealthHandler(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var body map[string]string
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", body["status"])
	}
	if body["mode"] != "strict" {
		t.Errorf("expected mode=strict, got %q", body["mode"])
	}
	if body["version"] == "" {
		t.Error("expected version field in health response")
	}
}

func TestModeAliases(t *testing.T) {
	os.Setenv("RESPONSE_MODE", "input_only")
	cfg := config.Load()
	if cfg.ResponseMode != "fast" {
		t.Errorf("expected input_only → fast, got %q", cfg.ResponseMode)
	}
	os.Unsetenv("RESPONSE_MODE")

	os.Setenv("RESPONSE_MODE", "buffer")
	cfg = config.Load()
	if cfg.ResponseMode != "audit" {
		t.Errorf("expected buffer → audit, got %q", cfg.ResponseMode)
	}
	os.Unsetenv("RESPONSE_MODE")
}

func TestAnthropicHandlerBlocksSSN(t *testing.T) {
	srv := newTestServer(nil)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"My SSN is 123-45-6789"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeAnthropic(rec, req)

	var body map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["type"] != "error" {
		t.Error("expected error response")
	}
	errObj := body["error"].(map[string]interface{})
	if !strings.Contains(errObj["message"].(string), "SSN") {
		t.Errorf("expected SSN in error message, got %v", errObj["message"])
	}
}

func TestOpenAIHandlerBlocksSSN(t *testing.T) {
	srv := newTestServer(nil)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"My SSN is 123-45-6789"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeOpenAI(rec, req)

	var body map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"] == nil {
		t.Error("expected error response")
	}
	errObj := body["error"].(map[string]interface{})
	if !strings.Contains(errObj["message"].(string), "SSN") {
		t.Errorf("expected SSN in error message, got %v", errObj["message"])
	}
}

func TestAnthropicHandlerSuccess(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Hello!", "STOP"))
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"Say hello"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeAnthropic(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var msg anthropic.Message
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil {
		t.Fatalf("Anthropic SDK parse error: %v\nBody: %s", err, rec.Body.String())
	}
	if msg.Type != "message" {
		t.Errorf("expected type=message, got %q", msg.Type)
	}
	if len(msg.Content) == 0 || msg.Content[0].Text != "Hello!" {
		t.Errorf("expected 'Hello!', got %v", msg.Content)
	}
}

func TestOpenAIHandlerSuccess(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Hi there!", "STOP"))
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"Say hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeOpenAI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var msg openai.ChatCompletion
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil {
		t.Fatalf("OpenAI SDK parse error: %v\nBody: %s", err, rec.Body.String())
	}
	if msg.Object != "chat.completion" {
		t.Errorf("expected object=chat.completion, got %q", msg.Object)
	}
	if len(msg.Choices) == 0 || msg.Choices[0].Message.Content != "Hi there!" {
		t.Errorf("expected 'Hi there!', got %v", msg.Choices)
	}
}

func TestEnvOr(t *testing.T) {
	os.Setenv("TEST_FOO", "bar")
	defer os.Unsetenv("TEST_FOO")

	if v := config.EnvOr("TEST_FOO", "default"); v != "bar" {
		t.Errorf("expected 'bar', got %q", v)
	}
	if v := config.EnvOr("TEST_MISSING", "default"); v != "default" {
		t.Errorf("expected 'default', got %q", v)
	}
}

func TestSplitEnv(t *testing.T) {
	os.Setenv("TEST_LIST", "a,b,c")
	defer os.Unsetenv("TEST_LIST")

	result := config.SplitEnv("TEST_LIST", "x")
	if len(result) != 3 || result[0] != "a" || result[2] != "c" {
		t.Errorf("expected [a b c], got %v", result)
	}

	result = config.SplitEnv("TEST_MISSING", "x")
	if len(result) != 1 || result[0] != "x" {
		t.Errorf("expected [x], got %v", result)
	}
}

func TestContains(t *testing.T) {
	s := []string{"a", "b", "c"}
	if !config.Contains(s, "a") || !config.Contains(s, "c") {
		t.Error("expected true")
	}
	if config.Contains(s, "d") {
		t.Error("expected false")
	}
}

func TestStrVal(t *testing.T) {
	if v := translate.StrVal("hello", "def"); v != "hello" {
		t.Errorf("expected 'hello', got %q", v)
	}
	if v := translate.StrVal(42, "def"); v != "def" {
		t.Errorf("expected 'def', got %q", v)
	}
	if v := translate.StrVal(nil, "def"); v != "def" {
		t.Errorf("expected 'def', got %q", v)
	}
}

func TestIntVal(t *testing.T) {
	if v := translate.IntVal(float64(42)); v != 42 {
		t.Errorf("expected 42, got %d", v)
	}
	if v := translate.IntVal("not a number"); v != 0 {
		t.Errorf("expected 0, got %d", v)
	}
	if v := translate.IntVal(nil); v != 0 {
		t.Errorf("expected 0, got %d", v)
	}
}

func TestExtractGeminiText(t *testing.T) {
	tests := []struct {
		name     string
		resp     map[string]interface{}
		expected string
	}{
		{
			"normal",
			map[string]interface{}{
				"candidates": []interface{}{
					map[string]interface{}{
						"content": map[string]interface{}{
							"parts": []interface{}{map[string]interface{}{"text": "hello"}},
						},
					},
				},
			},
			"hello",
		},
		{
			"no candidates",
			map[string]interface{}{},
			"",
		},
		{
			"empty candidates",
			map[string]interface{}{"candidates": []interface{}{}},
			"",
		},
		{
			"no text in parts",
			map[string]interface{}{
				"candidates": []interface{}{
					map[string]interface{}{
						"content": map[string]interface{}{
							"parts": []interface{}{map[string]interface{}{}},
						},
					},
				},
			},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := translate.ExtractGeminiText(tt.resp)
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestAnthropicHandlerMethodNotAllowed(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("GET", "/v1/messages", nil)
	rec := httptest.NewRecorder()
	srv.ServeAnthropic(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestOpenAIHandlerMethodNotAllowed(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	srv.ServeOpenAI(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestUserAgentEnforcement(t *testing.T) {
	testCfg := &config.Config{
		AllowedDomains: []string{"example.com"},
		UserAgentRegex: regexp.MustCompile(`^opencode/`),
		APIKeys:        map[string]bool{},
	}

	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("User-Agent", "curl/7.88")
	rec := httptest.NewRecorder()

	if auth.CheckUserAgent(testCfg, rec, req) {
		t.Error("expected curl UA to be rejected")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	req2 := httptest.NewRequest("POST", "/v1/messages", nil)
	req2.Header.Set("User-Agent", "opencode/1.0.0")
	rec2 := httptest.NewRecorder()
	if !auth.CheckUserAgent(testCfg, rec2, req2) {
		t.Error("expected opencode UA to be allowed")
	}
}

func TestUserAgentNoRegex(t *testing.T) {
	testCfg := &config.Config{UserAgentRegex: nil}
	req := httptest.NewRequest("POST", "/v1/messages", nil)
	rec := httptest.NewRecorder()
	if !auth.CheckUserAgent(testCfg, rec, req) {
		t.Error("expected no regex to allow all UAs")
	}
}

func TestRequestMiddleware(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("X-Request-ID", "test-req-123")
	req.Header.Set("X-Cloud-Trace-Context", "abc123def456/123;o=1")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRequestMiddlewareGeneratesID(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestMapKeys(t *testing.T) {
	os.Setenv("TEST_MAPKEYS", "key1,key2,key3")
	defer os.Unsetenv("TEST_MAPKEYS")
	m := config.MapKeys("TEST_MAPKEYS")
	if len(m) != 3 || !m["key1"] || !m["key3"] {
		t.Errorf("expected 3 keys, got %v", m)
	}
}

func TestMapKeysEmpty(t *testing.T) {
	m := config.MapKeys("TEST_NONEXISTENT_MAPKEYS")
	if len(m) != 0 {
		t.Errorf("expected empty map, got %v", m)
	}
}

func TestMin(t *testing.T) {
	if min(1, 2) != 1 {
		t.Error("expected min(1,2)=1")
	}
	if min(5, 3) != 3 {
		t.Error("expected min(5,3)=3")
	}
}

func TestWriteAnthropicSSE(t *testing.T) {
	msg := map[string]interface{}{
		"id":          "msg_test123",
		"type":        "message",
		"role":        "assistant",
		"model":       "gemini-2.5-flash",
		"stop_reason": "end_turn",
		"content":     []interface{}{map[string]interface{}{"type": "text", "text": "Hello SSE"}},
		"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
	}

	var buf bytes.Buffer
	streaming.WriteAnthropicSSE(&buf, msg)
	output := buf.String()

	if !strings.Contains(output, "event: message_start") {
		t.Error("expected message_start")
	}
	if !strings.Contains(output, "event: content_block_start") {
		t.Error("expected content_block_start")
	}
	if !strings.Contains(output, "event: content_block_delta") {
		t.Error("expected content_block_delta")
	}
	if !strings.Contains(output, "Hello SSE") {
		t.Error("expected text in delta")
	}
	if !strings.Contains(output, "event: content_block_stop") {
		t.Error("expected content_block_stop")
	}
	if !strings.Contains(output, "event: message_delta") {
		t.Error("expected message_delta")
	}
	if !strings.Contains(output, "event: message_stop") {
		t.Error("expected message_stop")
	}
}

func TestWriteOpenAISSE(t *testing.T) {
	msg := map[string]interface{}{
		"id":      "chatcmpl-test123",
		"object":  "chat.completion",
		"created": int64(1234567890),
		"model":   "gemini-2.5-flash",
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"message":       map[string]interface{}{"role": "assistant", "content": "Hi SSE"},
				"finish_reason": "stop",
			},
		},
	}

	var buf bytes.Buffer
	streaming.WriteOpenAISSE(&buf, msg)
	output := buf.String()

	if !strings.Contains(output, "Hi SSE") {
		t.Error("expected content in chunk")
	}
	if !strings.Contains(output, "[DONE]") {
		t.Error("expected [DONE] sentinel")
	}
	if !strings.Contains(output, "chat.completion.chunk") {
		t.Error("expected chunk object type")
	}
	if !strings.Contains(output, `"stop"`) {
		t.Error("expected finish_reason stop")
	}
}

func TestAnthropicStrictStreaming(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Hello", "STOP"))
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeAnthropic(rec, req)

	output := rec.Body.String()
	if !strings.Contains(output, "event: message_start") {
		t.Error("expected SSE events")
	}
}

func TestOpenAIStrictStreaming(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Hello", "STOP"))
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeOpenAI(rec, req)

	output := rec.Body.String()
	if !strings.Contains(output, "chat.completion.chunk") {
		t.Error("expected SSE chunks")
	}
}

func TestAnthropicFastStreaming(t *testing.T) {
	vertexTS := stubVertexStream(testGeminiStreamChunks())
	defer vertexTS.Close()
	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash", ResponseMode: "fast",
		VertexBase: vertexTS.URL, ModelArmorEndpoint: "https://unused",
		APIKeys:      map[string]bool{},
		TokenInfoURL: "https://oauth2.googleapis.com",
	}
	srv := newCustomServer(testCfg, vertexTS.Client())

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeAnthropic(rec, req)

	output := rec.Body.String()
	if !strings.Contains(output, "content_block_delta") {
		t.Errorf("expected streaming SSE events, got: %s", output[:min(len(output), 200)])
	}
}

func TestOpenAIFastStreaming(t *testing.T) {
	vertexTS := stubVertexStream(testGeminiStreamChunks())
	defer vertexTS.Close()
	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash", ResponseMode: "fast",
		VertexBase: vertexTS.URL, ModelArmorEndpoint: "https://unused",
		APIKeys:      map[string]bool{},
		TokenInfoURL: "https://oauth2.googleapis.com",
	}
	srv := newCustomServer(testCfg, vertexTS.Client())

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeOpenAI(rec, req)

	output := rec.Body.String()
	if !strings.Contains(output, "chat.completion.chunk") {
		t.Errorf("expected streaming SSE chunks, got: %s", output[:min(len(output), 200)])
	}
}

func TestAnthropicAuditStreaming(t *testing.T) {
	vertexTS := stubVertexStream(testGeminiStreamChunks())
	defer vertexTS.Close()
	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash", ResponseMode: "audit",
		VertexBase: vertexTS.URL, ModelArmorEndpoint: "https://unused",
		APIKeys:      map[string]bool{},
		TokenInfoURL: "https://oauth2.googleapis.com",
	}
	srv := newCustomServer(testCfg, vertexTS.Client())

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeAnthropic(rec, req)

	output := rec.Body.String()
	if !strings.Contains(output, "content_block_delta") {
		t.Errorf("expected streaming SSE events, got: %s", output[:min(len(output), 200)])
	}
}

func TestOpenAIAuditStreaming(t *testing.T) {
	vertexTS := stubVertexStream(testGeminiStreamChunks())
	defer vertexTS.Close()
	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash", ResponseMode: "audit",
		VertexBase: vertexTS.URL, ModelArmorEndpoint: "https://unused",
		APIKeys:      map[string]bool{},
		TokenInfoURL: "https://oauth2.googleapis.com",
	}
	srv := newCustomServer(testCfg, vertexTS.Client())

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeOpenAI(rec, req)

	output := rec.Body.String()
	if !strings.Contains(output, "chat.completion.chunk") {
		t.Errorf("expected streaming SSE chunks, got: %s", output[:min(len(output), 200)])
	}
}

func TestAnthropicFastNonStreaming(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Fast!", "STOP"))
	defer vertexTS.Close()
	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash", ResponseMode: "fast",
		VertexBase: vertexTS.URL, ModelArmorEndpoint: "https://unused",
		APIKeys:      map[string]bool{},
		TokenInfoURL: "https://oauth2.googleapis.com",
	}
	srv := newCustomServer(testCfg, vertexTS.Client())

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeAnthropic(rec, req)

	var msg anthropic.Message
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil {
		t.Fatalf("SDK parse error: %v", err)
	}
}

func TestOpenAIFastNonStreaming(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Fast!", "STOP"))
	defer vertexTS.Close()
	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash", ResponseMode: "fast",
		VertexBase: vertexTS.URL, ModelArmorEndpoint: "https://unused",
		APIKeys:      map[string]bool{},
		TokenInfoURL: "https://oauth2.googleapis.com",
	}
	srv := newCustomServer(testCfg, vertexTS.Client())

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeOpenAI(rec, req)

	var msg openai.ChatCompletion
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil {
		t.Fatalf("SDK parse error: %v", err)
	}
}

func TestAnthropicAuditNonStreaming(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Audit!", "STOP"))
	defer vertexTS.Close()
	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash", ResponseMode: "audit",
		VertexBase: vertexTS.URL, ModelArmorEndpoint: "https://unused",
		APIKeys:      map[string]bool{},
		TokenInfoURL: "https://oauth2.googleapis.com",
	}
	srv := newCustomServer(testCfg, vertexTS.Client())

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeAnthropic(rec, req)

	var msg anthropic.Message
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil {
		t.Fatalf("SDK parse error: %v", err)
	}
}

func TestOpenAIAuditNonStreaming(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Audit!", "STOP"))
	defer vertexTS.Close()
	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash", ResponseMode: "audit",
		VertexBase: vertexTS.URL, ModelArmorEndpoint: "https://unused",
		APIKeys:      map[string]bool{},
		TokenInfoURL: "https://oauth2.googleapis.com",
	}
	srv := newCustomServer(testCfg, vertexTS.Client())

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeOpenAI(rec, req)

	var msg openai.ChatCompletion
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil {
		t.Fatalf("SDK parse error: %v", err)
	}
}

func TestAnthropicStrictVertexError(t *testing.T) {
	vertexTS := stubVertexServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("upstream error"))
	})
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeAnthropic(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (error wrapped in Anthropic format), got %d", rec.Code)
	}
	var body map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["type"] != "error" {
		t.Error("expected Anthropic error format")
	}
}

func TestOpenAIStrictVertexError(t *testing.T) {
	vertexTS := stubVertexServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("upstream error"))
	})
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeOpenAI(rec, req)

	var body map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"] == nil {
		t.Error("expected OpenAI error format")
	}
}

func TestVertexCompatNonStreaming(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Vertex!", "STOP"))
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/models/gemini-2.5-flash:generateContent", strings.NewReader(
		`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeVertexCompat(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Vertex!") {
		t.Error("expected Vertex! in response")
	}
}

func TestVertexCompatStreaming(t *testing.T) {
	vertexTS := stubVertexStream(testGeminiStreamChunks())
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/models/gemini-2.5-flash:streamGenerateContent", strings.NewReader(
		`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeVertexCompat(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	output := rec.Body.String()
	if !strings.Contains(output, "Hi") {
		t.Errorf("expected streaming content, got: %s", output[:min(len(output), 200)])
	}
}

func TestVertexProjectHandler(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Proj!", "STOP"))
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/v1/projects/test/locations/europe-west2/publishers/google/models/gemini-2.5-flash:generateContent", strings.NewReader(
		`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeVertexProject(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestVertexProjectHandlerNoModels(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("POST", "/v1/projects/test/locations/europe-west2", nil)
	rec := httptest.NewRecorder()
	srv.ServeVertexProject(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestVertexCompatStreamingError(t *testing.T) {
	vertexTS := stubVertexServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	})
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/models/gemini-2.5-flash:streamGenerateContent", strings.NewReader(
		`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeVertexCompat(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}
}

func TestVertexCompatNonStreamingError(t *testing.T) {
	vertexTS := stubVertexServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	})
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/models/gemini-2.5-flash:generateContent", strings.NewReader(
		`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeVertexCompat(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}
}

func TestVertexCompatPromptFeedbackBlock(t *testing.T) {
	resp := `{"promptFeedback":{"blockReason":"SAFETY","blockReasonMessage":"Blocked by safety"},"candidates":[]}`
	vertexTS := stubVertexOK(resp)
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/models/gemini-2.5-flash:generateContent", strings.NewReader(
		`{"contents":[{"role":"user","parts":[{"text":"bad stuff"}]}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeVertexCompat(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (block passed through), got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "blockReason") {
		t.Error("expected blockReason in response")
	}
}

func TestVertexCompatSSNBlock(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("ok", "STOP"))
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/models/gemini-2.5-flash:generateContent", strings.NewReader(
		`{"contents":[{"role":"user","parts":[{"text":"My SSN is 123-45-6789"}]}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeVertexCompat(rec, req)
	var body map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["type"] != "error" {
		t.Error("expected SSN to be blocked")
	}
}

func TestModelArmorInspectorName(t *testing.T) {
	testCfg := &config.Config{
		Project:            "test-project",
		Location:           "europe-west2",
		ModelArmorLocation: "europe-west2",
		ModelArmorTemplate: "test-template",
		ModelArmorEndpoint: "https://unused",
		ResponseMode:       "fast",
	}
	ma := inspector.NewModelArmorInspector(testCfg, http.DefaultClient)
	if ma.Name() != "model_armor" {
		t.Errorf("expected name 'model_armor', got %q", ma.Name())
	}
}

func TestModelArmorInspectorPromptBlocked(t *testing.T) {
	maServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "sanitizeUserPrompt") {
			w.Write([]byte(`{"sanitizationResult":{"invocationResult":"SUCCESS","filterMatchState":"MATCH_FOUND","filterResults":{"rai_filter":{"raiFilterResult":{"matchState":"MATCH_FOUND"}}}}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer maServer.Close()

	testCfg := &config.Config{
		Project:            "test-project",
		Location:           "europe-west2",
		ModelArmorLocation: "europe-west2",
		ModelArmorTemplate: "test-template",
		ModelArmorEndpoint: maServer.URL,
		ResponseMode:       "fast",
	}
	ma := inspector.NewModelArmorInspector(testCfg, maServer.Client())

	br := ma.InspectPrompt(context.Background(), "bad prompt", "fake-token")
	if br == nil {
		t.Fatal("expected prompt to be blocked by Model Armor")
	}
	if !strings.Contains(br.Reason, "Model Armor") {
		t.Errorf("expected Model Armor reason, got %q", br.Reason)
	}
}

func TestModelArmorInspectorResponseBlocked(t *testing.T) {
	maServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "sanitizeModelResponse") {
			w.Write([]byte(`{"sanitizationResult":{"invocationResult":"SUCCESS","filterMatchState":"MATCH_FOUND","filterResults":{"csam":{"csamFilterFilterResult":{"matchState":"MATCH_FOUND"}}}}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer maServer.Close()

	testCfg := &config.Config{
		Project:            "test-project",
		Location:           "europe-west2",
		ModelArmorLocation: "europe-west2",
		ModelArmorTemplate: "test-template",
		ModelArmorEndpoint: maServer.URL,
		ResponseMode:       "fast",
	}
	ma := inspector.NewModelArmorInspector(testCfg, maServer.Client())

	br := ma.InspectResponse(context.Background(), "bad response", "fake-token")
	if br == nil {
		t.Fatal("expected response to be blocked by Model Armor")
	}
}

func TestModelArmorInspectorCleanPasses(t *testing.T) {
	maServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"sanitizationResult":{"invocationResult":"SUCCESS","filterMatchState":"NO_MATCH_FOUND"}}`))
	}))
	defer maServer.Close()

	testCfg := &config.Config{
		Project:            "test-project",
		Location:           "europe-west2",
		ModelArmorLocation: "europe-west2",
		ModelArmorTemplate: "test-template",
		ModelArmorEndpoint: maServer.URL,
		ResponseMode:       "fast",
	}
	ma := inspector.NewModelArmorInspector(testCfg, maServer.Client())

	br := ma.InspectPrompt(context.Background(), "clean prompt", "fake-token")
	if br != nil {
		t.Errorf("expected clean prompt to pass, got blocked: %s", br.Reason)
	}
}

func TestModelArmorInspectorHTTPError(t *testing.T) {
	maServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer maServer.Close()

	testCfg := &config.Config{
		Project:            "test-project",
		Location:           "europe-west2",
		ModelArmorLocation: "europe-west2",
		ModelArmorTemplate: "test-template",
		ModelArmorEndpoint: maServer.URL,
		ResponseMode:       "fast",
	}
	ma := inspector.NewModelArmorInspector(testCfg, maServer.Client())

	br := ma.InspectPrompt(context.Background(), "test", "fake-token")
	if br != nil && br.Blocked {
		t.Error("expected HTTP error to be treated as pass (fail open)")
	}
}

func TestModelArmorInspectorConnectionError(t *testing.T) {
	testCfg := &config.Config{
		Project:            "test",
		Location:           "europe-west2",
		ModelArmorLocation: "europe-west2",
		ModelArmorTemplate: "test",
		ModelArmorEndpoint: "http://127.0.0.1:1",
		ResponseMode:       "fast",
	}
	ma := inspector.NewModelArmorInspector(testCfg, http.DefaultClient)
	br := ma.InspectPrompt(context.Background(), "test", "fake-token")
	if br != nil && br.Blocked {
		t.Error("expected connection error to fail open")
	}
}

func TestDLPInspectorName(t *testing.T) {
	testCfg := &config.Config{Project: "test", Location: "europe-west2"}
	dlp := inspector.NewDLPInspector(testCfg, http.DefaultClient)
	if dlp.Name() != "dlp" {
		t.Errorf("expected name 'dlp', got %q", dlp.Name())
	}
}

func TestDLPInspectorBlocked(t *testing.T) {
	dlpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":{"findings":[{"infoType":{"name":"US_SOCIAL_SECURITY_NUMBER"},"likelihood":"LIKELY"},{"infoType":{"name":"EMAIL_ADDRESS"},"likelihood":"POSSIBLE"}]}}`))
	}))
	defer dlpServer.Close()

	body, _ := json.Marshal(map[string]interface{}{
		"item": map[string]interface{}{"value": "SSN: 123-45-6789"},
		"inspectConfig": map[string]interface{}{
			"infoTypes":     []interface{}{map[string]interface{}{"name": "US_SOCIAL_SECURITY_NUMBER"}},
			"minLikelihood": "LIKELY",
		},
	})
	resp, err := http.Post(dlpServer.URL+"/v2/projects/test/locations/europe-west2/content:inspect", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("DLP mock error: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	var result map[string]interface{}
	json.Unmarshal(data, &result)
	findings := result["result"].(map[string]interface{})["findings"].([]interface{})
	if len(findings) == 0 {
		t.Error("expected findings")
	}
}

func TestDLPInspectorCleanPasses(t *testing.T) {
	dlpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":{"findings":[]}}`))
	}))
	defer dlpServer.Close()

	os.Setenv("DLP_INFO_TYPES", "US_SOCIAL_SECURITY_NUMBER")
	os.Setenv("DLP_ENDPOINT", dlpServer.URL)
	defer os.Unsetenv("DLP_INFO_TYPES")
	defer os.Unsetenv("DLP_ENDPOINT")

	testCfg := &config.Config{Project: "test", Location: "europe-west2"}
	dlp := inspector.NewDLPInspector(testCfg, dlpServer.Client())
	br := dlp.InspectPrompt(context.Background(), "clean text", "fake-token")
	if br != nil {
		t.Errorf("expected clean text to pass, got: %v", br)
	}
}

func TestDLPInspectorHTTPErr(t *testing.T) {
	dlpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"denied"}`))
	}))
	defer dlpServer.Close()

	os.Setenv("DLP_INFO_TYPES", "US_SOCIAL_SECURITY_NUMBER")
	os.Setenv("DLP_ENDPOINT", dlpServer.URL)
	defer os.Unsetenv("DLP_INFO_TYPES")
	defer os.Unsetenv("DLP_ENDPOINT")

	testCfg := &config.Config{Project: "test", Location: "europe-west2"}
	dlp := inspector.NewDLPInspector(testCfg, dlpServer.Client())
	br := dlp.InspectPrompt(context.Background(), "test", "fake-token")
	if br != nil && br.Blocked {
		t.Error("expected fail open on non-ok response")
	}
}

func TestCallVertexStreamRawSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("stream data"))
	}))
	defer server.Close()

	testCfg := &config.Config{VertexBase: server.URL, Project: "test-project"}
	vc := vertex.NewClient(testCfg, server.Client())
	rc, err := vc.CallStreamRaw(context.Background(), map[string]interface{}{}, "token", "gemini-2.5-flash", "streamGenerateContent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "stream data" {
		t.Errorf("expected 'stream data', got %q", string(data))
	}
}

func TestCallVertexStreamRawError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("unavailable"))
	}))
	defer server.Close()

	testCfg := &config.Config{VertexBase: server.URL, Project: "test-project"}
	vc := vertex.NewClient(testCfg, server.Client())
	_, err := vc.CallStreamRaw(context.Background(), map[string]interface{}{}, "token", "gemini-2.5-flash", "streamGenerateContent")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected 503 in error, got %v", err)
	}
}

func TestCallVertexJSONForModelSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":"ok"}`))
	}))
	defer server.Close()

	testCfg := &config.Config{VertexBase: server.URL, Project: "test-project"}
	vc := vertex.NewClient(testCfg, server.Client())
	data, err := vc.CallJSONForModel(context.Background(), map[string]interface{}{}, "token", "gemini-2.5-flash", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != `{"result":"ok"}` {
		t.Errorf("expected ok response, got %q", string(data))
	}
}

func TestCallVertexJSONForModelError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer server.Close()

	testCfg := &config.Config{VertexBase: server.URL, Project: "test-project"}
	vc := vertex.NewClient(testCfg, server.Client())
	_, err := vc.CallJSONForModel(context.Background(), map[string]interface{}{}, "token", "gemini-2.5-flash", false)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCallVertexStreamSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("stream"))
	}))
	defer server.Close()

	testCfg := &config.Config{
		VertexBase: server.URL, Project: "test-project",
		FallbackGeminiModel: "gemini-2.5-flash",
	}
	vc := vertex.NewClient(testCfg, server.Client())
	rc, err := vc.CallStream(context.Background(), map[string]interface{}{}, "token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer rc.Close()
}

func TestCallVertexStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("err"))
	}))
	defer server.Close()

	testCfg := &config.Config{
		VertexBase: server.URL, Project: "test-project",
		FallbackGeminiModel: "gemini-2.5-flash",
	}
	vc := vertex.NewClient(testCfg, server.Client())
	_, err := vc.CallStream(context.Background(), map[string]interface{}{}, "token")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenAIHandlerSystemMessage(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("System handled!", "STOP"))
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"system","content":"You are helpful"},{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeOpenAI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestInitInspectorsStrict(t *testing.T) {
	testCfg := &config.Config{
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash",
		ResponseMode:        "strict",
		ModelArmorTemplate:  "test-template",
		ModelArmorLocation:  "europe-west2",
	}
	os.Unsetenv("DLP_API")
	var insps []inspector.Inspector
	insps = append(insps, inspector.NewRegexInspector())
	if testCfg.ModelArmorTemplate != "" && testCfg.ResponseMode != "strict" {
		insps = append(insps, inspector.NewModelArmorInspector(testCfg, http.DefaultClient))
	}
	if os.Getenv("DLP_API") == "true" {
		insps = append(insps, inspector.NewDLPInspector(testCfg, http.DefaultClient))
	}

	if len(insps) != 1 || insps[0].Name() != "regex" {
		t.Errorf("expected only regex in strict mode, got %v", inspectorNames(insps))
	}
}

func TestInitInspectorsNonStrict(t *testing.T) {
	testCfg := &config.Config{
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash",
		ResponseMode:        "fast",
		ModelArmorTemplate:  "test-template",
		ModelArmorLocation:  "europe-west2",
		ModelArmorEndpoint:  "https://unused",
	}
	os.Unsetenv("DLP_API")
	var insps []inspector.Inspector
	insps = append(insps, inspector.NewRegexInspector())
	if testCfg.ModelArmorTemplate != "" && testCfg.ResponseMode != "strict" {
		insps = append(insps, inspector.NewModelArmorInspector(testCfg, http.DefaultClient))
	}
	if os.Getenv("DLP_API") == "true" {
		insps = append(insps, inspector.NewDLPInspector(testCfg, http.DefaultClient))
	}

	if len(insps) != 2 {
		t.Errorf("expected regex + model_armor in non-strict mode, got %v", inspectorNames(insps))
	}
}

func TestInitInspectorsWithDLP(t *testing.T) {
	testCfg := &config.Config{
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash",
		ResponseMode:        "strict",
		ModelArmorTemplate:  "test-template",
		ModelArmorLocation:  "europe-west2",
	}
	os.Setenv("DLP_API", "true")
	defer os.Unsetenv("DLP_API")
	var insps []inspector.Inspector
	insps = append(insps, inspector.NewRegexInspector())
	if testCfg.ModelArmorTemplate != "" && testCfg.ResponseMode != "strict" {
		insps = append(insps, inspector.NewModelArmorInspector(testCfg, http.DefaultClient))
	}
	if os.Getenv("DLP_API") == "true" {
		insps = append(insps, inspector.NewDLPInspector(testCfg, http.DefaultClient))
	}

	names := inspectorNames(insps)
	if len(insps) != 2 {
		t.Errorf("expected regex + dlp, got %v", names)
	}
	hasDLP := false
	for _, n := range names {
		if n == "dlp" {
			hasDLP = true
		}
	}
	if !hasDLP {
		t.Error("expected DLP inspector")
	}
}

func TestDomainDenyInAuth(t *testing.T) {
	testCfg := &config.Config{
		AllowedDomains: []string{"example.com"},
		APIKeys:        map[string]bool{},
	}

	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@evil.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	_, ok := auth.Authenticate(testCfg, http.DefaultClient, rec, req)
	if ok {
		t.Error("expected evil.com domain to be rejected")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestDLPInspectResponseDelegate(t *testing.T) {
	os.Setenv("DLP_ENDPOINT", "http://127.0.0.1:1")
	defer os.Unsetenv("DLP_ENDPOINT")
	testCfg := &config.Config{Project: "test", Location: "europe-west2"}
	dlp := inspector.NewDLPInspector(testCfg, http.DefaultClient)
	br := dlp.InspectResponse(context.Background(), "test", "fake-token")
	if br != nil && br.Blocked {
		t.Error("expected fail open on connection error")
	}
}

func TestVertexCompatAnthropicFormat(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Anthropic compat!", "STOP"))
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/models/gemini-2.5-flash:generateContent", strings.NewReader(
		`{"anthropic_version":"2023-06-01","messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeVertexCompat(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAnthropicStrictInvalidVertexJSON(t *testing.T) {
	vertexTS := stubVertexOK("not valid json")
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeAnthropic(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with error wrapper, got %d", rec.Code)
	}
}

func TestOpenAIStrictInvalidVertexJSON(t *testing.T) {
	vertexTS := stubVertexOK("not valid json")
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeOpenAI(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with error wrapper, got %d", rec.Code)
	}
}

func TestVertexCompatInvalidVertexJSON(t *testing.T) {
	vertexTS := stubVertexOK("not valid json")
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/models/gemini-2.5-flash:generateContent", strings.NewReader(
		`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeVertexCompat(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}
}

func TestVertexCompatResponseSSNBlock(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("SSN: 123-45-6789", "STOP"))
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/models/gemini-2.5-flash:generateContent", strings.NewReader(
		`{"contents":[{"role":"user","parts":[{"text":"tell me a secret"}]}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeVertexCompat(rec, req)
	var body map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["type"] != "error" {
		t.Error("expected response SSN to be blocked")
	}
}

func TestAnthropicHandlerEmptyModel(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Fallback!", "STOP"))
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeAnthropic(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var msg anthropic.Message
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil {
		t.Fatalf("SDK parse error: %v", err)
	}
	if msg.Model != "gemini-2.5-flash" {
		t.Errorf("expected fallback model, got %q", msg.Model)
	}
}

func TestOpenAIHandlerEmptyModel(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Fallback!", "STOP"))
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeOpenAI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStreamGeminiAsAnthropicNoFinish(t *testing.T) {
	rc := io.NopCloser(strings.NewReader(testGeminiStreamChunksNoFinish()))
	var buf bytes.Buffer
	accumulated := streaming.StreamGeminiAsAnthropic(&buf, rc, "model", nil)
	if accumulated != "Hi there" {
		t.Errorf("expected 'Hi there', got %q", accumulated)
	}
}

func TestStreamGeminiAsOpenAINoFinish(t *testing.T) {
	rc := io.NopCloser(strings.NewReader(testGeminiStreamChunksNoFinish()))
	var buf bytes.Buffer
	accumulated := streaming.StreamGeminiAsOpenAI(&buf, rc, "model", nil)
	if accumulated != "Hi there" {
		t.Errorf("expected 'Hi there', got %q", accumulated)
	}
}

func TestExtractGenerationConfigStopSlice(t *testing.T) {
	body := map[string]interface{}{
		"stop": []interface{}{"END", "STOP"},
	}
	gc := translate.ExtractGenerationConfig(body)
	stopSeqs, ok := gc["stopSequences"].([]interface{})
	if !ok || len(stopSeqs) != 2 {
		t.Errorf("expected 2 stop sequences, got %v", gc["stopSequences"])
	}
}

func TestVertexCompatEmptyModel(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Fallback!", "STOP"))
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/models/:generateContent", strings.NewReader(
		`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeVertexCompat(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestVertexCompatNoColon(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("NoColon!", "STOP"))
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/models/gemini-2.5-flash", strings.NewReader(
		`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeVertexCompat(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthNoForwardedToken(t *testing.T) {
	testCfg := &config.Config{
		AllowedDomains: []string{"example.com"},
		APIKeys:        map[string]bool{},
	}

	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	rec := httptest.NewRecorder()

	identity, ok := auth.Authenticate(testCfg, http.DefaultClient, rec, req)
	if !ok {
		t.Fatal("expected auth to succeed")
	}
	if identity.AccessToken != "" {
		t.Errorf("expected empty AccessToken when no forwarded token, got %q", identity.AccessToken)
	}
}

func TestCallVertexJSONError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("model not found"))
	}))
	defer server.Close()

	testCfg := &config.Config{
		VertexBase: server.URL, Project: "test-project",
		FallbackGeminiModel: "gemini-2.5-flash",
	}
	vc := vertex.NewClient(testCfg, server.Client())
	_, err := vc.CallJSON(context.Background(), map[string]interface{}{}, "token", false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error, got %v", err)
	}
}

func TestExtractEmailFromJWTInvalidBase64(t *testing.T) {
	result := auth.ExtractEmailFromJWT("a.!!!invalid-base64!!!.c")
	if result != "" {
		t.Errorf("expected empty for invalid base64, got %q", result)
	}
}

func TestExtractEmailFromJWTNoEmail(t *testing.T) {
	header := base64Encode(`{"alg":"RS256","typ":"JWT"}`)
	payload := base64Encode(`{"sub":"12345"}`)
	token := header + "." + payload + ".sig"
	result := auth.ExtractEmailFromJWT(token)
	if result != "" {
		t.Errorf("expected empty for no email, got %q", result)
	}
}

func TestOpenAIHandlerInvalidBody(t *testing.T) {
	srv := newTestServer(nil)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("bad json"))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeOpenAI(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestAnthropicHandlerInvalidBody(t *testing.T) {
	srv := newTestServer(nil)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader("bad json"))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeAnthropic(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestDLPInspectBlocked(t *testing.T) {
	dlpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":{"findings":[{"infoType":{"name":"US_SOCIAL_SECURITY_NUMBER"},"likelihood":"LIKELY"},{"infoType":{"name":"EMAIL_ADDRESS"},"likelihood":"POSSIBLE"}]}}`))
	}))
	defer dlpServer.Close()

	os.Setenv("DLP_INFO_TYPES", "US_SOCIAL_SECURITY_NUMBER")
	os.Setenv("DLP_ENDPOINT", dlpServer.URL)
	defer os.Unsetenv("DLP_INFO_TYPES")
	defer os.Unsetenv("DLP_ENDPOINT")

	testCfg := &config.Config{Project: "test-project", Location: "europe-west2"}
	dlp := inspector.NewDLPInspector(testCfg, dlpServer.Client())

	br := dlp.InspectPrompt(context.Background(), "SSN: 123-45-6789", "fake-token")
	if br == nil {
		t.Fatal("expected DLP to block SSN")
	}
	if !strings.Contains(br.Reason, "DLP") {
		t.Errorf("expected DLP prefix, got %q", br.Reason)
	}
	if !strings.Contains(br.Reason, "US_SOCIAL_SECURITY_NUMBER") {
		t.Errorf("expected SSN info type, got %q", br.Reason)
	}
}

func TestDLPInspectResponseBlocked(t *testing.T) {
	dlpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":{"findings":[{"infoType":{"name":"CREDIT_CARD_NUMBER"},"likelihood":"VERY_LIKELY"}]}}`))
	}))
	defer dlpServer.Close()

	os.Setenv("DLP_INFO_TYPES", "CREDIT_CARD_NUMBER")
	os.Setenv("DLP_ENDPOINT", dlpServer.URL)
	defer os.Unsetenv("DLP_INFO_TYPES")
	defer os.Unsetenv("DLP_ENDPOINT")

	testCfg := &config.Config{Project: "test-project", Location: "europe-west2"}
	dlp := inspector.NewDLPInspector(testCfg, dlpServer.Client())

	br := dlp.InspectResponse(context.Background(), "card 1234567890123456", "fake-token")
	if br == nil {
		t.Fatal("expected DLP to block credit card")
	}
	if !strings.Contains(br.Reason, "CREDIT_CARD_NUMBER") {
		t.Errorf("expected CREDIT_CARD_NUMBER, got %q", br.Reason)
	}
}

func TestDLPInspectDedup(t *testing.T) {
	dlpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":{"findings":[{"infoType":{"name":"EMAIL_ADDRESS"},"likelihood":"LIKELY"},{"infoType":{"name":"EMAIL_ADDRESS"},"likelihood":"POSSIBLE"}]}}`))
	}))
	defer dlpServer.Close()

	os.Setenv("DLP_INFO_TYPES", "EMAIL_ADDRESS")
	os.Setenv("DLP_ENDPOINT", dlpServer.URL)
	defer os.Unsetenv("DLP_INFO_TYPES")
	defer os.Unsetenv("DLP_ENDPOINT")

	testCfg := &config.Config{Project: "test", Location: "europe-west2"}
	dlp := inspector.NewDLPInspector(testCfg, dlpServer.Client())

	br := dlp.InspectPrompt(context.Background(), "email@test.com", "fake-token")
	if br == nil {
		t.Fatal("expected block")
	}
	if strings.Count(br.Reason, "EMAIL_ADDRESS") != 1 {
		t.Errorf("expected dedup, got %q", br.Reason)
	}
}

func TestDLPInspectConnectionError(t *testing.T) {
	os.Setenv("DLP_ENDPOINT", "http://127.0.0.1:1")
	defer os.Unsetenv("DLP_ENDPOINT")
	testCfg := &config.Config{Project: "test", Location: "europe-west2"}
	dlp := inspector.NewDLPInspector(testCfg, http.DefaultClient)
	br := dlp.InspectPrompt(context.Background(), "test", "fake-token")
	if br != nil && br.Blocked {
		t.Error("expected connection error to fail open")
	}
}

func TestDLPInspectRequestError(t *testing.T) {
	os.Setenv("DLP_ENDPOINT", "http://127.0.0.1:1")
	defer os.Unsetenv("DLP_ENDPOINT")
	testCfg := &config.Config{Project: "test", Location: "europe-west2"}
	dlp := inspector.NewDLPInspector(testCfg, http.DefaultClient)
	br := dlp.InspectPrompt(context.Background(), "test", "fake-token")
	if br != nil && br.Blocked {
		t.Error("expected request error to fail open")
	}
}

func TestTokenInfoSuccess(t *testing.T) {
	tiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"email":"user@example.com","email_verified":true}`))
	}))
	defer tiServer.Close()

	testCfg := &config.Config{
		TokenInfoURL:   tiServer.URL,
		AllowedDomains: []string{"example.com"},
		APIKeys:        map[string]bool{},
	}

	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer real-access-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	identity, ok := auth.Authenticate(testCfg, tiServer.Client(), rec, req)
	if !ok {
		t.Fatalf("expected auth to succeed, got %d: %s", rec.Code, rec.Body.String())
	}
	if identity.Email != "user@example.com" {
		t.Errorf("expected email from tokeninfo, got %q", identity.Email)
	}
}

func TestTokenInfoFailure(t *testing.T) {
	tiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer tiServer.Close()

	testCfg := &config.Config{
		TokenInfoURL:   tiServer.URL,
		AllowedDomains: []string{"example.com"},
		APIKeys:        map[string]bool{},
	}

	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer fake-token-no-email")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	_, ok := auth.Authenticate(testCfg, tiServer.Client(), rec, req)
	if ok {
		t.Error("expected auth to fail when tokeninfo returns 401 and JWT has no email")
	}
}

func TestTokenInfoConnectionError(t *testing.T) {
	testCfg := &config.Config{
		TokenInfoURL:   "http://127.0.0.1:1",
		AllowedDomains: []string{"example.com"},
		APIKeys:        map[string]bool{},
	}

	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	_, ok := auth.Authenticate(testCfg, http.DefaultClient, rec, req)
	if ok {
		t.Error("expected auth to fail when tokeninfo is unreachable and no JWT email")
	}
}

func TestAnthropicStrictResponseSSNBlock(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Your SSN is 123-45-6789", "STOP"))
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"tell me a secret"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeAnthropic(rec, req)

	var body map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["type"] != "error" {
		t.Error("expected SSN in response to be blocked")
	}
}

func TestOpenAIStrictResponseSSNBlock(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Your SSN is 123-45-6789", "STOP"))
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"tell me a secret"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeOpenAI(rec, req)

	var body map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"] == nil {
		t.Error("expected SSN in response to be blocked")
	}
}

func TestAnthropicStrictModelArmorBlock(t *testing.T) {
	resp := `{"promptFeedback":{"blockReason":"SAFETY","blockReasonMessage":"Blocked by safety"},"candidates":[]}`
	vertexTS := stubVertexOK(resp)
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"bad stuff"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeAnthropic(rec, req)

	var body map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["type"] != "error" {
		t.Error("expected Model Armor block")
	}
}

func TestOpenAIStrictModelArmorBlock(t *testing.T) {
	resp := `{"promptFeedback":{"blockReason":"SAFETY","blockReasonMessage":"Blocked by safety"},"candidates":[]}`
	vertexTS := stubVertexOK(resp)
	defer vertexTS.Close()
	srv := newTestServer(vertexTS)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"bad stuff"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeOpenAI(rec, req)

	var body map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"] == nil {
		t.Error("expected Model Armor block")
	}
}

func TestCallVertexStreamRawRequestError(t *testing.T) {
	testCfg := &config.Config{VertexBase: "http://127.0.0.1:1", Project: "test-project"}
	vc := vertex.NewClient(testCfg, http.DefaultClient)
	_, err := vc.CallStreamRaw(context.Background(), map[string]interface{}{}, "token", "model", "action")
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestCallVertexJSONRequestError(t *testing.T) {
	testCfg := &config.Config{
		VertexBase: "http://127.0.0.1:1", Project: "test-project",
		FallbackGeminiModel: "gemini-2.5-flash",
	}
	vc := vertex.NewClient(testCfg, http.DefaultClient)
	_, err := vc.CallJSON(context.Background(), map[string]interface{}{}, "token", false)
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestCallVertexStreamRequestError(t *testing.T) {
	testCfg := &config.Config{
		VertexBase: "http://127.0.0.1:1", Project: "test-project",
		FallbackGeminiModel: "gemini-2.5-flash",
	}
	vc := vertex.NewClient(testCfg, http.DefaultClient)
	_, err := vc.CallStream(context.Background(), map[string]interface{}{}, "token")
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestCallVertexJSONForModelRequestError(t *testing.T) {
	testCfg := &config.Config{VertexBase: "http://127.0.0.1:1", Project: "test-project"}
	vc := vertex.NewClient(testCfg, http.DefaultClient)
	_, err := vc.CallJSONForModel(context.Background(), map[string]interface{}{}, "token", "model", false)
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestLocalModeAuthSkipped(t *testing.T) {
	testCfg := &config.Config{
		LocalMode:           true,
		AllowedDomains:      []string{"example.com"},
		APIKeys:             map[string]bool{},
		FallbackGeminiModel: "gemini-2.5-flash",
		VertexBase:          "https://unused.example.com",
		ModelArmorEndpoint:  "https://unused.example.com",
	}

	req := httptest.NewRequest("POST", "/v1/messages", nil)
	rec := httptest.NewRecorder()

	identity, ok := auth.Authenticate(testCfg, http.DefaultClient, rec, req)
	if !ok {
		t.Fatal("expected LOCAL_MODE auth to succeed without any headers")
	}
	if identity.Email != "local@localhost" {
		t.Errorf("expected local@localhost, got %q", identity.Email)
	}
	if identity.AccessToken != "" {
		t.Errorf("expected empty AccessToken in LOCAL_MODE, got %q", identity.AccessToken)
	}
}

func TestLocalModeIgnoresAuthHeaders(t *testing.T) {
	testCfg := &config.Config{
		LocalMode:           true,
		AllowedDomains:      []string{"example.com"},
		APIKeys:             map[string]bool{},
		FallbackGeminiModel: "gemini-2.5-flash",
		VertexBase:          "https://unused.example.com",
		ModelArmorEndpoint:  "https://unused.example.com",
	}

	req := httptest.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	req.Header.Set("X-Forwarded-Access-Token", "some-access-token")
	rec := httptest.NewRecorder()

	identity, ok := auth.Authenticate(testCfg, http.DefaultClient, rec, req)
	if !ok {
		t.Fatal("expected auth to succeed")
	}
	if identity.Email != "local@localhost" {
		t.Errorf("expected local@localhost regardless of headers, got %q", identity.Email)
	}
	if identity.AccessToken != "" {
		t.Error("expected empty AccessToken in LOCAL_MODE even when headers sent")
	}
}

func TestLocalModeAnthropicHandlerNoAuth(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Hello from local!", "STOP"))
	defer vertexTS.Close()

	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		LocalMode:           true,
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash",
		ResponseMode:        "strict",
		VertexBase:          vertexTS.URL,
		ModelArmorEndpoint:  "https://unused.example.com",
		APIKeys:             map[string]bool{},
		TokenInfoURL:        "https://oauth2.googleapis.com",
	}
	chain := inspector.NewChain(inspector.NewRegexInspector())
	vc := vertex.NewClient(testCfg, vertexTS.Client())
	vc.SetADCTokenFunc(func() string { return "adc-mock-token" })
	srv := handler.NewServer(testCfg, chain, vc, vertexTS.Client(), nil)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"hello"}]}`,
	))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeAnthropic(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var msg anthropic.Message
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil {
		t.Fatalf("SDK parse error: %v", err)
	}
	if len(msg.Content) == 0 || msg.Content[0].Text != "Hello from local!" {
		t.Errorf("expected 'Hello from local!', got %v", msg.Content)
	}
}

func TestLocalModeOpenAIHandlerNoAuth(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Local mode!", "STOP"))
	defer vertexTS.Close()

	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		LocalMode:           true,
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash",
		ResponseMode:        "strict",
		VertexBase:          vertexTS.URL,
		ModelArmorEndpoint:  "https://unused.example.com",
		APIKeys:             map[string]bool{},
		TokenInfoURL:        "https://oauth2.googleapis.com",
	}
	chain := inspector.NewChain(inspector.NewRegexInspector())
	vc := vertex.NewClient(testCfg, vertexTS.Client())
	vc.SetADCTokenFunc(func() string { return "adc-mock-token" })
	srv := handler.NewServer(testCfg, chain, vc, vertexTS.Client(), nil)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"hello"}]}`,
	))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeOpenAI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var msg openai.ChatCompletion
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil {
		t.Fatalf("SDK parse error: %v", err)
	}
	if len(msg.Choices) == 0 || msg.Choices[0].Message.Content != "Local mode!" {
		t.Errorf("expected 'Local mode!', got %v", msg.Choices)
	}
}

func TestLocalModeBlocksSSN(t *testing.T) {
	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		LocalMode:           true,
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash",
		ResponseMode:        "strict",
		VertexBase:          "https://unused.example.com",
		ModelArmorEndpoint:  "https://unused.example.com",
		APIKeys:             map[string]bool{},
		TokenInfoURL:        "https://oauth2.googleapis.com",
	}
	chain := inspector.NewChain(inspector.NewRegexInspector())
	vc := vertex.NewClient(testCfg, http.DefaultClient)
	srv := handler.NewServer(testCfg, chain, vc, http.DefaultClient, nil)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"My SSN is 123-45-6789"}]}`,
	))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeAnthropic(rec, req)

	var body map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["type"] != "error" {
		t.Error("expected SSN to be blocked even in LOCAL_MODE")
	}
}

func TestLocalModeUsesADCToken(t *testing.T) {
	var receivedToken string
	vertexTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(makeGeminiResponse("ok", "STOP")))
	}))
	defer vertexTS.Close()

	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		LocalMode:           true,
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash",
		ResponseMode:        "strict",
		VertexBase:          vertexTS.URL,
		ModelArmorEndpoint:  "https://unused.example.com",
		APIKeys:             map[string]bool{},
		TokenInfoURL:        "https://oauth2.googleapis.com",
	}
	chain := inspector.NewChain(inspector.NewRegexInspector())
	vc := vertex.NewClient(testCfg, vertexTS.Client())
	vc.SetADCTokenFunc(func() string { return "adc-token-12345" })
	srv := handler.NewServer(testCfg, chain, vc, vertexTS.Client(), nil)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"hello"}]}`,
	))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.ServeAnthropic(rec, req)

	if receivedToken != "adc-token-12345" {
		t.Errorf("expected Vertex AI to receive ADC token, got %q", receivedToken)
	}
}

func TestModeAliasesPassthrough(t *testing.T) {
	os.Setenv("RESPONSE_MODE", "strict")
	cfg := config.Load()
	if cfg.ResponseMode != "strict" {
		t.Errorf("expected strict to pass through, got %q", cfg.ResponseMode)
	}
	os.Unsetenv("RESPONSE_MODE")

	os.Setenv("RESPONSE_MODE", "audit")
	cfg = config.Load()
	if cfg.ResponseMode != "audit" {
		t.Errorf("expected audit to pass through, got %q", cfg.ResponseMode)
	}
	os.Unsetenv("RESPONSE_MODE")

	os.Setenv("RESPONSE_MODE", "fast")
	cfg = config.Load()
	if cfg.ResponseMode != "fast" {
		t.Errorf("expected fast to pass through, got %q", cfg.ResponseMode)
	}
	os.Unsetenv("RESPONSE_MODE")

	os.Setenv("RESPONSE_MODE", "unknown_mode")
	cfg = config.Load()
	if cfg.ResponseMode != "unknown_mode" {
		t.Errorf("expected unknown_mode to pass through, got %q", cfg.ResponseMode)
	}
	os.Unsetenv("RESPONSE_MODE")
}

func TestConfigLoadDefaults(t *testing.T) {
	os.Unsetenv("GOOGLE_CLOUD_PROJECT")
	os.Unsetenv("GOOGLE_CLOUD_LOCATION")
	os.Unsetenv("RESPONSE_MODE")
	os.Unsetenv("FALLBACK_GEMINI_MODEL")
	os.Unsetenv("MODEL_ARMOR_TEMPLATE")
	os.Unsetenv("LOCAL_MODE")
	os.Unsetenv("PORT")
	os.Unsetenv("LOG_LEVEL")
	os.Unsetenv("LOG_PROMPT_MODE")
	os.Unsetenv("API_KEYS")

	cfg := config.Load()
	if cfg.Location != "europe-west2" {
		t.Errorf("expected default location europe-west2, got %q", cfg.Location)
	}
	if cfg.ResponseMode != "strict" {
		t.Errorf("expected default mode strict, got %q", cfg.ResponseMode)
	}
	if cfg.FallbackGeminiModel != "gemini-2.5-flash" {
		t.Errorf("expected default model gemini-2.5-flash, got %q", cfg.FallbackGeminiModel)
	}
	if cfg.Port != "8080" {
		t.Errorf("expected default port 8080, got %q", cfg.Port)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected default log level info, got %q", cfg.LogLevel)
	}
	if cfg.LogPromptMode != "truncate" {
		t.Errorf("expected default log prompt mode truncate, got %q", cfg.LogPromptMode)
	}
	if cfg.LocalMode != false {
		t.Error("expected LOCAL_MODE to be false by default")
	}
}

func TestConfigLoadCustom(t *testing.T) {
	os.Setenv("GOOGLE_CLOUD_PROJECT", "my-project")
	os.Setenv("GOOGLE_CLOUD_LOCATION", "us-central1")
	os.Setenv("RESPONSE_MODE", "fast")
	os.Setenv("FALLBACK_GEMINI_MODEL", "gemini-2.5-pro")
	os.Setenv("LOCAL_MODE", "true")
	os.Setenv("PORT", "9090")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("LOG_PROMPT_MODE", "hash")
	os.Setenv("ALLOWED_DOMAINS", "foo.com, bar.com")
	os.Setenv("API_KEYS", "key1,key2")
	defer func() {
		os.Unsetenv("GOOGLE_CLOUD_PROJECT")
		os.Unsetenv("GOOGLE_CLOUD_LOCATION")
		os.Unsetenv("RESPONSE_MODE")
		os.Unsetenv("FALLBACK_GEMINI_MODEL")
		os.Unsetenv("LOCAL_MODE")
		os.Unsetenv("PORT")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("LOG_PROMPT_MODE")
		os.Unsetenv("ALLOWED_DOMAINS")
		os.Unsetenv("API_KEYS")
	}()

	cfg := config.Load()
	if cfg.Project != "my-project" {
		t.Errorf("expected project my-project, got %q", cfg.Project)
	}
	if cfg.Location != "us-central1" {
		t.Errorf("expected location us-central1, got %q", cfg.Location)
	}
	if cfg.ResponseMode != "fast" {
		t.Errorf("expected mode fast, got %q", cfg.ResponseMode)
	}
	if cfg.FallbackGeminiModel != "gemini-2.5-pro" {
		t.Errorf("expected model gemini-2.5-pro, got %q", cfg.FallbackGeminiModel)
	}
	if !cfg.LocalMode {
		t.Error("expected LOCAL_MODE true")
	}
	if cfg.Port != "9090" {
		t.Errorf("expected port 9090, got %q", cfg.Port)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected log level debug, got %q", cfg.LogLevel)
	}
	if cfg.LogPromptMode != "hash" {
		t.Errorf("expected log prompt mode hash, got %q", cfg.LogPromptMode)
	}
	if !config.Contains(cfg.AllowedDomains, "foo.com") || !config.Contains(cfg.AllowedDomains, "bar.com") {
		t.Errorf("expected allowed domains [foo.com bar.com], got %v", cfg.AllowedDomains)
	}
	if !cfg.APIKeys["key1"] || !cfg.APIKeys["key2"] {
		t.Errorf("expected API keys [key1 key2], got %v", cfg.APIKeys)
	}
	if !strings.Contains(cfg.VertexBase, "us-central1") {
		t.Errorf("expected VertexBase to contain us-central1, got %q", cfg.VertexBase)
	}
}

func TestRedactPromptModes(t *testing.T) {
	cfg := &config.Config{LogPromptMode: "truncate", LogPromptLength: 32}

	if cfg.RedactPrompt("") != "" {
		t.Error("expected empty string for empty prompt")
	}

	long := strings.Repeat("a", 100)
	truncated := cfg.RedactPrompt(long)
	if !strings.HasSuffix(truncated, "...") {
		t.Errorf("expected truncation with ..., got %q", truncated)
	}

	short := "hello world"
	if cfg.RedactPrompt(short) != short {
		t.Errorf("expected short prompt unchanged, got %q", cfg.RedactPrompt(short))
	}

	cfg.LogPromptMode = "none"
	if cfg.RedactPrompt("secret") != "" {
		t.Error("expected empty string for none mode")
	}

	cfg.LogPromptMode = "hash"
	hashed := cfg.RedactPrompt("secret")
	if len(hashed) != 16 {
		t.Errorf("expected 16-char SHA-256 prefix, got %d chars: %q", len(hashed), hashed)
	}
	different := cfg.RedactPrompt("different")
	if hashed == different {
		t.Error("expected different hashes for different inputs")
	}

	cfg.LogPromptMode = "full"
	if cfg.RedactPrompt("everything") != "everything" {
		t.Error("expected full prompt returned in full mode")
	}
}

func TestAuditResponseEmpty(t *testing.T) {
	vertexTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer vertexTS.Close()

	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash", ResponseMode: "audit",
		VertexBase: vertexTS.URL, ModelArmorEndpoint: "https://unused",
		APIKeys:      map[string]bool{},
		TokenInfoURL: "https://oauth2.googleapis.com",
	}
	chain := inspector.NewChain(inspector.NewRegexInspector())
	vc := vertex.NewClient(testCfg, vertexTS.Client())
	srv := handler.NewServer(testCfg, chain, vc, vertexTS.Client(), nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")

	srv.ServeAnthropic(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuditResponseWithSSNInResponse(t *testing.T) {
	blockedTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunks := `[{"candidates":[{"content":{"role":"model","parts":[{"text":"My SSN is 123-45-6789"}]}}]}]` + "\n"
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, chunks)
	}))
	defer blockedTS.Close()

	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash", ResponseMode: "audit",
		VertexBase: blockedTS.URL, ModelArmorEndpoint: "https://unused",
		APIKeys:      map[string]bool{},
		TokenInfoURL: "https://oauth2.googleapis.com",
	}
	chain := inspector.NewChain(inspector.NewRegexInspector())
	vc := vertex.NewClient(testCfg, blockedTS.Client())
	srv := handler.NewServer(testCfg, chain, vc, blockedTS.Client(), nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")

	srv.ServeAnthropic(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "content_block_delta") {
		t.Errorf("expected response to stream despite SSN in audit mode, got: %s", body[:min(len(body), 200)])
	}
}

func TestOpenAIAuditResponseWithSSNInResponse(t *testing.T) {
	blockedTS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunks := `[{"candidates":[{"content":{"role":"model","parts":[{"text":"My SSN is 123-45-6789"}]}}]}]` + "\n"
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, chunks)
	}))
	defer blockedTS.Close()

	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash", ResponseMode: "audit",
		VertexBase: blockedTS.URL, ModelArmorEndpoint: "https://unused",
		APIKeys:      map[string]bool{},
		TokenInfoURL: "https://oauth2.googleapis.com",
	}
	chain := inspector.NewChain(inspector.NewRegexInspector())
	vc := vertex.NewClient(testCfg, blockedTS.Client())
	srv := handler.NewServer(testCfg, chain, vc, blockedTS.Client(), nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")

	srv.ServeOpenAI(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "chat.completion.chunk") {
		t.Errorf("expected response to stream despite SSN in audit mode, got: %s", body[:min(len(body), 200)])
	}
}

func TestOpenAIFastNonStreamingMissingStream(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Hello!", "STOP"))
	defer vertexTS.Close()
	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash", ResponseMode: "fast",
		VertexBase: vertexTS.URL, ModelArmorEndpoint: "https://unused",
		APIKeys:      map[string]bool{},
		TokenInfoURL: "https://oauth2.googleapis.com",
	}
	srv := newCustomServer(testCfg, vertexTS.Client())

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeOpenAI(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected valid JSON, got parse error: %v", err)
	}
}

func TestAnthropicFastNonStreamingMissingStream(t *testing.T) {
	vertexTS := stubVertexOK(makeGeminiResponse("Hello!", "STOP"))
	defer vertexTS.Close()
	testCfg := &config.Config{
		Project: "test-project", Location: "europe-west2",
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash", ResponseMode: "fast",
		VertexBase: vertexTS.URL, ModelArmorEndpoint: "https://unused",
		APIKeys:      map[string]bool{},
		TokenInfoURL: "https://oauth2.googleapis.com",
	}
	srv := newCustomServer(testCfg, vertexTS.Client())

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeAnthropic(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRedactPromptCustomLength(t *testing.T) {
	cfg := &config.Config{LogPromptMode: "truncate", LogPromptLength: 10}
	result := cfg.RedactPrompt("hello world this is long")
	if result != "hello worl..." {
		t.Errorf("expected first 10 chars + ..., got %q", result)
	}

	cfg.LogPromptLength = 0
	result = cfg.RedactPrompt("hello world this is long")
	if strings.HasSuffix(result, "...") {
		t.Errorf("expected no truncation when LogPromptLength=0 (defaults to 32, prompt is shorter), got %q", result)
	}

	cfg.LogPromptLength = -1
	result = cfg.RedactPrompt("hello world this is long")
	if strings.HasSuffix(result, "...") {
		t.Errorf("expected no truncation when LogPromptLength<0 (defaults to 32, prompt is shorter), got %q", result)
	}
}

func TestUserAgentRegexConfig(t *testing.T) {
	os.Setenv("USER_AGENT_REGEX", "^opencode/.*$")
	defer os.Unsetenv("USER_AGENT_REGEX")
	cfg := config.Load()
	if cfg.UserAgentRegex == nil {
		t.Fatal("expected UserAgentRegex to be set")
	}
	if !cfg.UserAgentRegex.MatchString("opencode/1.0") {
		t.Error("expected regex to match opencode/1.0")
	}
	if cfg.UserAgentRegex.MatchString("curl/8.0") {
		t.Error("expected regex not to match curl/8.0")
	}
}

func TestModelArmorEndpointConfig(t *testing.T) {
	os.Setenv("MODEL_ARMOR_LOCATION", "us-central1")
	defer os.Unsetenv("MODEL_ARMOR_LOCATION")
	cfg := config.Load()
	if !strings.Contains(cfg.ModelArmorEndpoint, "us-central1") {
		t.Errorf("expected ModelArmorEndpoint to contain us-central1, got %q", cfg.ModelArmorEndpoint)
	}
}

func TestEICARTestStringsTriggerPromptBlocking(t *testing.T) {
	ri := inspector.NewRegexInspector()

	cases := []struct {
		name  string
		input string
	}{
		{"SSN", inspector.TestSSN},
		{"CreditCard", inspector.TestCreditCard},
		{"PrivateKey", inspector.TestPrivateKey},
		{"AWSKey", inspector.TestAWSKey},
		{"APIKey", inspector.TestAPIKey},
		{"Credentials", inspector.TestCredentials},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			br := ri.InspectPrompt(context.Background(), tc.input, "")
			if br == nil {
				t.Errorf("Test%s (%q) should be blocked but was not", tc.name, tc.input)
			}
		})
	}
}

func TestEICARTestStringsTriggerResponseBlocking(t *testing.T) {
	ri := inspector.NewRegexInspector()

	ssn := ri.InspectResponse(context.Background(), inspector.TestSSN, "")
	if ssn == nil {
		t.Errorf("TestSSN should be blocked in response but was not")
	}

	pk := ri.InspectResponse(context.Background(), inspector.TestPrivateKey, "")
	if pk == nil {
		t.Errorf("TestPrivateKey should be blocked in response but was not")
	}
}

func TestEICARTestStringsAreObviouslyFake(t *testing.T) {
	if !strings.HasPrefix(inspector.TestSSN, "BULWARKAI-TEST") {
		t.Error("TestSSN should have BULWARKAI-TEST prefix")
	}
	if !strings.HasPrefix(inspector.TestCreditCard, "BULWARKAI-TEST") {
		t.Error("TestCreditCard should have BULWARKAI-TEST prefix")
	}
	if !strings.HasPrefix(inspector.TestPrivateKey, "BULWARKAI-TEST") {
		t.Error("TestPrivateKey should have BULWARKAI-TEST prefix")
	}
	if !strings.HasPrefix(inspector.TestAWSKey, "BULWARKAI-TEST") {
		t.Error("TestAWSKey should have BULWARKAI-TEST prefix")
	}
	if !strings.HasPrefix(inspector.TestAPIKey, "BULWARKAI-TEST") {
		t.Error("TestAPIKey should have BULWARKAI-TEST prefix")
	}
	if !strings.HasPrefix(inspector.TestCredentials, "BULWARKAI-TEST") {
		t.Error("TestCredentials should have BULWARKAI-TEST prefix")
	}
}

func TestEICARStringsViaHandler(t *testing.T) {
	srv := newTestServer(nil)
	for _, tc := range []struct {
		name    string
		payload string
	}{
		{"SSN", fmt.Sprintf(`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":%q}]}`, inspector.TestSSN)},
		{"CreditCard", fmt.Sprintf(`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":%q}]}`, inspector.TestCreditCard)},
		{"PrivateKey", fmt.Sprintf(`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":%q}]}`, inspector.TestPrivateKey)},
		{"AWSKey", fmt.Sprintf(`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":%q}]}`, inspector.TestAWSKey)},
		{"APIKey", fmt.Sprintf(`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":%q}]}`, inspector.TestAPIKey)},
		{"Credentials", fmt.Sprintf(`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":%q}]}`, inspector.TestCredentials)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(tc.payload))
			req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.ServeAnthropic(rec, req)

			var body map[string]interface{}
			json.Unmarshal(rec.Body.Bytes(), &body)
			errObj, _ := body["error"].(map[string]interface{})
			if errObj == nil {
				t.Errorf("%s: expected block, got: %s", tc.name, rec.Body.String()[:min(len(rec.Body.String()), 200)])
			}
			msg, _ := errObj["message"].(string)
			if !strings.Contains(msg, "Blocked by bulwarkai") {
				t.Errorf("%s: expected blocked message, got: %s", tc.name, msg)
			}
		})
	}
}

func TestEICARStringsViaOpenAIHandler(t *testing.T) {
	srv := newTestServer(nil)
	for _, tc := range []struct {
		name    string
		payload string
	}{
		{"SSN", fmt.Sprintf(`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":%q}]}`, inspector.TestSSN)},
		{"CreditCard", fmt.Sprintf(`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":%q}]}`, inspector.TestCreditCard)},
		{"PrivateKey", fmt.Sprintf(`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":%q}]}`, inspector.TestPrivateKey)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(tc.payload))
			req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.ServeOpenAI(rec, req)

			var body map[string]interface{}
			json.Unmarshal(rec.Body.Bytes(), &body)
			errObj, _ := body["error"].(map[string]interface{})
			if errObj == nil {
				t.Errorf("%s: expected block, got: %s", tc.name, rec.Body.String()[:min(len(rec.Body.String()), 200)])
			}
		})
	}
}

func TestTestStringsEndpoint(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("GET", "/test-strings", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var body map[string]string
	json.Unmarshal(rec.Body.Bytes(), &body)

	expected := []string{"ssn", "credit_card", "private_key", "aws_key", "api_key", "credentials"}
	for _, key := range expected {
		val, ok := body[key]
		if !ok {
			t.Errorf("expected key %q in response", key)
			continue
		}
		if !strings.HasPrefix(val, "BULWARKAI-TEST") {
			t.Errorf("expected %q to have BULWARKAI-TEST prefix, got %q", key, val)
		}
	}
}

func TestCleanPassesWithEICARPrefix(t *testing.T) {
	ri := inspector.NewRegexInspector()
	br := ri.InspectPrompt(context.Background(), "BULWARKAI-TEST this is safe text", "")
	if br != nil {
		t.Error("BULWARKAI-TEST prefix alone should not trigger blocking")
	}
}

func TestOpenAIModelsList(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("GET", "/v1/models", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if body["object"] != "list" {
		t.Errorf("expected object=list, got %v", body["object"])
	}
	data, ok := body["data"].([]interface{})
	if !ok || len(data) < 2 {
		t.Fatalf("expected data array with models, got %v", body["data"])
	}
	first, _ := data[0].(map[string]interface{})
	if first["id"] == "" {
		t.Error("expected model id")
	}
	if first["object"] != "model" {
		t.Errorf("expected object=model, got %v", first["object"])
	}
	if first["owned_by"] != "bulwarkai" {
		t.Errorf("expected owned_by=bulwarkai, got %v", first["owned_by"])
	}
}

func TestOpenAIModelDetail(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("GET", "/v1/models/gemini-2.5-flash", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if body["id"] != "gemini-2.5-flash" {
		t.Errorf("expected id=gemini-2.5-flash, got %v", body["id"])
	}
	if body["object"] != "model" {
		t.Errorf("expected object=model, got %v", body["object"])
	}
}

func TestOpenAIModelDetailNotFound(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("GET", "/v1/models/nonexistent-model", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestOpenAIModelsMethodNotAllowed(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("POST", "/v1/models", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestOpenAIModelsIncludesFallbackModel(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("GET", "/v1/models", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	var body map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &body)
	data, _ := body["data"].([]interface{})

	found := false
	for _, d := range data {
		m, _ := d.(map[string]interface{})
		if m["id"] == "gemini-2.5-flash" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected gemini-2.5-flash in model list")
	}
}

func TestBulwarkaiHeaderPresent(t *testing.T) {
	srv := newTestServer(nil)
	for _, path := range []string{"/health", "/test-strings", "/v1/models"} {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		srv.Routes().ServeHTTP(rec, req)
		if rec.Header().Get("X-Bulwarkai") == "" {
			t.Errorf("expected X-Bulwarkai header on %s, got empty", path)
		}
		if rec.Header().Get("X-Bulwarkai") == "v1" {
			t.Errorf("expected semver+hash in X-Bulwarkai, got old 'v1' value")
		}
	}
}

func TestBulwarkaiHeaderOnBlockedRequest(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		fmt.Sprintf(`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":%q}]}`, inspector.TestSSN),
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)

	if rec.Header().Get("X-Bulwarkai") == "" {
		t.Errorf("expected X-Bulwarkai header on blocked request, got empty")
	}
}

func TestChainNames(t *testing.T) {
	chain := inspector.NewChain(inspector.NewRegexInspector())
	names := chain.Names()
	if len(names) != 1 || names[0] != "regex" {
		t.Errorf("expected [regex], got %v", names)
	}
}

func TestConcurrentInspectorsAllRun(t *testing.T) {
	var promptCalls, responseCalls int32
	slow := &countingInspector{name: "slow", promptCalls: &promptCalls, responseCalls: &responseCalls}
	fast := &countingInspector{name: "fast", promptCalls: &promptCalls, responseCalls: &responseCalls}
	chain := inspector.NewChain(slow, fast)

	result := chain.ScreenPrompt(context.Background(), "hello", "")
	if result != nil {
		t.Error("expected nil result for clean prompt")
	}
	if promptCalls != 2 {
		t.Errorf("expected 2 prompt calls, got %d", promptCalls)
	}

	result = chain.ScreenResponse(context.Background(), "hello", "")
	if result != nil {
		t.Error("expected nil result for clean response")
	}
	if responseCalls != 2 {
		t.Errorf("expected 2 response calls, got %d", responseCalls)
	}
}

func TestConcurrentInspectorsBlockStillRunsAll(t *testing.T) {
	var calls int32
	blocker := &countingInspector{name: "blocker", block: true, promptCalls: &calls}
	observer := &countingInspector{name: "observer", promptCalls: &calls}
	chain := inspector.NewChain(blocker, observer)

	result := chain.ScreenPrompt(context.Background(), "trigger", "")
	if result == nil {
		t.Error("expected block result")
	}
	if calls != 2 {
		t.Errorf("expected 2 calls even when one blocks, got %d", calls)
	}
}

type countingInspector struct {
	name          string
	block         bool
	promptCalls   *int32
	responseCalls *int32
}

func (c *countingInspector) Name() string       { return c.name }
func (c *countingInspector) TestMethod() string { return "" }
func (c *countingInspector) InspectPrompt(_ context.Context, _ string, _ string) *inspector.BlockResult {
	atomic.AddInt32(c.promptCalls, 1)
	if c.block {
		return &inspector.BlockResult{Blocked: true, Reason: c.name + " blocked"}
	}
	return nil
}
func (c *countingInspector) InspectResponse(_ context.Context, _ string, _ string) *inspector.BlockResult {
	atomic.AddInt32(c.responseCalls, 1)
	if c.block {
		return &inspector.BlockResult{Blocked: true, Reason: c.name + " blocked"}
	}
	return nil
}

func TestRequestIDInResponseHeader(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("X-Request-ID", "custom-req-123")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Header().Get("X-Request-ID") != "custom-req-123" {
		t.Errorf("expected X-Request-ID=custom-req-123, got %q", rec.Header().Get("X-Request-ID"))
	}
}

func TestRequestIDAutoGenerated(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("expected auto-generated X-Request-ID")
	}
}

func TestNosniffHeader(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("expected X-Content-Type-Options: nosniff")
	}
}

func TestMetricsEndpoint(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 from /metrics, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "bulwarkai_active_requests") {
		t.Error("expected bulwarkai_active_requests metric")
	}
}

func TestReadinessEndpoint(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	var body map[string]string
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["status"] != "ready" {
		t.Errorf("expected ready, got %q", body["status"])
	}
}

func TestRequestBodyTooLarge(t *testing.T) {
	srv := newTestServer(nil)
	hugeBody := strings.Repeat("x", 10*1024*1024+100)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"gemini-2.5-flash","max_tokens":4096,"messages":[{"role":"user","content":"`+hugeBody+`"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", rec.Code)
	}
}

func TestInvalidJSONError(t *testing.T) {
	srv := newTestServer(nil)
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader("not json"))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid json") {
		t.Errorf("expected invalid json error, got %q", rec.Body.String())
	}
}

func newDemoTestServer() *handler.Server {
	testCfg := &config.Config{
		Project:             "test-project",
		Location:            "europe-west2",
		AllowedDomains:      []string{"example.com"},
		FallbackGeminiModel: "gemini-2.5-flash",
		ResponseMode:        "strict",
		ModelArmorTemplate:  "test-template",
		ModelArmorLocation:  "europe-west2",
		ModelArmorEndpoint:  "https://unused.example.com",
		APIKeys:             map[string]bool{"test-api-key": true},
		TokenInfoURL:        "https://oauth2.googleapis.com",
		Version:             "demo-test",
		DemoMode:            true,
	}
	chain := inspector.NewChain(inspector.NewRegexInspector())
	demo := vertex.NewDemoClient(testCfg)
	return handler.NewServer(testCfg, chain, demo, http.DefaultClient, nil)
}

func TestDemoModeAnthropicNonStreaming(t *testing.T) {
	srv := newDemoTestServer()
	body := `{"model":"gemini-2.5-flash","max_tokens":1024,"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp["type"] != "message" {
		t.Errorf("expected type=message, got %v", resp["type"])
	}
	text := ""
	if content, ok := resp["content"].([]interface{}); ok && len(content) > 0 {
		if block, ok := content[0].(map[string]interface{}); ok {
			text = fmt.Sprintf("%v", block["text"])
		}
	}
	if !strings.Contains(text, "demo response") {
		t.Errorf("expected demo text in response, got %q", text)
	}
}

func TestDemoModeOpenAINonStreaming(t *testing.T) {
	srv := newDemoTestServer()
	body := `{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if resp["object"] != "chat.completion" {
		t.Errorf("expected object=chat.completion, got %v", resp["object"])
	}
}

func TestDemoModeAnthropicStreaming(t *testing.T) {
	srv := newDemoTestServer()
	body := `{"model":"gemini-2.5-flash","max_tokens":1024,"stream":true,"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := rec.Body.String()
	if !strings.Contains(resp, "event:") {
		t.Errorf("expected SSE events in streaming response, got %q", resp)
	}
}

func TestDemoModeOpenAIStreaming(t *testing.T) {
	srv := newDemoTestServer()
	body := `{"model":"gemini-2.5-flash","stream":true,"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := rec.Body.String()
	if !strings.Contains(resp, "data:") {
		t.Errorf("expected SSE data in streaming response, got %q", resp)
	}
}

func TestDemoModeBlocksSSN(t *testing.T) {
	srv := newDemoTestServer()
	body := `{"model":"gemini-2.5-flash","max_tokens":1024,"messages":[{"role":"user","content":"My SSN is 123-45-6789"}]}`
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Blocked by bulwarkai") {
		t.Errorf("expected block, got %q", rec.Body.String())
	}
}

func TestDemoModeVertexCompat(t *testing.T) {
	srv := newDemoTestServer()
	body := `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`
	req := httptest.NewRequest("POST", "/models/gemini-2.5-flash:generateContent", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+makeTestJWT("user@example.com"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "demo response") {
		t.Errorf("expected demo text, got %q", rec.Body.String())
	}
}
