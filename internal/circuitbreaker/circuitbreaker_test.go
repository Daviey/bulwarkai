package circuitbreaker

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBreaker_StartsClosed(t *testing.T) {
	b := NewBreaker("test", 3, time.Second)
	if b.State() != Closed {
		t.Errorf("expected closed, got %s", b.State())
	}
}

func TestBreaker_AllowsWhenClosed(t *testing.T) {
	b := NewBreaker("test", 3, time.Second)
	if err := b.Allow(); err != nil {
		t.Errorf("closed breaker should allow, got %v", err)
	}
}

func TestBreaker_OpensAfterMaxFailures(t *testing.T) {
	b := NewBreaker("test", 3, time.Second)
	for i := 0; i < 3; i++ {
		b.RecordFailure()
	}
	if b.State() != Open {
		t.Errorf("expected open after 3 failures, got %s", b.State())
	}
	if err := b.Allow(); err == nil {
		t.Error("open breaker should reject")
	}
}

func TestBreaker_TransitionsToHalfOpen(t *testing.T) {
	b := NewBreaker("test", 1, 50*time.Millisecond)
	b.RecordFailure()
	if b.State() != Open {
		t.Fatalf("expected open, got %s", b.State())
	}
	time.Sleep(60 * time.Millisecond)
	if err := b.Allow(); err != nil {
		t.Errorf("should transition to half-open after timeout, got %v", err)
	}
	if b.State() != HalfOpen {
		t.Errorf("expected half-open, got %s", b.State())
	}
}

func TestBreaker_ClosesFromHalfOpen(t *testing.T) {
	b := NewBreaker("test", 1, 50*time.Millisecond)
	b.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	b.Allow()
	b.RecordSuccess()
	if b.State() != Closed {
		t.Errorf("expected closed after success in half-open, got %s", b.State())
	}
}

func TestBreaker_ReopensFromHalfOpen(t *testing.T) {
	b := NewBreaker("test", 1, 50*time.Millisecond)
	b.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	b.Allow()
	b.RecordFailure()
	if b.State() != Open {
		t.Errorf("expected open after failure in half-open, got %s", b.State())
	}
}

func TestBreaker_Execute_Success(t *testing.T) {
	b := NewBreaker("test", 3, time.Second)
	called := false
	err := b.Execute(context.Background(), func(ctx context.Context) error {
		called = true
		return nil
	})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if !called {
		t.Error("expected function to be called")
	}
}

func TestBreaker_Execute_Failure(t *testing.T) {
	b := NewBreaker("test", 1, time.Second)
	err := b.Execute(context.Background(), func(ctx context.Context) error {
		return errors.New("fail")
	})
	if err == nil {
		t.Error("expected error")
	}
	if b.State() != Open {
		t.Errorf("expected open after failure, got %s", b.State())
	}
}

func TestBreaker_Execute_RejectedWhenOpen(t *testing.T) {
	b := NewBreaker("test", 1, time.Second)
	b.RecordFailure()
	called := false
	err := b.Execute(context.Background(), func(ctx context.Context) error {
		called = true
		return nil
	})
	if err == nil {
		t.Error("expected rejection error")
	}
	if called {
		t.Error("function should not be called when open")
	}
}

func TestBreaker_StateString(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{Closed, "closed"},
		{Open, "open"},
		{HalfOpen, "half-open"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.expected)
		}
	}
}
