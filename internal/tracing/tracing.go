package tracing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"
)

type contextKey string

const (
	spanKey    contextKey = "span"
	traceIDKey contextKey = "trace_id"
)

type Span struct {
	Name       string
	TraceID    string
	SpanID     string
	StartTime  time.Time
	Attributes map[string]string
	mu         sync.Mutex
	children   []*Span
}

func StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	span := &Span{
		Name:      name,
		TraceID:   TraceIDFromContext(ctx),
		SpanID:    generateSpanID(),
		StartTime: time.Now(),
	}

	parent := SpanFromContext(ctx)
	if parent != nil {
		parent.mu.Lock()
		parent.children = append(parent.children, span)
		parent.mu.Unlock()
	}

	return context.WithValue(ctx, spanKey, span), span
}

func (s *Span) SetAttribute(key, value string) {
	if s.Attributes == nil {
		s.Attributes = make(map[string]string)
	}
	s.Attributes[key] = value
}

func (s *Span) End() {
	duration := time.Since(s.StartTime)
	attrs := make([]any, 0, len(s.Attributes)*2+4)
	attrs = append(attrs, "span", s.Name, "span_id", s.SpanID, "duration_ms", duration.Milliseconds())
	for k, v := range s.Attributes {
		attrs = append(attrs, k, v)
	}
	if s.TraceID != "" {
		attrs = append(attrs, "trace_id", s.TraceID)
	}
	slog.Debug("span completed", attrs...)
}

func SpanFromContext(ctx context.Context) *Span {
	span, _ := ctx.Value(spanKey).(*Span)
	return span
}

func TraceIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(traceIDKey).(string)
	return id
}

func ContextWithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

func generateSpanID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
