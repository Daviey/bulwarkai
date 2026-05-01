package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Daviey/bulwarkai/internal/metrics"
	"github.com/Daviey/bulwarkai/internal/webhook"
)

func reqIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func (s *Server) auditResponse(ctx context.Context, text, token, model, email string) {
	if text == "" {
		return
	}
	if br := s.chain.ScreenResponse(ctx, text, token); br != nil {
		s.logAction("BLOCK_RESPONSE_AUDIT", model, "", br.Reason, email, reqIDFromContext(ctx))
	}
}

func (s *Server) logCtx(ctx context.Context, action, model, prompt, reason, email string) {
	s.logAction(action, model, prompt, reason, email, reqIDFromContext(ctx))
}

func (s *Server) logAction(action, model, prompt, reason, email, requestID string) {
	metrics.RequestsTotal.WithLabelValues(action, model).Inc()
	prompt = s.cfg.RedactPrompt(prompt)
	level := slog.LevelInfo
	if strings.HasPrefix(action, "BLOCK") || strings.HasPrefix(action, "DENY") {
		level = slog.LevelWarn
		if s.webhook != nil {
			s.webhook.Notify(webhook.BlockEvent{
				Action:    action,
				Model:     model,
				Email:     email,
				Reason:    reason,
				Prompt:    prompt,
				RequestID: requestID,
			})
		}
	}
	slog.LogAttrs(context.Background(), level, action,
		slog.String("action", action),
		slog.String("model", model),
		slog.String("email", email),
		slog.String("reason", reason),
		slog.String("prompt", prompt),
		slog.String("request_id", requestID),
	)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("json encode error", "error", err)
	}
}

func (s *Server) parseBody(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	maxBodySize := int64(s.cfg.MaxBodySize)
	if maxBodySize <= 0 {
		maxBodySize = 10 * 1024 * 1024
	}
	lr := io.LimitReader(r.Body, maxBodySize+1)
	if err := json.NewDecoder(lr).Decode(target); err != nil {
		if err == io.ErrUnexpectedEOF || lr.(*io.LimitedReader).N <= 0 {
			http.Error(w, "request body exceeds size limit", http.StatusRequestEntityTooLarge)
			return false
		}
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}
