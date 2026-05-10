package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Daviey/bulwarkai/internal/config"
)

type Identity struct {
	Email       string
	AccessToken string
}

func Authenticate(cfg *config.Config, httpClient *http.Client, w http.ResponseWriter, r *http.Request) (*Identity, bool) {
	if cfg.LocalMode {
		return &Identity{Email: "local@localhost"}, true
	}

	if key := r.Header.Get("X-Api-Key"); key != "" {
		if cfg.APIKeys != nil && cfg.APIKeys[key] {
			domain := "unknown"
			if len(cfg.AllowedDomains) > 0 {
				domain = cfg.AllowedDomains[0]
			}
			return &Identity{Email: "apikey@" + domain, AccessToken: ""}, true
		}
		http.Error(w, "invalid API key", http.StatusUnauthorized)
		return nil, false
	}

	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		http.Error(w, "missing Authorization header", http.StatusUnauthorized)
		return nil, false
	}

	accessToken := r.Header.Get("X-Forwarded-Access-Token")

	email := ""
	ti, err := tokenInfo(r.Context(), httpClient, cfg.TokenInfoURL, token)
	if err == nil && ti["email"] != nil {
		email = strings.ToLower(fmt.Sprintf("%v", ti["email"]))
	}
	if email == "" {
		email = ExtractEmailFromJWT(token)
	}
	if email == "" {
		http.Error(w, "cannot identify caller", http.StatusUnauthorized)
		return nil, false
	}

	if len(cfg.AllowedDomains) > 0 {
		parts := strings.SplitN(email, "@", 2)
		domain := "unknown"
		if len(parts) == 2 {
			domain = parts[1]
		}
		if len(parts) != 2 || !config.Contains(cfg.AllowedDomains, domain) {
			slog.Warn("DENY_DOMAIN", "action", "DENY_DOMAIN", "email", email, "domain", domain)
			http.Error(w, "domain not allowed", http.StatusForbidden)
			return nil, false
		}
	}

	return &Identity{Email: email, AccessToken: accessToken}, true
}

func CheckUserAgent(cfg *config.Config, w http.ResponseWriter, r *http.Request) bool {
	if cfg.UserAgentRegex == nil {
		return true
	}
	if !cfg.UserAgentRegex.MatchString(r.UserAgent()) {
		slog.Warn("DENY_UA", "action", "DENY_UA", "ua", r.UserAgent())
		http.Error(w, "User-Agent not allowed", http.StatusForbidden)
		return false
	}
	return true
}

func tokenInfo(ctx context.Context, httpClient *http.Client, tokenInfoURL, token string) (map[string]interface{}, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, "GET", tokenInfoURL+"?token="+token, nil) //nosec G704
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req) //nosec G704
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tokeninfo returned %d", resp.StatusCode)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("tokeninfo decode: %w", err)
	}
	return result, nil
}

func ExtractEmailFromJWT(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]interface{}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	email, _ := claims["email"].(string)
	return strings.ToLower(email)
}
