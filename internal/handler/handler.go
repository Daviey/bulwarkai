package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Daviey/bulwarkai/internal/auth"
	"github.com/Daviey/bulwarkai/internal/config"
	"github.com/Daviey/bulwarkai/internal/inspector"
	"github.com/Daviey/bulwarkai/internal/metrics"
	"github.com/Daviey/bulwarkai/internal/policy"
	"github.com/Daviey/bulwarkai/internal/vertex"
	"github.com/Daviey/bulwarkai/internal/webhook"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type contextKey string

const requestIDKey contextKey = "request_id"

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

type Server struct {
	cfg         *config.Config
	chain       inspector.Chain
	vertex      vertex.VertexCaller
	httpClient  *http.Client
	policy      *policy.Engine
	webhook     *webhook.Notifier
	rateLimiter RateLimiter
}

type RateLimiter interface {
	Allow(key string) bool
}

func NewServer(cfg *config.Config, chain inspector.Chain, vc vertex.VertexCaller, httpClient *http.Client, eng *policy.Engine, wh *webhook.Notifier, rl RateLimiter) *Server {
	return &Server{cfg: cfg, chain: chain, vertex: vc, httpClient: httpClient, policy: eng, webhook: wh, rateLimiter: rl}
}

func ctxLogger(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(contextKey("logger")).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", s.ServeAnthropic)
	mux.HandleFunc("/v1/chat/completions", s.ServeOpenAI)
	mux.HandleFunc("/v1/models", s.ServeOpenAIModels)
	mux.HandleFunc("/v1/models/", s.ServeOpenAIModelDetail)
	mux.HandleFunc("/health", s.healthHandler)
	mux.HandleFunc("/ready", s.readinessHandler)
	mux.HandleFunc("/test-strings", s.testStringsHandler)
	mux.HandleFunc("/models/", s.ServeVertexCompat)
	mux.HandleFunc("/projects/", s.ServeVertexProject)
	mux.HandleFunc("/v1/projects/", s.ServeVertexProject)
	mux.Handle("/metrics", promhttp.Handler())
	return s.corsMiddleware(s.rateLimitMiddleware(s.requestMiddleware(mux)))
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	origin := s.cfg.CORSOrigin
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Api-Key, X-Forwarded-Access-Token, X-Request-ID")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	if s.rateLimiter == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email := extractEmailFromRequest(r)
		if email != "" && !s.rateLimiter.Allow(email) {
			metrics.RequestsTotal.WithLabelValues("RATE_LIMITED", "").Inc()
			metrics.RateLimitExceeded.WithLabelValues(email).Inc()
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func extractEmailFromRequest(r *http.Request) string {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		return ""
	}
	return auth.ExtractEmailFromJWT(token)
}

func (s *Server) requestMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = uuid.New().String()[:16]
		}
		traceID := r.Header.Get("X-Cloud-Trace-Context")
		if idx := strings.Index(traceID, "/"); idx > 0 {
			traceID = traceID[:idx]
		}
		email := extractEmailFromRequest(r)
		ctx := context.WithValue(r.Context(), requestIDKey, reqID)
		logger := slog.With("request_id", reqID, "method", r.Method, "path", r.URL.Path, "email", email)
		if traceID != "" {
			logger = logger.With("trace_id", traceID)
		}
		ctx = context.WithValue(ctx, contextKey("logger"), logger)
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		w.Header().Set("X-Bulwarkai", s.cfg.Version)
		w.Header().Set("X-Request-ID", reqID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		metrics.ActiveRequests.Inc()
		defer metrics.ActiveRequests.Dec()
		if r.ContentLength > 0 {
			metrics.RequestBodySize.Observe(float64(r.ContentLength))
		}
		next.ServeHTTP(sw, r.WithContext(ctx))
		duration := time.Since(start).Seconds()
		metrics.RequestDuration.WithLabelValues("request").Observe(duration)
		logger.Info("request completed", "status", sw.status, "duration_ms", time.Since(start).Milliseconds(), "user_agent", r.UserAgent())
	})
}

func (s *Server) checkPolicy(w http.ResponseWriter, r *http.Request, identity *auth.Identity, model string, stream bool) bool {
	if s.policy == nil {
		return true
	}
	dec := s.policy.Evaluate(r.Context(), policy.Input{
		Email:  identity.Email,
		Model:  model,
		Stream: stream,
		Path:   r.URL.Path,
	})
	if !dec.Allowed {
		s.logCtx(r.Context(), "DENY_POLICY", model, "", dec.Reason, identity.Email)
		http.Error(w, dec.Reason, http.StatusForbidden)
		return false
	}
	return true
}

// @Summary Health check
// @Description Returns service status, screening mode, and component health
// @Tags Health
// @Produce json
// @Success 200 {object} map[string]string
// @Router /health [get]
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"status":  "ok",
		"mode":    s.cfg.ResponseMode,
		"version": s.cfg.Version,
	}
	if s.policy != nil {
		resp["opa"] = map[string]string{
			"status": "enabled",
			"info":   s.policy.Status(),
		}
	}
	if s.webhook != nil {
		resp["webhook"] = "enabled"
	}
	if s.rateLimiter != nil {
		resp["rate_limit"] = "enabled"
	}
	if vc, ok := s.vertex.(*vertex.Client); ok && vc != nil {
		resp["circuit_breaker"] = vc.BreakerInfo()
	}
	writeJSON(w, resp)
}

// @Summary Readiness check
// @Description Checks if the service can reach Vertex AI
// @Tags Health
// @Produce json
// @Success 200 {object} map[string]string
// @Failure 503 {string} string "not ready"
// @Router /ready [get]
func (s *Server) readinessHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.vertex == nil {
		writeJSON(w, map[string]string{"status": "not ready", "reason": "vertex client not initialised"})
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]string{"status": "ready", "mode": s.cfg.ResponseMode, "version": s.cfg.Version})
}

// @Summary EICAR-style test strings
// @Description Returns safe test strings that trigger each inspector for verification
// @Tags Health
// @Produce json
// @Success 200 {object} map[string]string
// @Router /test-strings [get]
func (s *Server) testStringsHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{
		"ssn":         inspector.TestSSN,
		"credit_card": inspector.TestCreditCard,
		"private_key": inspector.TestPrivateKey,
		"aws_key":     inspector.TestAWSKey,
		"api_key":     inspector.TestAPIKey,
		"credentials": inspector.TestCredentials,
	})
}
