package config

import (
	"os"
	"testing"
)

func TestEnvOr_Exists(t *testing.T) {
	os.Setenv("TEST_ENV_OR", "value")
	defer os.Unsetenv("TEST_ENV_OR")
	if got := EnvOr("TEST_ENV_OR", "default"); got != "value" {
		t.Fatalf("got %q", got)
	}
}

func TestEnvOr_Default(t *testing.T) {
	if got := EnvOr("TEST_MISSING_XYZ", "default"); got != "default" {
		t.Fatalf("got %q", got)
	}
}

func TestSplitEnv(t *testing.T) {
	os.Setenv("TEST_SPLIT", "a, b ,c")
	defer os.Unsetenv("TEST_SPLIT")
	got := SplitEnv("TEST_SPLIT", "")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("got %v", got)
	}
}

func TestSplitEnv_Empty(t *testing.T) {
	got := SplitEnv("TEST_MISSING_XYZ", "")
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestSplitEnv_Default(t *testing.T) {
	got := SplitEnv("TEST_MISSING_XYZ", "x,y")
	if len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Fatalf("got %v", got)
	}
}

func TestMapKeys(t *testing.T) {
	os.Setenv("TEST_MAPKEYS", "key1,key2")
	defer os.Unsetenv("TEST_MAPKEYS")
	got := MapKeys("TEST_MAPKEYS")
	if !got["key1"] || !got["key2"] {
		t.Fatalf("got %v", got)
	}
}

func TestMapKeys_Empty(t *testing.T) {
	got := MapKeys("TEST_MISSING_XYZ")
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestContains(t *testing.T) {
	s := []string{"a", "b", "c"}
	if !Contains(s, "b") {
		t.Fatal("expected true")
	}
	if Contains(s, "z") {
		t.Fatal("expected false")
	}
}

func TestNormalizeMode(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"strict", "strict"},
		{"fast", "fast"},
		{"audit", "audit"},
		{"input_only", "fast"},
		{"buffer", "audit"},
	}
	for _, tt := range tests {
		got := normalizeMode(tt.input)
		if got != tt.want {
			t.Errorf("normalizeMode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLoad_Defaults(t *testing.T) {
	for _, k := range []string{
		"GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION", "ALLOWED_DOMAINS",
		"RESPONSE_MODE", "MODEL_ARMOR_TEMPLATE", "LOCAL_MODE", "DEMO_MODE",
		"PORT", "LOG_LEVEL", "LOG_PROMPT_MODE", "API_KEYS", "USER_AGENT_REGEX",
	} {
		os.Unsetenv(k)
	}
	cfg := Load()
	if cfg.Location != "europe-west2" {
		t.Fatalf("got %q", cfg.Location)
	}
	if cfg.ResponseMode != "strict" {
		t.Fatalf("got %q", cfg.ResponseMode)
	}
	if cfg.Port != "8080" {
		t.Fatalf("got %q", cfg.Port)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("got %q", cfg.LogLevel)
	}
	if cfg.LogPromptMode != "truncate" {
		t.Fatalf("got %q", cfg.LogPromptMode)
	}
	if cfg.LocalMode {
		t.Fatal("expected false")
	}
	if cfg.DemoMode {
		t.Fatal("expected false")
	}
	if cfg.UserAgentRegex != nil {
		t.Fatal("expected nil")
	}
	if cfg.APIKeys != nil {
		t.Fatal("expected nil")
	}
}

func TestLoad_Overrides(t *testing.T) {
	os.Setenv("GOOGLE_CLOUD_PROJECT", "my-project")
	os.Setenv("GOOGLE_CLOUD_LOCATION", "us-central1")
	os.Setenv("RESPONSE_MODE", "fast")
	os.Setenv("PORT", "9090")
	os.Setenv("LOCAL_MODE", "true")
	os.Setenv("DEMO_MODE", "true")
	os.Setenv("API_KEYS", "k1,k2")
	os.Setenv("USER_AGENT_REGEX", "^test/")
	defer func() {
		for _, k := range []string{
			"GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION", "RESPONSE_MODE",
			"PORT", "LOCAL_MODE", "DEMO_MODE", "API_KEYS", "USER_AGENT_REGEX",
		} {
			os.Unsetenv(k)
		}
	}()

	cfg := Load()
	if cfg.Project != "my-project" {
		t.Fatalf("got %q", cfg.Project)
	}
	if cfg.Location != "us-central1" {
		t.Fatalf("got %q", cfg.Location)
	}
	if cfg.ResponseMode != "fast" {
		t.Fatalf("got %q", cfg.ResponseMode)
	}
	if cfg.Port != "9090" {
		t.Fatalf("got %q", cfg.Port)
	}
	if !cfg.LocalMode {
		t.Fatal("expected true")
	}
	if !cfg.DemoMode {
		t.Fatal("expected true")
	}
	if !cfg.APIKeys["k1"] || !cfg.APIKeys["k2"] {
		t.Fatalf("got %v", cfg.APIKeys)
	}
	if cfg.UserAgentRegex == nil || cfg.UserAgentRegex.String() != "^test/" {
		t.Fatal("expected regex")
	}
}

func TestRedactPrompt(t *testing.T) {
	cfg := &Config{LogPromptMode: "truncate", LogPromptLength: 32}

	got := cfg.RedactPrompt("short")
	if got != "short" {
		t.Fatalf("got %q", got)
	}

	longPrompt := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaX"
	got = cfg.RedactPrompt(longPrompt)
	if got != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa..." {
		t.Fatalf("got %q", got)
	}

	cfg.LogPromptMode = "none"
	got = cfg.RedactPrompt("secret")
	if got != "" {
		t.Fatalf("got %q", got)
	}

	cfg.LogPromptMode = "full"
	got = cfg.RedactPrompt("secret")
	if got != "secret" {
		t.Fatalf("got %q", got)
	}

	cfg.LogPromptMode = "hash"
	got = cfg.RedactPrompt("secret")
	if len(got) != 16 {
		t.Fatalf("hash should be 16 hex chars, got %q", got)
	}

	got = (&Config{LogPromptMode: "truncate"}).RedactPrompt("")
	if got != "" {
		t.Fatalf("got %q", got)
	}
}
