package streaming

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Daviey/bulwarkai/internal/translate"
)

func toJSON(v interface{}) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func WriteAnthropicSSE(w io.Writer, msg map[string]interface{}) {
	usage, _ := msg["usage"].(map[string]interface{})
	content, _ := msg["content"].([]interface{})

	fmt.Fprintf(w, "event: message_start\ndata: %s\r\n\r\n", toJSON(map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id": msg["id"], "type": "message", "role": "assistant",
			"content": []interface{}{}, "model": msg["model"],
			"stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]interface{}{"input_tokens": translate.IntVal(usage["input_tokens"]), "output_tokens": 0},
		},
	}))

	for i, block := range content {
		b, _ := block.(map[string]interface{})
		fmt.Fprintf(w, "event: content_block_start\ndata: %s\r\n\r\n", toJSON(map[string]interface{}{
			"type": "content_block_start", "index": i,
			"content_block": map[string]string{"type": "text", "text": ""},
		}))
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\r\n\r\n", toJSON(map[string]interface{}{
			"type": "content_block_delta", "index": i,
			"delta": map[string]string{"type": "text_delta", "text": translate.StrVal(b["text"], "")},
		}))
		fmt.Fprintf(w, "event: content_block_stop\ndata: %s\r\n\r\n", toJSON(map[string]interface{}{
			"type": "content_block_stop", "index": i,
		}))
	}

	fmt.Fprintf(w, "event: message_delta\ndata: %s\r\n\r\n", toJSON(map[string]interface{}{
		"type":  "message_delta",
		"delta": map[string]interface{}{"stop_reason": msg["stop_reason"], "stop_sequence": nil},
		"usage": map[string]interface{}{"output_tokens": translate.IntVal(usage["output_tokens"])},
	}))
	fmt.Fprintf(w, "event: message_stop\ndata: %s\r\n\r\n", toJSON(map[string]string{"type": "message_stop"}))
}

func WriteOpenAISSE(w io.Writer, msg map[string]interface{}) {
	choices, _ := msg["choices"].([]interface{})
	choice, _ := choices[0].(map[string]interface{})
	message, _ := choice["message"].(map[string]interface{})

	fmt.Fprintf(w, "data: %s\n\n", toJSON(map[string]interface{}{
		"id": msg["id"], "object": "chat.completion.chunk",
		"created": msg["created"], "model": msg["model"],
		"choices": []interface{}{map[string]interface{}{
			"index": 0, "delta": map[string]string{"role": "assistant", "content": translate.StrVal(message["content"], "")},
			"finish_reason": nil,
		}},
	}))
	fmt.Fprintf(w, "data: %s\n\n", toJSON(map[string]interface{}{
		"id": msg["id"], "object": "chat.completion.chunk",
		"created": msg["created"], "model": msg["model"],
		"choices": []interface{}{map[string]interface{}{
			"index": 0, "delta": map[string]interface{}{}, "finish_reason": choice["finish_reason"],
		}},
	}))
	fmt.Fprintf(w, "data: [DONE]\n\n")
}

func StreamGeminiAsAnthropic(w io.Writer, rc io.Reader, model string, capture *string) string {
	if capture != nil {
		*capture = ""
	}
	msgID := "msg_" + uuid.New().String()[:24]
	started := false
	totalTokens := 0
	accumulated := ""

	buf := make([]byte, 32*1024)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			for _, line := range strings.Split(chunk, "\n") {
				text, finish, _ := ParseGeminiChunk(line)
				if text == "" && finish == "" {
					continue
				}

				if !started {
					started = true
					fmt.Fprintf(w, "event: message_start\ndata: %s\r\n\r\n", toJSON(map[string]interface{}{
						"type": "message_start",
						"message": map[string]interface{}{
							"id": msgID, "type": "message", "role": "assistant",
							"content": []interface{}{}, "model": model,
							"stop_reason": nil, "stop_sequence": nil,
							"usage": map[string]interface{}{"input_tokens": 0, "output_tokens": 0},
						},
					}))
					fmt.Fprintf(w, "event: content_block_start\ndata: %s\r\n\r\n", toJSON(map[string]interface{}{
						"type": "content_block_start", "index": 0,
						"content_block": map[string]string{"type": "text", "text": ""},
					}))
				}

				if text != "" {
					totalTokens++
					accumulated += text
					fmt.Fprintf(w, "event: content_block_delta\ndata: %s\r\n\r\n", toJSON(map[string]interface{}{
						"type": "content_block_delta", "index": 0,
						"delta": map[string]string{"type": "text_delta", "text": text},
					}))
				}

				if finish != "" {
					stopReason := "end_turn"
					switch finish {
					case "MAX_TOKENS":
						stopReason = "max_tokens"
					case "SAFETY":
						stopReason = "stop_sequence"
					}
					fmt.Fprintf(w, "event: content_block_stop\ndata: %s\r\n\r\n", toJSON(map[string]interface{}{"type": "content_block_stop", "index": 0}))
					fmt.Fprintf(w, "event: message_delta\ndata: %s\r\n\r\n", toJSON(map[string]interface{}{
						"type":  "message_delta",
						"delta": map[string]interface{}{"stop_reason": stopReason, "stop_sequence": nil},
						"usage": map[string]interface{}{"output_tokens": totalTokens},
					}))
					fmt.Fprintf(w, "event: message_stop\ndata: %s\r\n\r\n", toJSON(map[string]string{"type": "message_stop"}))
					started = false
				}
			}
		}
		if err != nil {
			break
		}
	}

	if capture != nil {
		*capture = accumulated
	}
	return accumulated
}

