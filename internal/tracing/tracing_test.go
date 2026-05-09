package tracing

import (
	"context"
	"testing"
)

func TestStartSpan_CreatesSpan(t *testing.T) {
	ctx, span := StartSpan(context.Background(), "test-span")
	if span == nil {
		t.Fatal("expected span")
	}
	if span.Name != "test-span" {
		t.Errorf("got %q", span.Name)
	}
	if span.SpanID == "" {
		t.Error("expected span ID")
	}
	if SpanFromContext(ctx) != span {
		t.Error("span not in context")
	}
}

func TestSpan_SetAttribute(t *testing.T) {
	_, span := StartSpan(context.Background(), "test")
	span.SetAttribute("model", "gemini-2.5-flash")
	span.SetAttribute("email", "test@example.com")
	if span.Attributes["model"] != "gemini-2.5-flash" {
		t.Errorf("got %q", span.Attributes["model"])
	}
	if span.Attributes["email"] != "test@example.com" {
		t.Errorf("got %q", span.Attributes["email"])
	}
}

func TestSpan_ChildSpan(t *testing.T) {
	ctx, parent := StartSpan(context.Background(), "parent")
	_, child := StartSpan(ctx, "child")
	if len(parent.children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(parent.children))
	}
	if parent.children[0] != child {
		t.Error("child not attached to parent")
	}
}

func TestTraceID_ContextPropagation(t *testing.T) {
	ctx := ContextWithTraceID(context.Background(), "abc123")
	if TraceIDFromContext(ctx) != "abc123" {
		t.Errorf("got %q", TraceIDFromContext(ctx))
	}
}

func TestTraceID_InheritedByChild(t *testing.T) {
	ctx := ContextWithTraceID(context.Background(), "trace-456")
	_, span := StartSpan(ctx, "child")
	if span.TraceID != "trace-456" {
		t.Errorf("got %q", span.TraceID)
	}
}

func TestSpanFromContext_Nil(t *testing.T) {
	if SpanFromContext(context.Background()) != nil {
		t.Error("expected nil")
	}
}

func TestSpan_End(t *testing.T) {
	_, span := StartSpan(context.Background(), "test")
	span.SetAttribute("key", "value")
	span.End()
}
