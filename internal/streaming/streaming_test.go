package streaming

import (
	"strings"
	"testing"
)

func TestParseGeminiChunk_EmptyLines(t *testing.T) {
	tests := []string{"", "   ", "[", "]", ","}
	for _, line := range tests {
		text, finish, json_ := ParseGeminiChunk(line)
		if text != "" || finish != "" || json_ != nil {
			t.Fatalf("line %q should produce empty results", line)
		}
	}
}

func TestParseGeminiChunk_DataPrefix(t *testing.T) {
	line := `data: {"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"STOP"}]}`
	text, finish, _ := ParseGeminiChunk(line)
	if text != "hi" {
		t.Fatalf("expected 'hi', got %q", text)
	}
	if finish != "STOP" {
		t.Fatalf("expected STOP, got %q", finish)
	}
}

func TestParseGeminiChunk_BracketPrefix(t *testing.T) {
	line := `[{"candidates":[{"content":{"parts":[{"text":"world"}]}}]}`
	text, _, _ := ParseGeminiChunk(line)
	if text != "world" {
		t.Fatalf("expected 'world', got %q", text)
	}
}

func TestParseGeminiChunk_TrailingComma(t *testing.T) {
	line := `{"candidates":[{"content":{"parts":[{"text":"x"}]}}]},`
	text, _, _ := ParseGeminiChunk(line)
	if text != "x" {
		t.Fatalf("expected 'x', got %q", text)
	}
}

func TestParseGeminiChunk_TrailingBracket(t *testing.T) {
	line := `{"candidates":[{"content":{"parts":[{"text":"y"}]}}]}]`
	text, _, _ := ParseGeminiChunk(line)
	if text != "y" {
		t.Fatalf("expected 'y', got %q", text)
	}
}

func TestParseGeminiChunk_InvalidJSON(t *testing.T) {
	text, finish, json_ := ParseGeminiChunk("not json at all")
	if text != "" || finish != "" || json_ != nil {
		t.Fatal("invalid JSON should return empty")
	}
}

func TestParseGeminiChunk_NoCandidates(t *testing.T) {
	line := `{"other": "data"}`
	text, finish, _ := ParseGeminiChunk(line)
	if text != "" || finish != "" {
		t.Fatal("no candidates should return empty text/finish")
	}
}

func TestParseGeminiChunk_FinishOnly(t *testing.T) {
	line := `{"candidates":[{"finishReason":"MAX_TOKENS"}]}`
	text, finish, _ := ParseGeminiChunk(line)
	if text != "" {
		t.Fatal("should have no text")
	}
	if finish != "MAX_TOKENS" {
		t.Fatalf("expected MAX_TOKENS, got %q", finish)
	}
}

func TestToJSON(t *testing.T) {
	got := toJSON(map[string]string{"key": "value"})
	if got != `{"key":"value"}` {
		t.Fatalf("unexpected JSON: %s", got)
	}
}

func TestWriteAnthropicSSE(t *testing.T) {
	msg := map[string]interface{}{
		"id":          "msg_123",
		"model":       "gemini-2.5-flash",
		"stop_reason": "end_turn",
		"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "Hello"},
		},
	}
	var buf strings.Builder
	WriteAnthropicSSE(&buf, msg)
	output := buf.String()
	if !strings.Contains(output, "event: message_start") {
		t.Fatal("should contain message_start event")
	}
	if !strings.Contains(output, "event: content_block_start") {
		t.Fatal("should contain content_block_start event")
	}
	if !strings.Contains(output, "event: content_block_delta") {
		t.Fatal("should contain content_block_delta event")
	}
	if !strings.Contains(output, `"text":"Hello"`) {
		t.Fatal("should contain text content")
	}
	if !strings.Contains(output, "event: content_block_stop") {
		t.Fatal("should contain content_block_stop event")
	}
	if !strings.Contains(output, "event: message_delta") {
		t.Fatal("should contain message_delta event")
	}
	if !strings.Contains(output, "event: message_stop") {
		t.Fatal("should contain message_stop event")
	}
}

func TestWriteAnthropicSSE_MultipleBlocks(t *testing.T) {
	msg := map[string]interface{}{
		"id":          "msg_456",
		"model":       "gemini-2.5-flash",
		"stop_reason": "end_turn",
		"usage":       map[string]interface{}{"input_tokens": 0, "output_tokens": 0},
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "A"},
			map[string]interface{}{"type": "text", "text": "B"},
		},
	}
	var buf strings.Builder
	WriteAnthropicSSE(&buf, msg)
	output := buf.String()
	if strings.Count(output, `"index":0`) < 1 {
		t.Fatal("should have index 0 events")
	}
	if strings.Count(output, `"index":1`) < 1 {
		t.Fatal("should have index 1 events")
	}
	if strings.Count(output, `"text":"A"`) != 1 {
		t.Fatal("should contain text A")
	}
	if strings.Count(output, `"text":"B"`) != 1 {
		t.Fatal("should contain text B")
	}
}

func TestWriteOpenAISSE(t *testing.T) {
	msg := map[string]interface{}{
		"id":      "chatcmpl-123",
		"model":   "gemini-2.5-flash",
		"created": float64(1234567890),
		"choices": []interface{}{
			map[string]interface{}{
				"message":       map[string]interface{}{"content": "Hello"},
				"finish_reason": "stop",
			},
		},
	}
	var buf strings.Builder
	WriteOpenAISSE(&buf, msg)
	output := buf.String()
	if !strings.Contains(output, `"content":"Hello"`) {
		t.Fatal("should contain content")
	}
	if !strings.Contains(output, `"finish_reason":"stop"`) {
		t.Fatal("should contain finish_reason in final chunk")
	}
	if !strings.Contains(output, "data: [DONE]") {
		t.Fatal("should end with [DONE]")
	}
}

