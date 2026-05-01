package translate

import (
	"strings"
	"testing"
)

func TestIntVal_NonFloat(t *testing.T) {
	if IntVal("not a number") != 0 {
		t.Fatal("non-float should return 0")
	}
	if IntVal(nil) != 0 {
		t.Fatal("nil should return 0")
	}
	if IntVal(float64(42)) != 42 {
		t.Fatal("float64 42 should return 42")
	}
}

func TestStrVal_Defaults(t *testing.T) {
	if StrVal(nil, "def") != "def" {
		t.Fatal("nil should use default")
	}
	if StrVal(42, "def") != "def" {
		t.Fatal("non-string should use default")
	}
	if StrVal("hi", "def") != "hi" {
		t.Fatal("string should be returned")
	}
}

func TestExtractAnthropicPrompt_NoMessages(t *testing.T) {
	got := ExtractAnthropicPrompt(map[string]interface{}{})
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExtractAnthropicPrompt_ContentBlocks(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "hello"},
					map[string]interface{}{"type": "text", "text": "world"},
				},
			},
		},
	}
	got := ExtractAnthropicPrompt(body)
	if got != "hello world" {
		t.Fatalf("expected 'hello world', got %q", got)
	}
}

func TestExtractAnthropicPrompt_StringContent(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	got := ExtractAnthropicPrompt(body)
	if got != "hi" {
		t.Fatalf("expected 'hi', got %q", got)
	}
}

func TestExtractAnthropicPrompt_NonTextBlock(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "image", "data": "base64..."},
					map[string]interface{}{"type": "text", "text": "describe this"},
				},
			},
		},
	}
	got := ExtractAnthropicPrompt(body)
	if got != "describe this" {
		t.Fatalf("expected 'describe this', got %q", got)
	}
}

func TestExtractOpenAIPrompt_GeminiContents(t *testing.T) {
	body := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"parts": []interface{}{
					map[string]interface{}{"text": "hello"},
				},
			},
		},
	}
	got := ExtractOpenAIPrompt(body)
	if got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestExtractOpenAIPrompt_NoContent(t *testing.T) {
	got := ExtractOpenAIPrompt(map[string]interface{}{})
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExtractMessageText_EmptyContent(t *testing.T) {
	got := ExtractMessageText(map[string]interface{}{})
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExtractMessageText_NestedStringBlocks(t *testing.T) {
	msg := map[string]interface{}{
		"content": []interface{}{"plain text", "more text"},
	}
	got := ExtractMessageText(msg)
	if got != "plain text more text" {
		t.Fatalf("expected 'plain text more text', got %q", got)
	}
}

func TestTranslateToGemini_WithSystem(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
			map[string]interface{}{"role": "assistant", "content": "hello"},
		},
	}
	got := TranslateToGemini(body, "be helpful", map[string]interface{}{})
	contents := got["contents"].([]interface{})
	if len(contents) != 4 {
		t.Fatalf("expected 4 content blocks (2 system + 2 messages), got %d", len(contents))
	}
	first := contents[0].(map[string]interface{})
	if first["role"] != "user" {
		t.Fatal("first block should be user (system)")
	}
	second := contents[1].(map[string]interface{})
	if second["role"] != "model" {
		t.Fatal("second block should be model ack")
	}
	userMsg := contents[2].(map[string]interface{})
	if userMsg["role"] != "user" {
		t.Fatal("third block should be user")
	}
	assistantMsg := contents[3].(map[string]interface{})
	if assistantMsg["role"] != "model" {
		t.Fatal("fourth block should be model (assistant)")
	}
}

func TestTranslateToGemini_NoSystem(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	got := TranslateToGemini(body, "", map[string]interface{}{})
	contents := got["contents"].([]interface{})
	if len(contents) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(contents))
	}
}

func TestTranslateToGemini_SkipsBadMessages(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{
			"not a map",
			map[string]interface{}{"role": "user", "content": "hi"},
		},
	}
	got := TranslateToGemini(body, "", map[string]interface{}{})
	contents := got["contents"].([]interface{})
	if len(contents) != 1 {
		t.Fatalf("expected 1 content block (bad message skipped), got %d", len(contents))
	}
}

func TestExtractGenerationConfig_AllFields(t *testing.T) {
	body := map[string]interface{}{
		"max_tokens":        float64(100),
		"temperature":       float64(0.7),
		"top_p":             float64(0.9),
		"top_k":             float64(40),
		"frequency_penalty": float64(0.5),
		"presence_penalty":  float64(0.3),
		"stop_sequences":    []interface{}{"STOP"},
	}
	gc := ExtractGenerationConfig(body)
	if gc["maxOutputTokens"] != float64(100) {
		t.Fatal("maxOutputTokens mismatch")
	}
	if gc["temperature"] != float64(0.7) {
		t.Fatal("temperature mismatch")
	}
	if gc["topP"] != float64(0.9) {
		t.Fatal("topP mismatch")
	}
	if gc["topK"] != float64(40) {
		t.Fatal("topK mismatch")
	}
	if gc["frequencyPenalty"] != float64(0.5) {
		t.Fatal("frequencyPenalty mismatch")
	}
	if gc["presencePenalty"] != float64(0.3) {
		t.Fatal("presencePenalty mismatch")
	}
	if gc["stopSequences"] == nil {
		t.Fatal("stopSequences should be set")
	}
}

func TestExtractGenerationConfig_StopString(t *testing.T) {
	body := map[string]interface{}{
		"stop": "END",
	}
	gc := ExtractGenerationConfig(body)
	seqs, ok := gc["stopSequences"].([]string)
	if !ok || len(seqs) != 1 || seqs[0] != "END" {
		t.Fatalf("expected [\"END\"], got %v", gc["stopSequences"])
	}
}

