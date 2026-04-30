package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
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
					n.send(evt)
				default:
					return
				}
			}
		case evt := <-n.queue:
			n.send(evt)
		}
	}
}

func (n *Notifier) send(evt BlockEvent) {
	payload, err := json.Marshal(evt)
	if err != nil {
		slog.Error("webhook marshal error", "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", n.url, bytes.NewReader(payload))
	if err != nil {
		slog.Error("webhook request create error", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "bulwarkai-webhook/1.0")
	if n.secret != "" {
		req.Header.Set("X-Webhook-Secret", n.secret)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		slog.Error("webhook send error", "url", n.url, "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		slog.Warn("webhook non-success response", "url", n.url, "status", resp.StatusCode)
	} else {
		slog.Debug("webhook sent", "action", evt.Action, "email", evt.Email, "status", resp.StatusCode)
	}
}
