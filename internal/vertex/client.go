package vertex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Daviey/bulwarkai/internal/circuitbreaker"
	"github.com/Daviey/bulwarkai/internal/config"

	"golang.org/x/oauth2/google"
)

type Client struct {
	cfg          *config.Config
	httpClient   *http.Client
	adcTokenFunc func() string
	breaker      *circuitbreaker.Breaker
}

func NewClient(cfg *config.Config, httpClient *http.Client) *Client {
	cbFailures := cfg.CBMaxFailures
	if cbFailures <= 0 {
		cbFailures = 5
	}
	cbTimeout := parseDurationSafe(cfg.CBResetTimeout, 30*time.Second)
	c := &Client{
		cfg:        cfg,
		httpClient: httpClient,
		breaker:    circuitbreaker.NewBreaker("vertex-ai", cbFailures, cbTimeout),
	}
	if cfg.LocalMode {
		c.initADC()
	}
	return c
}

func parseDurationSafe(s string, def time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}

func (c *Client) initADC() {
	creds, err := google.FindDefaultCredentials(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		slog.Warn("LOCAL_MODE: ADC not available, Vertex AI calls will fail", "error", err)
		return
	}
	ts := creds.TokenSource
	c.adcTokenFunc = func() string {
		tok, err := ts.Token()
		if err != nil {
			slog.Error("ADC token refresh failed", "error", err)
			return ""
		}
		return tok.AccessToken
	}
	slog.Info("LOCAL_MODE: ADC credentials loaded")
}

func (c *Client) SetADCTokenFunc(f func() string) {
	c.adcTokenFunc = f
}

func (c *Client) BreakerState() string {
	return c.breaker.State().String()
}

func (c *Client) resolveToken(accessToken string) string {
	if accessToken != "" {
		return accessToken
	}
	if c.adcTokenFunc != nil {
		return c.adcTokenFunc()
	}
	return ""
}

func (c *Client) buildVertexURL(streaming bool) string {
	action := "generateContent"
	if streaming {
		action = "streamGenerateContent"
	}
	return fmt.Sprintf("%s/publishers/google/models/%s:%s", c.cfg.VertexBase, c.cfg.FallbackGeminiModel, action)
}

func (c *Client) CallStreamRaw(ctx context.Context, body map[string]interface{}, accessToken, model, action string) (io.ReadCloser, error) {
	if err := c.breaker.Allow(); err != nil {
		return nil, err
	}
	rc, err := c.callStreamRaw(ctx, body, accessToken, model, action)
	if err != nil {
		c.breaker.RecordFailure()
		return nil, err
	}
	c.breaker.RecordSuccess()
	return rc, nil
}

func (c *Client) callStreamRaw(ctx context.Context, body map[string]interface{}, accessToken, model, action string) (io.ReadCloser, error) {
	url := fmt.Sprintf("%s/publishers/google/models/%s:%s", c.cfg.VertexBase, model, action)
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.resolveToken(accessToken))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-user-project", c.cfg.Project)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		data, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("vertex returned %d (body read failed: %s)", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("vertex returned %d: %s", resp.StatusCode, string(data[:min(len(data), 500)]))
	}
	return resp.Body, nil
}

func (c *Client) CallJSONForModel(ctx context.Context, body map[string]interface{}, accessToken, model string, streaming bool) ([]byte, error) {
	if err := c.breaker.Allow(); err != nil {
		return nil, err
	}
	data, err := c.callJSONForModel(ctx, body, accessToken, model, streaming)
	if err != nil {
		c.breaker.RecordFailure()
		return nil, err
	}
	c.breaker.RecordSuccess()
	return data, nil
}

func (c *Client) callJSONForModel(ctx context.Context, body map[string]interface{}, accessToken, model string, streaming bool) ([]byte, error) {
	url := fmt.Sprintf("%s/publishers/google/models/%s:generateContent", c.cfg.VertexBase, model)
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.resolveToken(accessToken))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-user-project", c.cfg.Project)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vertex returned %d: %s", resp.StatusCode, string(data[:min(len(data), 500)]))
	}
	return data, nil
}

func (c *Client) CallJSON(ctx context.Context, body map[string]interface{}, accessToken string, streaming bool) ([]byte, error) {
	if err := c.breaker.Allow(); err != nil {
		return nil, err
	}
	data, err := c.callJSON(ctx, body, accessToken, streaming)
	if err != nil {
		c.breaker.RecordFailure()
		return nil, err
	}
	c.breaker.RecordSuccess()
	return data, nil
}

func (c *Client) callJSON(ctx context.Context, body map[string]interface{}, accessToken string, streaming bool) ([]byte, error) {
	url := c.buildVertexURL(streaming)
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.resolveToken(accessToken))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-user-project", c.cfg.Project)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vertex returned %d: %s", resp.StatusCode, string(data[:min(len(data), 500)]))
	}
	return data, nil
}

func (c *Client) CallStream(ctx context.Context, body map[string]interface{}, accessToken string) (io.ReadCloser, error) {
	if err := c.breaker.Allow(); err != nil {
		return nil, err
	}
	rc, err := c.callStream(ctx, body, accessToken)
	if err != nil {
		c.breaker.RecordFailure()
		return nil, err
	}
	c.breaker.RecordSuccess()
	return rc, nil
}

func (c *Client) callStream(ctx context.Context, body map[string]interface{}, accessToken string) (io.ReadCloser, error) {
	url := c.buildVertexURL(true)
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.resolveToken(accessToken))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-user-project", c.cfg.Project)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		data, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("vertex returned %d (body read failed: %s)", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("vertex returned %d: %s", resp.StatusCode, string(data[:min(len(data), 500)]))
	}
	return resp.Body, nil
}
