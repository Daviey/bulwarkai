package inspector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Daviey/bulwarkai/internal/config"
)

func TestDLPInspector_Blocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"findings": []interface{}{
					map[string]interface{}{
						"infoType": map[string]interface{}{"name": "US_SOCIAL_SECURITY_NUMBER"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	d := &dlpInspector{
		project:   "test",
		location:  "us",
		infoTypes: []string{"US_SOCIAL_SECURITY_NUMBER"},
		client:    srv.Client(),
		endpoint:  srv.URL,
	}
	br := d.InspectPrompt(context.Background(), "SSN here", "token")
	if br == nil || !br.Blocked {
		t.Fatal("should be blocked")
	}
	if br.Reason != "DLP: US_SOCIAL_SECURITY_NUMBER" {
		t.Fatalf("unexpected reason: %s", br.Reason)
	}
}

func TestDLPInspector_Clean(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"findings": []interface{}{},
			},
		})
	}))
	defer srv.Close()

	d := &dlpInspector{
		project:   "test",
		location:  "us",
		infoTypes: []string{"US_SOCIAL_SECURITY_NUMBER"},
		client:    srv.Client(),
		endpoint:  srv.URL,
	}
	br := d.InspectPrompt(context.Background(), "clean text", "token")
	if br != nil {
		t.Fatal("clean text should pass")
	}
}

func TestDLPInspector_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	d := &dlpInspector{
		project:   "test",
		location:  "us",
		infoTypes: []string{"US_SOCIAL_SECURITY_NUMBER"},
		client:    srv.Client(),
		endpoint:  srv.URL,
	}
	br := d.InspectPrompt(context.Background(), "text", "token")
	if br == nil || br.Err == nil {
		t.Fatal("should fail-open with error")
	}
}

func TestDLPInspector_DedupFindings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"findings": []interface{}{
					map[string]interface{}{"infoType": map[string]interface{}{"name": "EMAIL_ADDRESS"}},
					map[string]interface{}{"infoType": map[string]interface{}{"name": "EMAIL_ADDRESS"}},
					map[string]interface{}{"infoType": map[string]interface{}{"name": "PHONE_NUMBER"}},
				},
			},
		})
	}))
	defer srv.Close()

	d := &dlpInspector{
		project:   "test",
		location:  "us",
		infoTypes: []string{"EMAIL_ADDRESS", "PHONE_NUMBER"},
		client:    srv.Client(),
		endpoint:  srv.URL,
	}
	br := d.InspectPrompt(context.Background(), "contact info", "token")
	if br == nil || !br.Blocked {
		t.Fatal("should be blocked")
	}
	if br.Reason != "DLP: EMAIL_ADDRESS, PHONE_NUMBER" {
		t.Fatalf("expected deduped types, got: %s", br.Reason)
	}
}

func TestDLPInspector_InspectResponseSameAsPrompt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"findings": []interface{}{
					map[string]interface{}{"infoType": map[string]interface{}{"name": "CREDIT_CARD_NUMBER"}},
				},
			},
		})
	}))
	defer srv.Close()

	d := &dlpInspector{
		project:   "test",
		location:  "us",
		infoTypes: []string{"CREDIT_CARD_NUMBER"},
		client:    srv.Client(),
		endpoint:  srv.URL,
	}
	br := d.InspectResponse(context.Background(), "cc here", "token")
	if br == nil || !br.Blocked {
		t.Fatal("response should also be blocked")
	}
}

func TestDLPInspector_ConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	d := &dlpInspector{
		project:   "test",
		location:  "us",
		infoTypes: []string{"US_SOCIAL_SECURITY_NUMBER"},
		client:    srv.Client(),
		endpoint:  srv.URL,
	}
	br := d.InspectPrompt(context.Background(), "text", "token")
	if br == nil || br.Err == nil {
		t.Fatal("connection error should fail-open")
	}
}

func TestModelArmorInspector_Blocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sanitizationResult": map[string]interface{}{
				"invocationResult": "SUCCESS",
				"filterMatchState": "MATCH_FOUND",
				"filterResults": map[string]interface{}{
					"rai": map[string]interface{}{
						"raiFilterResult": map[string]interface{}{"matchState": "MATCH_FOUND"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	m := &modelArmorInspector{
		endpoint: srv.URL,
		project:  "test",
		location: "us",
		template: "tmpl",
		client:   srv.Client(),
	}
	br := m.InspectPrompt(context.Background(), "bad stuff", "token")
	if br == nil || !br.Blocked {
		t.Fatal("should be blocked")
	}
	if br.Reason != "Model Armor: rai" {
		t.Fatalf("unexpected reason: %s", br.Reason)
	}
}

func TestModelArmorInspector_Clean(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sanitizationResult": map[string]interface{}{
				"invocationResult": "SUCCESS",
				"filterMatchState": "NO_MATCH_FOUND",
			},
		})
	}))
	defer srv.Close()

	m := &modelArmorInspector{
		endpoint: srv.URL,
		project:  "test",
		location: "us",
		template: "tmpl",
		client:   srv.Client(),
	}
	br := m.InspectPrompt(context.Background(), "clean", "token")
	if br != nil {
		t.Fatal("clean should pass")
	}
}

func TestModelArmorInspector_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	m := &modelArmorInspector{
		endpoint: srv.URL,
		project:  "test",
		location: "us",
		template: "tmpl",
		client:   srv.Client(),
	}
	br := m.InspectPrompt(context.Background(), "text", "token")
	if br == nil || br.Err == nil {
		t.Fatal("HTTP error should fail-open")
	}
}

