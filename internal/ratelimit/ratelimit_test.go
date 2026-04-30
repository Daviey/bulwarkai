package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiter_Allows(t *testing.T) {
	l := NewLimiter(3, time.Second)
	for i := 0; i < 3; i++ {
		if !l.Allow("key1") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if l.Allow("key1") {
		t.Fatal("4th request should be rejected")
	}
}

func TestLimiter_DifferentKeys(t *testing.T) {
	l := NewLimiter(1, time.Second)
	if !l.Allow("a") {
		t.Fatal("first request for a should be allowed")
	}
	if !l.Allow("b") {
		t.Fatal("first request for b should be allowed")
	}
}

func TestLimiter_WindowReset(t *testing.T) {
	l := NewLimiter(1, 100*time.Millisecond)
	if !l.Allow("key") {
		t.Fatal("first request should be allowed")
	}
	if l.Allow("key") {
		t.Fatal("second request should be rejected")
	}
	time.Sleep(150 * time.Millisecond)
	if !l.Allow("key") {
		t.Fatal("request after window reset should be allowed")
	}
}

func TestLimiter_Cleanup(t *testing.T) {
	l := NewLimiter(1, 100*time.Millisecond)
	l.Allow("a")
	l.Allow("b")
	time.Sleep(150 * time.Millisecond)
	l.Cleanup()
	l.mu.Lock()
	count := len(l.windows)
	l.mu.Unlock()
	if count != 0 {
		t.Fatalf("expected 0 windows after cleanup, got %d", count)
	}
}

func TestMiddleware_NilLimiter(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	h := Middleware(nil, func(r *http.Request) string { return "k" }, next)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Fatal("nil limiter should pass through")
	}
}

func TestMiddleware_Rejects(t *testing.T) {
	l := NewLimiter(1, time.Second)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	h := Middleware(l, func(r *http.Request) string { return "k" }, next)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request should be ok, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request should be 429, got %d", rec.Code)
	}
}

func TestMiddleware_EmptyKey(t *testing.T) {
	l := NewLimiter(1, time.Second)
	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
	})
	h := Middleware(l, func(r *http.Request) string { return "" }, next)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}
	if called != 5 {
		t.Fatalf("empty key should skip rate limiting, got %d calls", called)
	}
}
