package webhook

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestNotifier_SendsEvent(t *testing.T) {
	var received atomic.Int32
	var lastBody atomic.Pointer[string]
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		lastBody.Store(&s)
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	n := NewNotifier(ts.URL+"/webhook", "secret123", 16)
	n.Start()
	defer n.Stop()

	n.Notify(BlockEvent{
		Action:    "BLOCK_PROMPT",
		Model:     "gemini-2.5-flash",
		Email:     "test@example.com",
		Reason:    "SSN detected",
		RequestID: "req-123",
	})

	deadline := time.After(3 * time.Second)
	for received.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for webhook")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	body := *lastBody.Load()
	var evt BlockEvent
	if err := json.Unmarshal([]byte(body), &evt); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if evt.Action != "BLOCK_PROMPT" {
		t.Errorf("expected BLOCK_PROMPT, got %q", evt.Action)
	}
	if evt.Timestamp == "" {
		t.Error("expected timestamp to be set")
	}
}

func TestNotifier_SecretHeader(t *testing.T) {
	var gotSecret atomic.Pointer[string]
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := r.Header.Get("X-Webhook-Secret")
		gotSecret.Store(&s)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	n := NewNotifier(ts.URL, "my-secret", 16)
	n.Start()
	defer n.Stop()

	n.Notify(BlockEvent{Action: "BLOCK_PROMPT"})

	deadline := time.After(3 * time.Second)
	for gotSecret.Load() == nil {
		select {
		case <-deadline:
			t.Fatal("timed out")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
	if *gotSecret.Load() != "my-secret" {
		t.Errorf("expected secret header, got %q", *gotSecret.Load())
	}
}

func TestNotifier_EmptyURL(t *testing.T) {
	n := NewNotifier("", "", 16)
	n.Notify(BlockEvent{Action: "BLOCK_PROMPT"})
}

func TestNotifier_QueueFull(t *testing.T) {
	block := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	n := NewNotifier(ts.URL, "", 2)
	n.Start()
	defer n.Stop()

	for i := 0; i < 5; i++ {
		n.Notify(BlockEvent{Action: "BLOCK_PROMPT"})
	}
	close(block)
}

func TestNotifier_StopsDraining(t *testing.T) {
	var received atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	n := NewNotifier(ts.URL, "", 16)
	n.Start()

	for i := 0; i < 3; i++ {
		n.Notify(BlockEvent{Action: "BLOCK_PROMPT"})
	}
	n.Stop()

	if received.Load() != 3 {
		t.Errorf("expected 3 events after stop, got %d", received.Load())
	}
}