func TestModelArmorInspector_ConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	m := &modelArmorInspector{
		endpoint: srv.URL,
		project:  "test",
		location: "us",
		template: "tmpl",
		client:   srv.Client(),
	}
	br := m.InspectPrompt(context.Background(), "text", "token")
	if br == nil || br.Err == nil {
		t.Fatal("connection error should fail-open")
	}
}

func TestModelArmorInspector_ResponseBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sanitizationResult": map[string]interface{}{
				"invocationResult": "SUCCESS",
				"filterMatchState": "MATCH_FOUND",
				"filterResults": map[string]interface{}{
					"malicious_uri": map[string]interface{}{
						"maliciousUriFilterResult": map[string]interface{}{"matchState": "MATCH_FOUND"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	m := &modelArmorInspector{
		endpoint: srv.URL,
		project:  "test",
		location: "us",
		template: "tmpl",
		client:   srv.Client(),
	}
	br := m.InspectResponse(context.Background(), "bad url", "token")
	if br == nil || !br.Blocked {
		t.Fatal("response should be blocked")
	}
	if br.Reason != "Model Armor: malicious_uri" {
		t.Fatalf("unexpected reason: %s", br.Reason)
	}
}

func TestModelArmorInspector_MultipleFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"sanitizationResult": map[string]interface{}{
				"invocationResult": "SUCCESS",
				"filterMatchState": "MATCH_FOUND",
				"filterResults": map[string]interface{}{
					"rai": map[string]interface{}{
						"raiFilterResult": map[string]interface{}{"matchState": "MATCH_FOUND"},
					},
					"pi_jailbreak": map[string]interface{}{
						"piAndJailbreakFilterResult": map[string]interface{}{"matchState": "MATCH_FOUND"},
					},
					"clean_filter": map[string]interface{}{
						"raiFilterResult": map[string]interface{}{"matchState": "NO_MATCH_FOUND"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	m := &modelArmorInspector{
		endpoint: srv.URL,
		project:  "test",
		location: "us",
		template: "tmpl",
		client:   srv.Client(),
	}
	br := m.InspectPrompt(context.Background(), "multi", "token")
	if br == nil || !br.Blocked {
		t.Fatal("should be blocked")
	}
	if !strings.Contains(br.Reason, "rai") || !strings.Contains(br.Reason, "pi_jailbreak") {
		t.Fatalf("expected both matching filters, got: %s", br.Reason)
	}
}

func TestNewModelArmorInspector(t *testing.T) {
	cfg := &config.Config{
		Project:            "proj",
		ModelArmorLocation: "eu",
		ModelArmorTemplate: "tmpl",
		ModelArmorEndpoint: "https://example.com",
		ResponseMode:       "strict",
	}
	m := NewModelArmorInspector(cfg, http.DefaultClient)
	if m.Name() != "model_armor" {
		t.Fatal("name should be model_armor")
	}
	if m.TestMethod() == "" {
		t.Fatal("test method should not be empty")
	}
}

func TestNewDLPInspector(t *testing.T) {
	cfg := &config.Config{
		Project:  "proj",
		Location: "eu",
	}
	d := NewDLPInspector(cfg, http.DefaultClient)
	if d.Name() != "dlp" {
		t.Fatal("name should be dlp")
	}
	if d.TestMethod() == "" {
		t.Fatal("test method should not be empty")
	}
}

func TestChain_Names(t *testing.T) {
	chain := NewChain(NewRegexInspector())
	names := chain.Names()
	if len(names) != 1 || names[0] != "regex" {
		t.Fatalf("expected [regex], got %v", names)
	}
}

func TestChain_ScreenResponse(t *testing.T) {
	chain := NewChain(NewRegexInspector())
	br := chain.ScreenResponse(context.Background(), "clean response", "")
	if br != nil {
		t.Fatal("clean response should pass")
	}

	br = chain.ScreenResponse(context.Background(), "SSN: 123-45-6789", "")
	if br == nil || !br.Blocked {
		t.Fatal("SSN in response should be blocked")
	}
}

func TestRegexInspector_ResponseRules(t *testing.T) {
	r := NewRegexInspector()
	br := r.InspectResponse(context.Background(), "key:\n-----BEGIN RSA PRIVATE KEY-----\n...", "")
	if br == nil || !br.Blocked {
		t.Fatal("private key in response should be blocked")
	}

	br = r.InspectResponse(context.Background(), "clean", "")
	if br != nil {
		t.Fatal("clean response should pass")
	}
}

func TestChain_Empty(t *testing.T) {
	chain := NewChain()
	br := chain.ScreenPrompt(context.Background(), "anything", "")
	if br != nil {
		t.Fatal("empty chain should pass")
	}
}

func TestRegexInspector_AllTestStrings(t *testing.T) {
	r := NewRegexInspector()
	tests := []struct {
		input string
		name  string
	}{
		{TestSSN, "SSN detected"},
		{TestCreditCard, "Credit card number detected"},
		{TestPrivateKey, "Private key detected"},
		{TestAWSKey, "AWS access key detected"},
		{TestAPIKey, "API key detected"},
		{TestCredentials, "Credentials in prompt"},
	}
	for _, tt := range tests {
		br := r.InspectPrompt(context.Background(), tt.input, "")
		if br == nil || !br.Blocked {
			t.Fatalf("%s should be blocked by %s", tt.input, tt.name)
		}
		if br.Reason != tt.name {
			t.Fatalf("expected reason %q, got %q for input %s", tt.name, br.Reason, tt.input)
		}
	}
}
