package translate

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

func StrVal(v interface{}, def string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return def
}

func IntVal(v interface{}) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

func ExtractAnthropicPrompt(body map[string]interface{}) string {
	msgs, ok := body["messages"].([]interface{})
	if !ok {
		return ""
	}
	var parts []string
	for _, m := range msgs {
		if msg, ok := m.(map[string]interface{}); ok {
			parts = append(parts, ExtractMessageText(msg))
		}
	}
	return strings.Join(parts, " ")
}

func ExtractOpenAIPrompt(body map[string]interface{}) string {
	if _, ok := body["messages"].([]interface{}); ok {
		return ExtractAnthropicPrompt(body)
	}
	if contents, ok := body["contents"].([]interface{}); ok {
		var parts []string
		for _, c := range contents {
			if content, ok := c.(map[string]interface{}); ok {
				if contentParts, ok := content["parts"].([]interface{}); ok {
					for _, p := range contentParts {
						if part, ok := p.(map[string]interface{}); ok {
							if t, ok := part["text"].(string); ok {
								parts = append(parts, t)
							}
						}
					}
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func ExtractMessageText(msg map[string]interface{}) string {
	switch c := msg["content"].(type) {
	case string:
		return c
	case []interface{}:
		var parts []string
		for _, b := range c {
			switch block := b.(type) {
			case string:
				parts = append(parts, block)
			case map[string]interface{}:
				if block["type"] == "text" {
					parts = append(parts, StrVal(block["text"], ""))
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func TranslateToGemini(body map[string]interface{}, system string, genConfig map[string]interface{}) map[string]interface{} {
	var contents []interface{}
	if system != "" {
		contents = append(contents,
			map[string]interface{}{"role": "user", "parts": []interface{}{map[string]string{"text": system}}},
			map[string]interface{}{"role": "model", "parts": []interface{}{map[string]string{"text": "Understood."}}},
		)
	}

	msgs, _ := body["messages"].([]interface{})
	for _, m := range msgs {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role := "user"
		if msg["role"] == "assistant" {
			role = "model"
		}
		text := ExtractMessageText(msg)
		if text != "" {
			contents = append(contents, map[string]interface{}{
				"role":  role,
				"parts": []interface{}{map[string]string{"text": text}},
			})
		}
	}

	return map[string]interface{}{"contents": contents, "generationConfig": genConfig}
}

func ExtractGenerationConfig(body map[string]interface{}) map[string]interface{} {
	gc := map[string]interface{}{}
	if v, ok := body["max_tokens"].(float64); ok {
		gc["maxOutputTokens"] = v
	}
	if v, ok := body["temperature"].(float64); ok {
		gc["temperature"] = v
	}
	if v, ok := body["top_p"].(float64); ok {
		gc["topP"] = v
	}
	if v, ok := body["top_k"].(float64); ok {
		gc["topK"] = v
	}
	if v, ok := body["frequency_penalty"].(float64); ok {
		gc["frequencyPenalty"] = v
	}
	if v, ok := body["presence_penalty"].(float64); ok {
		gc["presencePenalty"] = v
	}
	if v, ok := body["stop_sequences"].([]interface{}); ok {
		gc["stopSequences"] = v
	}
	if v, ok := body["stop"].(string); ok {
		gc["stopSequences"] = []string{v}
	}
	if v, ok := body["stop"].([]interface{}); ok {
		gc["stopSequences"] = v
	}
	return gc
}

func TranslateGeminiToAnthropic(vertexResp map[string]interface{}, model string) map[string]interface{} {
	text := ExtractGeminiText(vertexResp)
	usage, _ := vertexResp["usageMetadata"].(map[string]interface{})
	stopReason := "end_turn"
	if cands, ok := vertexResp["candidates"].([]interface{}); ok && len(cands) > 0 {
		if cand, ok := cands[0].(map[string]interface{}); ok {
			switch cand["finishReason"] {
			case "MAX_TOKENS":
				stopReason = "max_tokens"
			case "SAFETY":
				stopReason = "stop_sequence"
			}
		}
	}

	return map[string]interface{}{
		"id":            "msg_" + uuid.New().String()[:24],
		"type":          "message",
		"role":          "assistant",
		"model":         model,
		"content":       []interface{}{map[string]interface{}{"type": "text", "text": text}},
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage": map[string]interface{}{
			"input_tokens":  IntVal(usage["promptTokenCount"]),
			"output_tokens": IntVal(usage["candidatesTokenCount"]),
		},
	}
}

func TranslateGeminiToOpenAI(vertexResp map[string]interface{}, model string) map[string]interface{} {
	text := ExtractGeminiText(vertexResp)
	usage, _ := vertexResp["usageMetadata"].(map[string]interface{})
	finishReason := "stop"
	if cands, ok := vertexResp["candidates"].([]interface{}); ok && len(cands) > 0 {
		if cand, ok := cands[0].(map[string]interface{}); ok {
			switch cand["finishReason"] {
			case "MAX_TOKENS":
				finishReason = "length"
			case "SAFETY":
				finishReason = "content_filter"
			}
		}
	}

	return map[string]interface{}{
		"id":      "chatcmpl-" + uuid.New().String()[:24],
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"message":       map[string]interface{}{"role": "assistant", "content": text},
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     IntVal(usage["promptTokenCount"]),
			"completion_tokens": IntVal(usage["candidatesTokenCount"]),
			"total_tokens":      IntVal(usage["totalTokenCount"]),
		},
	}
}

func ExtractGeminiText(vertexResp map[string]interface{}) string {
	cands, _ := vertexResp["candidates"].([]interface{})
	if len(cands) == 0 {
		return ""
	}
	cand, _ := cands[0].(map[string]interface{})
	content, _ := cand["content"].(map[string]interface{})
	parts, _ := content["parts"].([]interface{})
	if len(parts) == 0 {
		return ""
	}
	part, _ := parts[0].(map[string]interface{})
	return StrVal(part["text"], "")
}
