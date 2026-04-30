package ratelimit

import (
	"net/http"
	"sync"
	"time"
)

type entry struct {
	count       int
	windowStart time.Time
}

type Limiter struct {
	mu      sync.Mutex
	windows map[string]*entry
	limit   int
	window  time.Duration
}

func NewLimiter(limit int, window time.Duration) *Limiter {
	return &Limiter{
		windows: make(map[string]*entry),
		limit:   limit,
		window:  window,
	}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	e, ok := l.windows[key]
	if !ok || now.Sub(e.windowStart) > l.window {
		l.windows[key] = &entry{count: 1, windowStart: now}
		return true
	}
	e.count++
	return e.count <= l.limit
}

func (l *Limiter) Cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for k, e := range l.windows {
		if now.Sub(e.windowStart) > l.window {
			delete(l.windows, k)
		}
	}
}

func Middleware(limiter *Limiter, keyFunc func(r *http.Request) string, next http.Handler) http.Handler {
	if limiter == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := keyFunc(r)
		if key != "" && !limiter.Allow(key) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
