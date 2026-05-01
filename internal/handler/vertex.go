package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Daviey/bulwarkai/internal/auth"
	"github.com/Daviey/bulwarkai/internal/translate"
)

// @Summary Vertex AI Gemini native format
// @Description Pass-through for Gemini native format requests (/models/{model}:generateContent or :streamGenerateContent)
// @Tags VertexAI
// @Accept json
// @Produce json
// @Param model path string true "Model name"
// @Param action path string true "Action (generateContent or streamGenerateContent)"
// @Success 200 {object} map[string]interface{}
// @Failure 401 {string} string "unauthorized"
// @Failure 502 {string} string "vertex ai error"
// @Router /models/{model}:{action} [post]
func (s *Server) ServeVertexCompat(w http.ResponseWriter, r *http.Request) {
	modelAndAction := strings.TrimPrefix(r.URL.Path, "/models/")
	s.handleVertexCompat(w, r, modelAndAction)
}

// @Summary Vertex AI full project path
// @Description Pass-through for full Vertex AI project path requests
// @Tags VertexAI
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /v1/projects/{project}/locations/{location}/models/{model}:{action} [post]
func (s *Server) ServeVertexProject(w http.ResponseWriter, r *http.Request) {
	idx := strings.Index(r.URL.Path, "/models/")
	if idx >= 0 {
		modelAndAction := r.URL.Path[idx+len("/models/"):]
		s.handleVertexCompat(w, r, modelAndAction)
	} else {
		http.NotFound(w, r)
	}
}

func (s *Server) handleVertexCompat(w http.ResponseWriter, r *http.Request, modelAndAction string) {
	identity, ok := auth.Authenticate(s.cfg, s.httpClient, w, r)
	if !ok {
		return
	}
	if !auth.CheckUserAgent(s.cfg, w, r) {
		return
	}

	colonIdx := strings.LastIndex(modelAndAction, ":")
	var model, action string
	if colonIdx >= 0 {
		model = modelAndAction[:colonIdx]
		action = modelAndAction[colonIdx+1:]
	} else {
		model = modelAndAction
		action = "generateContent"
	}
	if model == "" {
		model = s.cfg.FallbackGeminiModel
	}

	wasStreaming := strings.Contains(action, "stream")

	if !s.checkPolicy(w, r, identity, model, wasStreaming) {
		return
	}

	var body map[string]interface{}
	if !s.parseBody(w, r, &body) {
		return
	}

	isAnthropicFormat := body["anthropic_version"] != nil
	prompt := translate.ExtractAnthropicPrompt(body)
	if !isAnthropicFormat {
		prompt = translate.ExtractOpenAIPrompt(body)
	}

	br := s.chain.ScreenPrompt(r.Context(), prompt, identity.AccessToken)
	if br != nil {
		s.logCtx(r.Context(), "BLOCK_PROMPT", model, prompt, br.Reason, identity.Email)
		writeAnthropicError(w, br.Reason)
		return
	}

	var geminiBody map[string]interface{}
	if isAnthropicFormat {
		generationConfig := translate.ExtractGenerationConfig(body)
		geminiBody = translate.TranslateToGemini(body, translate.StrVal(body["system"], ""), generationConfig)
	} else {
		geminiBody = body
	}

	if wasStreaming {
		rc, err := s.vertex.CallStreamRaw(r.Context(), geminiBody, identity.AccessToken, model, action)
		if err != nil {
			ctxLogger(r.Context()).Error("vertex stream error", "model", model, "action", action, "error", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer rc.Close()
		s.logCtx(r.Context(), "ALLOW", model, prompt, "", identity.Email)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, canFlush := w.(http.Flusher)
		buf := make([]byte, 32*1024)
		for {
			n, err2 := rc.Read(buf)
			if n > 0 {
				if _, err := w.Write(buf[:n]); err != nil {
					ctxLogger(r.Context()).Error("stream write error", "error", err)
					return
				}
				if canFlush {
					flusher.Flush()
				}
			}
			if err2 != nil {
				break
			}
		}
	} else {
		jsonBytes, err := s.vertex.CallJSONForModel(r.Context(), geminiBody, identity.AccessToken, model, false)
		if err != nil {
			ctxLogger(r.Context()).Error("vertex json error", "model", model, "error", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		var vertexResp map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &vertexResp); err != nil {
			http.Error(w, string(jsonBytes), http.StatusBadGateway)
			return
		}

		if pf, ok := vertexResp["promptFeedback"].(map[string]interface{}); ok {
			if pf["blockReason"] != nil {
				s.logCtx(r.Context(), "MODEL_ARMOR_BLOCK", model, prompt, translate.StrVal(pf["blockReasonMessage"], ""), identity.Email)
				w.Header().Set("Content-Type", "application/json")
				if _, err := w.Write(jsonBytes); err != nil {
					ctxLogger(r.Context()).Error("write error", "error", err)
				}
				return
			}
		}

		responseText := translate.ExtractGeminiText(vertexResp)
		if responseText != "" {
			if verdict := s.chain.ScreenResponse(r.Context(), responseText, identity.AccessToken); verdict != nil {
				s.logCtx(r.Context(), "BLOCK_RESPONSE", model, "", verdict.Reason, identity.Email)
				writeAnthropicError(w, verdict.Reason)
				return
			}
		}

		s.logCtx(r.Context(), "ALLOW", model, prompt, "", identity.Email)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(jsonBytes); err != nil {
			ctxLogger(r.Context()).Error("write error", "error", err)
		}
	}
}
