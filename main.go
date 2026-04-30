// @title Bulwarkai Proxy Service
// @version 1.0
// @description AI safety proxy that screens every request and response between client applications and Google Vertex AI.
// @description Supports Anthropic Messages, OpenAI Chat Completions, and Vertex AI Gemini native formats.
// @contact.name Dave Walker
// @contact.url https://github.com/Daviey/bulwarkai
// @license.name Apache-2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0
// @securitydefinitions.apikey BearerToken
// @in header
// @name Authorization
// @securitydefinitions.apikey ApiKey
// @in header
// @name X-Api-Key
// @securitydefinitions.apikey ForwardedAccessToken
// @in header
// @name X-Forwarded-Access-Token
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Daviey/bulwarkai/internal/config"
	"github.com/Daviey/bulwarkai/internal/handler"
	"github.com/Daviey/bulwarkai/internal/inspector"
	"github.com/Daviey/bulwarkai/internal/policy"
	"github.com/Daviey/bulwarkai/internal/ratelimit"
	"github.com/Daviey/bulwarkai/internal/vertex"
	"github.com/Daviey/bulwarkai/internal/webhook"
)

var version = "dev"

func init() {
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
}

func main() {
	cfg := config.Load()
	cfg.Version = version

	httpClient := &http.Client{
		Timeout: 240 * time.Second,
	}

	var inspectors []inspector.Inspector
	inspectors = append(inspectors, inspector.NewRegexInspector())
	if cfg.ModelArmorTemplate != "" && cfg.ResponseMode != "strict" {
		inspectors = append(inspectors, inspector.NewModelArmorInspector(cfg, httpClient))
	}
	if os.Getenv("DLP_API") == "true" {
		inspectors = append(inspectors, inspector.NewDLPInspector(cfg, httpClient))
	}
	chain := inspector.NewChain(inspectors...)

	var policyEngine *policy.Engine
	if cfg.OPAEnabled {
		pe, err := policy.NewEngineWithHTTP(context.Background(), true, cfg.OPAPolicyFile, cfg.OPAPolicyURL, httpClient)
		if err != nil {
			slog.Error("opa init failed", "error", err)
			os.Exit(1)
		}
		policyEngine = pe
		slog.Info("opa policy engine enabled")
	}

	var wh *webhook.Notifier
	if cfg.WebhookURL != "" {
		wh = webhook.NewNotifier(cfg.WebhookURL, cfg.WebhookSecret, 256)
		wh.Start()
		slog.Info("webhook notifier enabled", "url", cfg.WebhookURL)
	}

	var caller vertex.VertexCaller
	if cfg.DemoMode {
		caller = vertex.NewDemoClient(cfg)
		slog.Info("DEMO_MODE enabled: returning canned responses, no Vertex AI calls")
	} else {
		caller = vertex.NewClient(cfg, httpClient)
	}

	var rateLimiter *ratelimit.Limiter
	if cfg.RateLimit > 0 {
		window := parseDuration(cfg.RateLimitWindow, time.Minute)
		rateLimiter = ratelimit.NewLimiter(cfg.RateLimit, window)
		slog.Info("rate limiting enabled", "limit", cfg.RateLimit, "window", cfg.RateLimitWindow)
		go func() {
			ticker := time.NewTicker(window)
			defer ticker.Stop()
			for range ticker.C {
				rateLimiter.Cleanup()
			}
		}()
	}

	server := handler.NewServer(cfg, chain, caller, httpClient, policyEngine, wh, rateLimiter)

	slog.Info("bulwarkai starting", "version", version, "mode", cfg.ResponseMode, "inspectors", chain.Names(), "template", cfg.ModelArmorTemplate, "model", cfg.FallbackGeminiModel, "local", cfg.LocalMode, "demo", cfg.DemoMode)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		slog.Info("bulwarkai listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit
	slog.Info("shutting down", "timeout", "30s")
	if policyEngine != nil {
		policyEngine.Stop()
	}
	if wh != nil {
		wh.Stop()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
	slog.Info("shutdown complete")
}

func parseDuration(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}
