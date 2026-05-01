package circuitbreaker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

type Breaker struct {
	mu           sync.Mutex
	state        State
	failures     int
	successes    int
	lastFailure  time.Time
	maxFailures  int
	resetTimeout time.Duration
	halfOpenMax  int
	name         string
}

func NewBreaker(name string, maxFailures int, resetTimeout time.Duration) *Breaker {
	return &Breaker{
		name:         name,
		state:        Closed,
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		halfOpenMax:  1,
	}
}

func (b *Breaker) Allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case Closed:
		return nil
	case Open:
		if time.Since(b.lastFailure) > b.resetTimeout {
			b.state = HalfOpen
			b.successes = 0
			slog.Info("circuit breaker half-open", "breaker", b.name)
			return nil
		}
		return fmt.Errorf("circuit breaker %s open", b.name)
	case HalfOpen:
		if b.successes < b.halfOpenMax {
			return nil
		}
		return fmt.Errorf("circuit breaker %s half-open (probe in flight)", b.name)
	default:
		return nil
	}
}

func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == HalfOpen {
		b.successes++
		if b.successes >= b.halfOpenMax {
			b.state = Closed
			b.failures = 0
			slog.Info("circuit breaker closed", "breaker", b.name)
		}
	}
}

func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures++
	b.lastFailure = time.Now()

	if b.state == HalfOpen {
		b.state = Open
		slog.Warn("circuit breaker reopened from half-open", "breaker", b.name)
		return
	}

	if b.failures >= b.maxFailures {
		b.state = Open
		slog.Warn("circuit breaker opened", "breaker", b.name, "failures", b.failures)
	}
}

func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *Breaker) Failures() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failures
}

func (b *Breaker) Execute(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := b.Allow(); err != nil {
		return err
	}
	err := fn(ctx)
	if err != nil {
		b.RecordFailure()
		return err
	}
	b.RecordSuccess()
	return nil
}
