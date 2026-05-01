package handler

import (
	"encoding/json"
	"net/http"

	"github.com/Daviey/bulwarkai/internal/auth"
	"github.com/Daviey/bulwarkai/internal/streaming"
	"github.com/Daviey/bulwarkai/internal/translate"
)

// @Summary OpenAI Chat Completions API
// @Description Proxies OpenAI-format requests to Vertex AI Gemini, screening prompt and response
// @Tags OpenAI
// @Accept json
// @Produce json
// @Param request body map[string]interface{} true "OpenAI Chat Completions request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {string} string "invalid json"
// @Failure 401 {string} string "unauthorized"
// @Router /v1/chat/completions [post]
func (s *Server) ServeOpenAI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	identity, ok := auth.Authenticate(s.cfg, s.httpClient, w, r)
	if !ok {
		return
	}
	if !auth.CheckUserAgent(s.cfg, w, r) {
		return
	}

	var body map[string]interface{}
	if !parseBody(w, r, &body) {
		return
	}

	isStream := false
	if v, ok := body["stream"].(bool); ok {
		isStream = v
	}
	model := translate.StrVal(body["model"], s.cfg.FallbackGeminiModel)

	if !s.checkPolicy(w, r, identity, model, isStream) {
		return
	}

	prompt := translate.ExtractOpenAIPrompt(body)

	br := s.chain.ScreenPrompt(r.Context(), prompt, identity.AccessToken)
	if br != nil {
		s.logCtx(r.Context(), "BLOCK_PROMPT", model, prompt, br.Reason, identity.Email)
		writeOpenAIError(w, br.Reason)
		return
	}

	systemMsg := ""
	var nonSystemMessages []interface{}
	if msgs, ok := body["messages"].([]interface{}); ok {
		for _, m := range msgs {
			if msg, ok := m.(map[string]interface{}); ok {
				if msg["role"] == "system" {
					systemMsg = translate.StrVal(msg["content"], "")
				} else {
					nonSystemMessages = append(nonSystemMessages, m)
				}
			}
		}
	}

	generationConfig := translate.ExtractGenerationConfig(body)
	geminiBody := translate.TranslateToGemini(map[string]interface{}{"messages": nonSystemMessages}, systemMsg, generationConfig)

	switch s.cfg.ResponseMode {
	case "strict":
		s.handleOpenAIStrict(w, r, geminiBody, identity, model, prompt, isStream)
	case "fast":
		s.handleOpenAIFast(w, r, geminiBody, identity, model, prompt, isStream)
	case "audit":
		s.handleOpenAIAudit(w, r, geminiBody, identity, model, prompt, isStream)
	}
}

func (s *Server) handleOpenAIStrict(w http.ResponseWriter, r *http.Request, geminiBody map[string]interface{}, identity *auth.Identity, model, prompt string, stream bool) {
	jsonBytes, err := s.vertex.CallJSON(r.Context(), geminiBody, identity.AccessToken, false)
	if err != nil {
		writeOpenAIError(w, err.Error())
		return
	}
	var vertexResp map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &vertexResp); err != nil {
		writeOpenAIError(w, "vertex response decode error")
		return
	}

	if pf, ok := vertexResp["promptFeedback"].(map[string]interface{}); ok && pf["blockReason"] != nil {
		s.logCtx(r.Context(), "MODEL_ARMOR_BLOCK", model, prompt, translate.StrVal(pf["blockReasonMessage"], ""), identity.Email)
		writeOpenAIError(w, "Model Armor: "+translate.StrVal(pf["blockReasonMessage"], ""))
		return
	}

	responseText := translate.ExtractGeminiText(vertexResp)
	if responseText != "" {
		if verdict := s.chain.ScreenResponse(r.Context(), responseText, identity.AccessToken); verdict != nil {
			s.logCtx(r.Context(), "BLOCK_RESPONSE", model, "", verdict.Reason, identity.Email)
			writeOpenAIError(w, verdict.Reason)
			return
		}
	}

	s.logCtx(r.Context(), "ALLOW", model, prompt, "", identity.Email)
	msg := translate.TranslateGeminiToOpenAI(vertexResp, model)

	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		streaming.WriteOpenAISSE(w, msg)
	} else {
		writeJSON(w, msg)
	}
}

func (s *Server) handleOpenAIFast(w http.ResponseWriter, r *http.Request, geminiBody map[string]interface{}, identity *auth.Identity, model, prompt string, stream bool) {
	if stream {
		rc, err := s.vertex.CallStream(r.Context(), geminiBody, identity.AccessToken)
		if err != nil {
			writeOpenAIError(w, err.Error())
			return
		}
		defer rc.Close()
		s.logCtx(r.Context(), "ALLOW_FAST", model, prompt, "", identity.Email)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		streaming.StreamGeminiAsOpenAI(w, rc, model, nil)
	} else {
		jsonBytes, err := s.vertex.CallJSON(r.Context(), geminiBody, identity.AccessToken, false)
		if err != nil {
			writeOpenAIError(w, err.Error())
			return
		}
		var vertexResp map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &vertexResp); err != nil {
			writeOpenAIError(w, "vertex response decode error")
			return
		}
		s.logCtx(r.Context(), "ALLOW_FAST", model, prompt, "", identity.Email)
		writeJSON(w, translate.TranslateGeminiToOpenAI(vertexResp, model))
	}
}

func (s *Server) handleOpenAIAudit(w http.ResponseWriter, r *http.Request, geminiBody map[string]interface{}, identity *auth.Identity, model, prompt string, stream bool) {
	var accumulated string
	if stream {
		rc, err := s.vertex.CallStream(r.Context(), geminiBody, identity.AccessToken)
		if err != nil {
			writeOpenAIError(w, err.Error())
			return
		}
		defer rc.Close()
		s.logCtx(r.Context(), "ALLOW_AUDIT", model, prompt, "", identity.Email)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		accumulated = streaming.StreamGeminiAsOpenAI(w, rc, model, nil)
	} else {
		jsonBytes, err := s.vertex.CallJSON(r.Context(), geminiBody, identity.AccessToken, false)
		if err != nil {
			writeOpenAIError(w, err.Error())
			return
		}
		var vertexResp map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &vertexResp); err != nil {
			writeOpenAIError(w, "vertex response decode error")
			return
		}
		accumulated = translate.ExtractGeminiText(vertexResp)
		s.logCtx(r.Context(), "ALLOW_AUDIT", model, prompt, "", identity.Email)
		writeJSON(w, translate.TranslateGeminiToOpenAI(vertexResp, model))
	}
	s.auditResponse(r.Context(), accumulated, identity.AccessToken, model, identity.Email)
}

func writeOpenAIError(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{"message": "Blocked by bulwarkai: " + reason, "type": "invalid_request_error"},
	}); err != nil {
		_ = err
	}
}
