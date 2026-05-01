package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	Project             string
	Location            string
	AllowedDomains      []string
	UserAgentRegex      *regexp.Regexp
	FallbackGeminiModel string
	ResponseMode        string
	ModelArmorTemplate  string
	ModelArmorLocation  string
	ModelArmorEndpoint  string
	APIKeys             map[string]bool
	VertexBase          string
	TokenInfoURL        string
	LocalMode           bool
	DemoMode            bool
	Port                string
	LogLevel            string
	LogPromptMode       string
	LogPromptLength     int
	Version             string
	OPAEnabled          bool
	OPAPolicyFile       string
	OPAPolicyURL        string
	RateLimit           int
	RateLimitWindow     string
	WebhookURL          string
	WebhookSecret       string
	CORSOrigin          string
	MaxBodySize         int
	CBMaxFailures       int
	CBResetTimeout      string
}

func Load() *Config {
	c := &Config{ //nosec G101
		Project:             EnvOr("GOOGLE_CLOUD_PROJECT", ""),
		Location:            EnvOr("GOOGLE_CLOUD_LOCATION", "europe-west2"),
		AllowedDomains:      SplitEnv("ALLOWED_DOMAINS", ""),
		FallbackGeminiModel: EnvOr("FALLBACK_GEMINI_MODEL", "gemini-2.5-flash"),
		ResponseMode:        normalizeMode(EnvOr("RESPONSE_MODE", "strict")),
		ModelArmorTemplate:  EnvOr("MODEL_ARMOR_TEMPLATE", "test-template"),
		ModelArmorLocation:  EnvOr("MODEL_ARMOR_LOCATION", "europe-west2"),
		VertexBase:          "https://" + EnvOr("GOOGLE_CLOUD_LOCATION", "europe-west2") + "-aiplatform.googleapis.com/v1",
		TokenInfoURL:        "https://oauth2.googleapis.com/tokeninfo",
		LocalMode:           os.Getenv("LOCAL_MODE") == "true",
		DemoMode:            os.Getenv("DEMO_MODE") == "true",
		Port:                EnvOr("PORT", "8080"),
		LogLevel:            EnvOr("LOG_LEVEL", "info"),
		LogPromptMode:       EnvOr("LOG_PROMPT_MODE", "truncate"),
		LogPromptLength:     32,
		APIKeys:             MapKeys("API_KEYS"),
		Version:             "dev",
		OPAEnabled:          os.Getenv("OPA_ENABLED") == "true",
		OPAPolicyFile:       EnvOr("OPA_POLICY_FILE", ""),
		OPAPolicyURL:        EnvOr("OPA_POLICY_URL", ""),
		RateLimit:           EnvInt("RATE_LIMIT", 0),
		RateLimitWindow:     EnvOr("RATE_LIMIT_WINDOW", "1m"),
		WebhookURL:          EnvOr("WEBHOOK_URL", ""),
		WebhookSecret:       EnvOr("WEBHOOK_SECRET", ""),
		CORSOrigin:          EnvOr("CORS_ORIGIN", ""),
		MaxBodySize:         EnvInt("MAX_BODY_SIZE", 10*1024*1024),
		CBMaxFailures:       EnvInt("CB_MAX_FAILURES", 5),
		CBResetTimeout:      EnvOr("CB_RESET_TIMEOUT", "30s"),
	}
	c.ModelArmorEndpoint = "https://modelarmor." + c.ModelArmorLocation + ".rep.googleapis.com"
	if ua := os.Getenv("USER_AGENT_REGEX"); ua != "" {
		c.UserAgentRegex = regexp.MustCompile(ua)
	}
	return c
}

func normalizeMode(mode string) string {
	switch mode {
	case "input_only":
		return "fast"
	case "buffer":
		return "audit"
	default:
		return mode
	}
}

func EnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func EnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func SplitEnv(key, def string) []string {
	v := os.Getenv(key)
	if v == "" {
		v = def
	}
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func MapKeys(key string) map[string]bool {
	m := map[string]bool{}
	for _, k := range SplitEnv(key, "") {
		if k != "" {
			m[k] = true
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func Contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func (c *Config) RedactPrompt(prompt string) string {
	if prompt == "" {
		return ""
	}
	switch c.LogPromptMode {
	case "none":
		return ""
	case "hash":
		h := sha256.Sum256([]byte(prompt))
		return fmt.Sprintf("%x", h[:8])
	case "full":
		return prompt
	default:
		n := c.LogPromptLength
		if n <= 0 {
			n = 32
		}
		if len(prompt) > n {
			return prompt[:n] + "..."
		}
		return prompt
	}
}