func TestExtractGenerationConfig_StopArray(t *testing.T) {
	body := map[string]interface{}{
		"stop": []interface{}{"A", "B"},
	}
	gc := ExtractGenerationConfig(body)
	arr, ok := gc["stopSequences"].([]interface{})
	if !ok || len(arr) != 2 {
		t.Fatalf("expected 2-element array, got %v", gc["stopSequences"])
	}
}

func TestExtractGenerationConfig_Empty(t *testing.T) {
	gc := ExtractGenerationConfig(map[string]interface{}{})
	if len(gc) != 0 {
		t.Fatalf("empty body should produce empty config, got %v", gc)
	}
}

func TestTranslateGeminiToAnthropic_Basic(t *testing.T) {
	resp := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"parts": []interface{}{
						map[string]interface{}{"text": "Hello!"},
					},
				},
				"finishReason": "STOP",
			},
		},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     float64(10),
			"candidatesTokenCount": float64(5),
		},
	}
	got := TranslateGeminiToAnthropic(resp, "gemini-2.5-flash")
	if !strings.HasPrefix(got["id"].(string), "msg_") {
		t.Fatal("id should have msg_ prefix")
	}
	if got["type"] != "message" {
		t.Fatal("type should be message")
	}
	if got["role"] != "assistant" {
		t.Fatal("role should be assistant")
	}
	if got["stop_reason"] != "end_turn" {
		t.Fatalf("stop_reason should be end_turn, got %v", got["stop_reason"])
	}
	content := got["content"].([]interface{})
	block := content[0].(map[string]interface{})
	if block["text"] != "Hello!" {
		t.Fatal("content text mismatch")
	}
	usage := got["usage"].(map[string]interface{})
	if usage["input_tokens"] != 10 {
		t.Fatal("input_tokens mismatch")
	}
}

func TestTranslateGeminiToAnthropic_MaxTokens(t *testing.T) {
	resp := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content":      map[string]interface{}{"parts": []interface{}{map[string]interface{}{"text": "cut"}}},
				"finishReason": "MAX_TOKENS",
			},
		},
	}
	got := TranslateGeminiToAnthropic(resp, "model")
	if got["stop_reason"] != "max_tokens" {
		t.Fatal("MAX_TOKENS should map to max_tokens")
	}
}

func TestTranslateGeminiToAnthropic_Safety(t *testing.T) {
	resp := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content":      map[string]interface{}{"parts": []interface{}{map[string]interface{}{"text": ""}}},
				"finishReason": "SAFETY",
			},
		},
	}
	got := TranslateGeminiToAnthropic(resp, "model")
	if got["stop_reason"] != "stop_sequence" {
		t.Fatal("SAFETY should map to stop_sequence")
	}
}

func TestTranslateGeminiToOpenAI_Basic(t *testing.T) {
	resp := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"parts": []interface{}{
						map[string]interface{}{"text": "Hi there"},
					},
				},
				"finishReason": "STOP",
			},
		},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":     float64(8),
			"candidatesTokenCount": float64(3),
			"totalTokenCount":      float64(11),
		},
	}
	got := TranslateGeminiToOpenAI(resp, "gemini-2.5-flash")
	if !strings.HasPrefix(got["id"].(string), "chatcmpl-") {
		t.Fatal("id should have chatcmpl- prefix")
	}
	if got["object"] != "chat.completion" {
		t.Fatal("object should be chat.completion")
	}
	choices := got["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	msg := choice["message"].(map[string]interface{})
	if msg["content"] != "Hi there" {
		t.Fatal("content mismatch")
	}
	if choice["finish_reason"] != "stop" {
		t.Fatal("finish_reason should be stop")
	}
	usage := got["usage"].(map[string]interface{})
	if usage["total_tokens"] != 11 {
		t.Fatal("total_tokens mismatch")
	}
}

func TestTranslateGeminiToOpenAI_MaxTokens(t *testing.T) {
	resp := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content":      map[string]interface{}{"parts": []interface{}{map[string]interface{}{"text": "x"}}},
				"finishReason": "MAX_TOKENS",
			},
		},
	}
	got := TranslateGeminiToOpenAI(resp, "model")
	choices := got["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	if choice["finish_reason"] != "length" {
		t.Fatal("MAX_TOKENS should map to length")
	}
}

func TestTranslateGeminiToOpenAI_Safety(t *testing.T) {
	resp := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content":      map[string]interface{}{"parts": []interface{}{map[string]interface{}{"text": ""}}},
				"finishReason": "SAFETY",
			},
		},
	}
	got := TranslateGeminiToOpenAI(resp, "model")
	choices := got["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	if choice["finish_reason"] != "content_filter" {
		t.Fatal("SAFETY should map to content_filter")
	}
}

func TestExtractGeminiText_Empty(t *testing.T) {
	if ExtractGeminiText(map[string]interface{}{}) != "" {
		t.Fatal("no candidates should return empty")
	}
	if ExtractGeminiText(map[string]interface{}{"candidates": []interface{}{}}) != "" {
		t.Fatal("empty candidates should return empty")
	}
	cand := map[string]interface{}{"content": map[string]interface{}{"parts": []interface{}{}}}
	if ExtractGeminiText(map[string]interface{}{"candidates": []interface{}{cand}}) != "" {
		t.Fatal("empty parts should return empty")
	}
}

func TestExtractGeminiText_WithText(t *testing.T) {
	resp := map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"content": map[string]interface{}{
					"parts": []interface{}{
						map[string]interface{}{"text": "response text"},
					},
				},
			},
		},
	}
	if ExtractGeminiText(resp) != "response text" {
		t.Fatal("text mismatch")
	}
}
