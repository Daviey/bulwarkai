package vertex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Daviey/bulwarkai/internal/config"
)

type DemoClient struct {
	cfg *config.Config
}

func NewDemoClient(cfg *config.Config) *DemoClient {
	return &DemoClient{cfg: cfg}
}

func (d *DemoClient) SetADCTokenFunc(func() string) {}

func (d *DemoClient) CallJSON(ctx context.Context, body map[string]interface{}, accessToken string, streaming bool) ([]byte, error) {
	return json.Marshal(d.buildGeminiResponse())
}

func (d *DemoClient) CallStream(ctx context.Context, body map[string]interface{}, accessToken string) (io.ReadCloser, error) {
	return d.buildStreamResponse()
}

func (d *DemoClient) CallStreamRaw(ctx context.Context, body map[string]interface{}, accessToken, model, action string) (io.ReadCloser, error) {
	return d.buildStreamResponse()
}

func (d *DemoClient) CallJSONForModel(ctx context.Context, body map[string]interface{}, accessToken, model string, streaming bool) ([]byte, error) {
	return json.Marshal(d.buildGeminiResponse())
}

func (d *DemoClient) buildGeminiResponse() map[string]interface{} {
	ts := time.Now().Format(time.RFC3339)
	return map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"role":  "model",
					"parts": []interface{}{map[string]interface{}{"text": d.demoText()}},
				},
				"finishReason":  "STOP",
				"finishMessage": "Token recitation stopped.",
				"index":         0,
			},
		},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     8,
			"candidatesTokenCount": 42,
			"totalTokenCount":      50,
		},
		"modelVersion": d.cfg.FallbackGeminiModel + "-" + ts[:7],
	}
}

func (d *DemoClient) demoText() string {
	return fmt.Sprintf("This is a demo response from Bulwarkai. No real LLM was called. "+
		"The screening pipeline (prompt inspection, response inspection) ran normally. "+
		"Timestamp: %s", time.Now().Format(time.RFC3339))
}

func (d *DemoClient) buildStreamResponse() (io.ReadCloser, error) {
	text := d.demoText()
	ts := fmt.Sprintf("%d", time.Now().UnixMilli())
	chunks := []map[string]interface{}{
		{
			"candidates": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"role":  "model",
						"parts": []interface{}{map[string]interface{}{"text": strings.Split(text, ".")[0] + "."}},
					},
				},
			},
		},
		{
			"candidates": []interface{}{
				map[string]interface{}{
					"content": map[string]interface{}{
						"role":  "model",
						"parts": []interface{}{map[string]interface{}{"text": " No real LLM was called."}},
					},
					"finishReason":  "STOP",
					"finishMessage": "Token recitation stopped.",
					"index":         0,
				},
			},
			"usageMetadata": map[string]interface{}{
				"promptTokenCount":     8,
				"candidatesTokenCount": 20,
				"totalTokenCount":      28,
			},
			"modelVersion": d.cfg.FallbackGeminiModel + "-" + ts[:6],
		},
	}

	var buf bytes.Buffer
	for _, chunk := range chunks {
		data, _ := json.Marshal(chunk)
		buf.Write(data)
		buf.WriteString("\n")
	}

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		time.Sleep(50 * time.Millisecond)
		if _, err := pw.Write(buf.Bytes()); err != nil {
			_ = err
		}
	}()
	return pr, nil
}

type VertexCaller interface {
	CallJSON(ctx context.Context, body map[string]interface{}, accessToken string, streaming bool) ([]byte, error)
	CallStream(ctx context.Context, body map[string]interface{}, accessToken string) (io.ReadCloser, error)
	CallStreamRaw(ctx context.Context, body map[string]interface{}, accessToken, model, action string) (io.ReadCloser, error)
	CallJSONForModel(ctx context.Context, body map[string]interface{}, accessToken, model string, streaming bool) ([]byte, error)
}

var _ VertexCaller = (*Client)(nil)
var _ VertexCaller = (*DemoClient)(nil)
