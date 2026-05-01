package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"
)

type BlockEvent struct {
	Timestamp string `json:"timestamp"`
	Action    string `json:"action"`
	Model     string `json:"model"`
	Email     string `json:"email"`
	Reason    string `json:"reason"`
	RequestID string `json:"request_id"`
	Prompt    string `json:"prompt,omitempty"`
}

const (
	maxRetries    = 3
	baseBackoff   = 500 * time.Millisecond
	maxBackoff    = 10 * time.Second
	backoffFactor = 2.0
)

type Notifier struct {
	url    string
	secret string
	client *http.Client
	queue  chan BlockEvent
	done   chan struct{}
	wg     sync.WaitGroup
}

func NewNotifier(url, secret string, bufferSize int) *Notifier {
	if bufferSize <= 0 {
		bufferSize = 256
	}
	return &Notifier{
		url:    url,
		secret: secret,
		client: &http.Client{Timeout: 10 * time.Second},
		queue:  make(chan BlockEvent, bufferSize),
		done:   make(chan struct{}),
	}
}

func (n *Notifier) Start() {
	n.wg.Add(1)
	go n.processQueue()
}

func (n *Notifier) Stop() {
	close(n.done)
	n.wg.Wait()
}

func (n *Notifier) Notify(evt BlockEvent) {
	if n.url == "" {
		return
	}
	evt.Timestamp = time.Now().UTC().Format(time.RFC3339)
	select {
	case n.queue <- evt:
	default:
		slog.Warn("webhook queue full, dropping event", "action", evt.Action, "email", evt.Email)
	}
}

func (n *Notifier) processQueue() {
	defer n.wg.Done()
	for {
		select {
		case <-n.done:
			for {
				select {
				case evt := <-n.queue:
					n.sendWithRetry(evt)
				default:
					return
				}
			}
		case evt := <-n.queue:
			n.sendWithRetry(evt)
		}
	}
}

func (n *Notifier) sendWithRetry(evt BlockEvent) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Min(float64(baseBackoff)*math.Pow(backoffFactor, float64(attempt-1)), float64(maxBackoff)))
			slog.Info("webhook retry", "action", evt.Action, "attempt", attempt, "backoff_ms", backoff.Milliseconds())
			select {
			case <-n.done:
				return
			case <-time.After(backoff):
			}
		}
		if n.send(evt) {
			return
		}
	}
	slog.Error("webhook exhausted retries", "action", evt.Action, "email", evt.Email, "url", n.url)
}

func (n *Notifier) send(evt BlockEvent) bool {
	payload, err := json.Marshal(evt)
	if err != nil {
		slog.Error("webhook marshal error", "error", err)
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", n.url, bytes.NewReader(payload))
	if err != nil {
		slog.Error("webhook request create error", "error", err)
		return true
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "bulwarkai-webhook/1.0")
	if n.secret != "" {
		req.Header.Set("X-Webhook-Secret", n.secret)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		slog.Warn("webhook send error", "url", n.url, "error", err)
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		slog.Warn("webhook server error", "url", n.url, "status", resp.StatusCode)
		return false
	}

	if resp.StatusCode >= 300 {
		slog.Warn("webhook client error, not retrying", "url", n.url, "status", resp.StatusCode)
		return true
	}

	slog.Debug("webhook sent", "action", evt.Action, "email", evt.Email, "status", resp.StatusCode)
	return true
}
