package inspector

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Daviey/bulwarkai/internal/metrics"
)

type BlockResult struct {
	Blocked bool
	Reason  string
	Err     error
}

type Inspector interface {
	Name() string
	TestMethod() string
	InspectPrompt(ctx context.Context, text string, token string) *BlockResult
	InspectResponse(ctx context.Context, text string, token string) *BlockResult
}

type Chain []Inspector

func NewChain(inspectors ...Inspector) Chain {
	return Chain(inspectors)
}

func (c Chain) Names() []string {
	names := make([]string, len(c))
	for i, insp := range c {
		names[i] = insp.Name()
	}
	return names
}

func (c Chain) TestMethods() map[string]string {
	methods := make(map[string]string, len(c))
	for _, insp := range c {
		methods[insp.Name()] = insp.TestMethod()
	}
	return methods
}

type screenResult struct {
	inspector string
	result    *BlockResult
	duration  time.Duration
}

func (c Chain) ScreenPrompt(ctx context.Context, text, token string) *BlockResult {
	return c.runConcurrently(ctx, "prompt", text, token, func(insp Inspector, ctx context.Context, text, token string) *BlockResult {
		return insp.InspectPrompt(ctx, text, token)
	})
}

func (c Chain) ScreenResponse(ctx context.Context, text, token string) *BlockResult {
	return c.runConcurrently(ctx, "response", text, token, func(insp Inspector, ctx context.Context, text, token string) *BlockResult {
		return insp.InspectResponse(ctx, text, token)
	})
}

type inspectFunc func(Inspector, context.Context, string, string) *BlockResult

func (c Chain) runConcurrently(ctx context.Context, direction, text, token string, fn inspectFunc) *BlockResult {
	var wg sync.WaitGroup
	ch := make(chan screenResult, len(c))

	for _, insp := range c {
		wg.Add(1)
		go func(i Inspector) {
			defer wg.Done()
			start := time.Now()
			result := fn(i, ctx, text, token)
			dur := time.Since(start)
			metrics.InspectorDuration.WithLabelValues(i.Name(), direction).Observe(dur.Seconds())
			if result != nil && result.Blocked {
				metrics.InspectorResults.WithLabelValues(i.Name(), direction, "block").Inc()
			} else if result != nil && result.Err != nil {
				metrics.InspectorResults.WithLabelValues(i.Name(), direction, "error").Inc()
			} else {
				metrics.InspectorResults.WithLabelValues(i.Name(), direction, "pass").Inc()
			}
			ch <- screenResult{
				inspector: i.Name(),
				result:    result,
				duration:  time.Since(start),
			}
		}(insp)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	var blockResult *BlockResult
	for sr := range ch {
		if sr.result != nil && sr.result.Blocked {
			slog.Warn("inspector blocked",
				"direction", direction,
				"inspector", sr.inspector,
				"reason", sr.result.Reason,
				"duration_ms", sr.duration.Milliseconds(),
			)
			if blockResult == nil {
				blockResult = sr.result
			}
		} else if sr.result != nil && sr.result.Err != nil {
			slog.Error("inspector error (fail-open)",
				"direction", direction,
				"inspector", sr.inspector,
				"error", sr.result.Err,
				"duration_ms", sr.duration.Milliseconds(),
			)
		} else {
			slog.Debug("inspector passed",
				"direction", direction,
				"inspector", sr.inspector,
				"duration_ms", sr.duration.Milliseconds(),
			)
		}
	}

	return blockResult
}
