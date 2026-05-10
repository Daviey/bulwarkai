package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/Daviey/bulwarkai/internal/config"
)

func makeJWT(email string) string {
	header := "eyJhbGciOiJIUzI1NiJ9"
	claims := map[string]interface{}{"email": email}
	claimsJSON, _ := json.Marshal(claims)
	payload := encodeSegment(claimsJSON)
	return header + "." + payload + ".fake-sig"
}

func encodeSegment(data []byte) string {
	const b64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	buf := make([]byte, (len(data)*8+5)/6)
	di, si := 0, 0
	n := (len(data) / 3) * 3
	for si < n {
		val := uint(data[si])<<16 | uint(data[si+1])<<8 | uint(data[si+2])
		buf[di] = b64[val>>18&0x3F]
		buf[di+1] = b64[val>>12&0x3F]
		buf[di+2] = b64[val>>6&0x3F]
		buf[di+3] = b64[val&0x3F]
		di += 4
		si += 3
	}
	rem := len(data) - si
	if rem == 1 {
		val := uint(data[si]) << 16
		buf[di] = b64[val>>18&0x3F]
		buf[di+1] = b64[val>>12&0x3F]
		di += 2
	} else if rem == 2 {
		val := uint(data[si])<<16 | uint(data[si+1])<<8
		buf[di] = b64[val>>18&0x3F]
		buf[di+1] = b64[val>>12&0x3F]
		buf[di+2] = b64[val>>6&0x3F]
		di += 3
	}
	return string(buf[:di])
}

func TestAuthenticate_LocalMode(t *testing.T) {
	cfg := &config.Config{LocalMode: true}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", nil)

	id, ok := Authenticate(cfg, nil, w, r)
	if !ok {
		t.Fatal("expected ok")
	}
	if id.Email != "local@localhost" {
		t.Fatalf("got %q", id.Email)
	}
}

func TestAuthenticate_APIKey_Valid(t *testing.T) {
	cfg := &config.Config{
		APIKeys:        map[string]bool{"my-key": true},
		AllowedDomains: []string{"example.com"},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("X-Api-Key", "my-key")

	id, ok := Authenticate(cfg, nil, w, r)
	if !ok {
		t.Fatal("expected ok")
	}
	if id.Email != "apikey@example.com" {
		t.Fatalf("got %q", id.Email)
	}
}

func TestAuthenticate_APIKey_Invalid(t *testing.T) {
	cfg := &config.Config{
		APIKeys: map[string]bool{"good-key": true},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("X-Api-Key", "bad-key")

	_, ok := Authenticate(cfg, nil, w, r)
	if ok {
		t.Fatal("expected rejection")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", w.Code)
	}
}

func TestAuthenticate_MissingAuth(t *testing.T) {
	cfg := &config.Config{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", nil)

	_, ok := Authenticate(cfg, nil, w, r)
	if ok {
		t.Fatal("expected rejection")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", w.Code)
	}
}

func TestAuthenticate_TokenInfo(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"email": "user@example.com"})
	}))
	defer ts.Close()

	cfg := &config.Config{
		AllowedDomains: []string{"example.com"},
		TokenInfoURL:   ts.URL,
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("Authorization", "Bearer test-token")
	r.Header.Set("X-Forwarded-Access-Token", "oauth-token")

	id, ok := Authenticate(cfg, ts.Client(), w, r)
	if !ok {
		t.Fatal("expected ok")
	}
	if id.Email != "user@example.com" {
		t.Fatalf("got %q", id.Email)
	}
	if id.AccessToken != "oauth-token" {
		t.Fatalf("got %q", id.AccessToken)
	}
}

func TestAuthenticate_JWTFallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	cfg := &config.Config{
		AllowedDomains: []string{"test.com"},
		TokenInfoURL:   ts.URL,
	}
	token := makeJWT("dev@test.com")
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("Authorization", "Bearer "+token)

	id, ok := Authenticate(cfg, ts.Client(), w, r)
	if !ok {
		t.Fatal("expected ok")
	}
	if id.Email != "dev@test.com" {
		t.Fatalf("got %q", id.Email)
	}
}

func TestAuthenticate_DomainDenied(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"email": "user@evil.com"})
	}))
	defer ts.Close()

	cfg := &config.Config{
		AllowedDomains: []string{"example.com"},
		TokenInfoURL:   ts.URL,
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("Authorization", "Bearer test-token")

	_, ok := Authenticate(cfg, ts.Client(), w, r)
	if ok {
		t.Fatal("expected rejection")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d", w.Code)
	}
}

func TestAuthenticate_NoDomainRestriction(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"email": "user@anywhere.com"})
	}))
	defer ts.Close()

	cfg := &config.Config{
		TokenInfoURL: ts.URL,
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("Authorization", "Bearer test-token")

	id, ok := Authenticate(cfg, ts.Client(), w, r)
	if !ok {
		t.Fatal("expected ok")
	}
	if id.Email != "user@anywhere.com" {
		t.Fatalf("got %q", id.Email)
	}
}

func TestAuthenticate_EmailWithNoAtSign(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"email": "notanemail"})
	}))
	defer ts.Close()

	cfg := &config.Config{
		AllowedDomains: []string{"example.com"},
		TokenInfoURL:   ts.URL,
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("Authorization", "Bearer test-token")

	_, ok := Authenticate(cfg, ts.Client(), w, r)
	if ok {
		t.Fatal("expected rejection for email without @")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d", w.Code)
	}
}

func TestExtractEmailFromJWT(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{"valid", makeJWT("user@example.com"), "user@example.com"},
		{"uppercase", makeJWT("User@Example.COM"), "user@example.com"},
		{"empty", "", ""},
		{"single_part", "not-a-jwt", ""},
		{"invalid_b64", "a.!!!.c", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractEmailFromJWT(tt.token)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckUserAgent_NoRegex(t *testing.T) {
	cfg := &config.Config{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)

	if !CheckUserAgent(cfg, w, r) {
		t.Fatal("expected pass when no regex configured")
	}
}

func TestCheckUserAgent_Match(t *testing.T) {
	cfg := &config.Config{
		UserAgentRegex: regexp.MustCompile(`^opencode/`),
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "opencode/1.0")

	if !CheckUserAgent(cfg, w, r) {
		t.Fatal("expected pass")
	}
}

func TestCheckUserAgent_Mismatch(t *testing.T) {
	cfg := &config.Config{
		UserAgentRegex: regexp.MustCompile(`^opencode/`),
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("User-Agent", "curl/8.0")

	if CheckUserAgent(cfg, w, r) {
		t.Fatal("expected rejection")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d", w.Code)
	}
}