func TestStreamGeminiAsAnthropic(t *testing.T) {
	input := `{"candidates":[{"content":{"parts":[{"text":"Hello "}]}},{}]}` + "\n" +
		`{"candidates":[{"content":{"parts":[{"text":"world"}]},"finishReason":"STOP"}]}` + "\n"

	var buf strings.Builder
	accumulated := StreamGeminiAsAnthropic(&buf, strings.NewReader(input), "gemini-2.5-flash", nil)
	output := buf.String()

	if !strings.Contains(output, "event: message_start") {
		t.Fatal("should contain message_start")
	}
	if !strings.Contains(output, "event: content_block_delta") {
		t.Fatal("should contain content_block_delta")
	}
	if !strings.Contains(output, "event: message_stop") {
		t.Fatal("should contain message_stop")
	}
	if accumulated != "Hello world" {
		t.Fatalf("expected 'Hello world', got %q", accumulated)
	}
}

func TestStreamGeminiAsAnthropic_WithCapture(t *testing.T) {
	input := `{"candidates":[{"content":{"parts":[{"text":"captured"}]},"finishReason":"STOP"}]}` + "\n"
	var buf strings.Builder
	var captured string
	StreamGeminiAsAnthropic(&buf, strings.NewReader(input), "model", &captured)
	if captured != "captured" {
		t.Fatalf("capture should be 'captured', got %q", captured)
	}
}

func TestStreamGeminiAsAnthropic_MaxTokensFinish(t *testing.T) {
	input := `{"candidates":[{"content":{"parts":[{"text":"x"}]},"finishReason":"MAX_TOKENS"}]}` + "\n"
	var buf strings.Builder
	StreamGeminiAsAnthropic(&buf, strings.NewReader(input), "model", nil)
	if !strings.Contains(buf.String(), `"stop_reason":"max_tokens"`) {
		t.Fatal("MAX_TOKENS should map to max_tokens stop_reason")
	}
}

func TestStreamGeminiAsAnthropic_SafetyFinish(t *testing.T) {
	input := `{"candidates":[{"content":{"parts":[{"text":"x"}]},"finishReason":"SAFETY"}]}` + "\n"
	var buf strings.Builder
	StreamGeminiAsAnthropic(&buf, strings.NewReader(input), "model", nil)
	if !strings.Contains(buf.String(), `"stop_reason":"stop_sequence"`) {
		t.Fatal("SAFETY should map to stop_sequence stop_reason")
	}
}

func TestStreamGeminiAsOpenAI(t *testing.T) {
	input := `{"candidates":[{"content":{"parts":[{"text":"chunk1"}]}}]}` + "\n" +
		`{"candidates":[{"content":{"parts":[{"text":"chunk2"}]},"finishReason":"STOP"}]}` + "\n"

	var buf strings.Builder
	accumulated := StreamGeminiAsOpenAI(&buf, strings.NewReader(input), "gemini-2.5-flash", nil)
	output := buf.String()

	if !strings.Contains(output, `"role":"assistant"`) {
		t.Fatal("should send role delta")
	}
	if !strings.Contains(output, `"content":"chunk1"`) {
		t.Fatal("should contain first chunk")
	}
	if !strings.Contains(output, `"content":"chunk2"`) {
		t.Fatal("should contain second chunk")
	}
	if !strings.Contains(output, "data: [DONE]") {
		t.Fatal("should end with [DONE]")
	}
	if accumulated != "chunk1chunk2" {
		t.Fatalf("expected 'chunk1chunk2', got %q", accumulated)
	}
}

func TestStreamGeminiAsOpenAI_WithCapture(t *testing.T) {
	input := `{"candidates":[{"content":{"parts":[{"text":"saved"}]},"finishReason":"STOP"}]}` + "\n"
	var buf strings.Builder
	var captured string
	StreamGeminiAsOpenAI(&buf, strings.NewReader(input), "model", &captured)
	if captured != "saved" {
		t.Fatalf("capture should be 'saved', got %q", captured)
	}
}

func TestStreamGeminiAsOpenAI_LengthFinish(t *testing.T) {
	input := `{"candidates":[{"content":{"parts":[{"text":"x"}]},"finishReason":"MAX_TOKENS"}]}` + "\n"
	var buf strings.Builder
	StreamGeminiAsOpenAI(&buf, strings.NewReader(input), "model", nil)
	if !strings.Contains(buf.String(), `"finish_reason":"length"`) {
		t.Fatal("MAX_TOKENS should map to length")
	}
}

func TestStreamGeminiAsOpenAI_ContentFilterFinish(t *testing.T) {
	input := `{"candidates":[{"content":{"parts":[{"text":"x"}]},"finishReason":"SAFETY"}]}` + "\n"
	var buf strings.Builder
	StreamGeminiAsOpenAI(&buf, strings.NewReader(input), "model", nil)
	if !strings.Contains(buf.String(), `"finish_reason":"content_filter"`) {
		t.Fatal("SAFETY should map to content_filter")
	}
}

func TestStreamGeminiAsAnthropic_EmptyInput(t *testing.T) {
	var buf strings.Builder
	accumulated := StreamGeminiAsAnthropic(&buf, strings.NewReader(""), "model", nil)
	if accumulated != "" {
		t.Fatalf("expected empty, got %q", accumulated)
	}
}