func StreamGeminiAsOpenAI(w io.Writer, rc io.Reader, model string, capture *string) string {
	if capture != nil {
		*capture = ""
	}
	chatID := "chatcmpl-" + uuid.New().String()[:24]
	created := time.Now().Unix()
	roleSent := false
	accumulated := ""

	buf := make([]byte, 32*1024)
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			for _, line := range strings.Split(chunk, "\n") {
				text, finish, _ := ParseGeminiChunk(line)
				if text == "" && finish == "" {
					continue
				}

				if !roleSent {
					roleSent = true
					fmt.Fprintf(w, "data: %s\n\n", toJSON(map[string]interface{}{
						"id": chatID, "object": "chat.completion.chunk", "created": created, "model": model,
						"choices": []interface{}{map[string]interface{}{"index": 0, "delta": map[string]string{"role": "assistant"}, "finish_reason": nil}},
					}))
				}

				if text != "" {
					accumulated += text
					fmt.Fprintf(w, "data: %s\n\n", toJSON(map[string]interface{}{
						"id": chatID, "object": "chat.completion.chunk", "created": created, "model": model,
						"choices": []interface{}{map[string]interface{}{"index": 0, "delta": map[string]string{"content": text}, "finish_reason": nil}},
					}))
				}

				if finish != "" {
					finishReason := "stop"
					switch finish {
					case "MAX_TOKENS":
						finishReason = "length"
					case "SAFETY":
						finishReason = "content_filter"
					}
					fmt.Fprintf(w, "data: %s\n\n", toJSON(map[string]interface{}{
						"id": chatID, "object": "chat.completion.chunk", "created": created, "model": model,
						"choices": []interface{}{map[string]interface{}{"index": 0, "delta": map[string]interface{}{}, "finish_reason": finishReason}},
					}))
					fmt.Fprintf(w, "data: [DONE]\n\n")
					roleSent = false
				}
			}
		}
		if err != nil {
			break
		}
	}

	if capture != nil {
		*capture = accumulated
	}
	return accumulated
}

func ParseGeminiChunk(line string) (text, finish string, json_ map[string]interface{}) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || trimmed == "[" || trimmed == "]" || trimmed == "," {
		return
	}
	var jsonStr string
	if strings.HasPrefix(trimmed, "data:") {
		jsonStr = strings.TrimSpace(trimmed[5:])
	} else if strings.HasPrefix(trimmed, "[") {
		jsonStr = strings.TrimPrefix(trimmed, "[")
	} else {
		jsonStr = trimmed
	}
	jsonStr = strings.TrimSuffix(jsonStr, ",")
	jsonStr = strings.TrimSuffix(jsonStr, "]")
	jsonStr = strings.TrimSpace(jsonStr)
	if jsonStr == "" {
		return
	}
	if json.Unmarshal([]byte(jsonStr), &json_) != nil {
		return
	}

	cands, _ := json_["candidates"].([]interface{})
	if len(cands) > 0 {
		cand, _ := cands[0].(map[string]interface{})
		if cand["finishReason"] != nil {
			finish = translate.StrVal(cand["finishReason"], "")
		}
		content, _ := cand["content"].(map[string]interface{})
		parts, _ := content["parts"].([]interface{})
		if len(parts) > 0 {
			part, _ := parts[0].(map[string]interface{})
			if t, ok := part["text"].(string); ok {
				text = t
			}
		}
	}
	return
}
