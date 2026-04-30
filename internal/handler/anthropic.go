package handler

import (
	"encoding/json"
	"net/http"

	"github.com/Daviey/bulwarkai/internal/auth"
	"github.com/Daviey/bulwarkai/internal/streaming"
	"github.com/Daviey/bulwarkai/internal/translate"
)

// @Summary Anthropic Messages API
// @Description Proxies Anthropic-format requests to Vertex AI Gemini, screening prompt and response
// @Tags Anthropic
// @Accept json
// @Produce json
// @Param request body map[string]interface{} true "Anthropic Messages request"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {string} string "invalid json"
// @Failure 401 {string} string "unauthorized"
// @Router /v1/messages [post]
func (s *Server) ServeAnthropic(w http.ResponseWriter, r *http.Request) {
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

	prompt := translate.ExtractAnthropicPrompt(body)

	br := s.chain.ScreenPrompt(r.Context(), prompt, identity.AccessToken)
	if br != nil {
		s.logAction("BLOCK_PROMPT", model, prompt, br.Reason, identity.Email)
		writeAnthropicError(w, br.Reason)
		return
	}

	generationConfig := translate.ExtractGenerationConfig(body)
	geminiBody := translate.TranslateToGemini(body, translate.StrVal(body["system"], ""), generationConfig)

	switch s.cfg.ResponseMode {
	case "strict":
		s.handleAnthropicStrict(w, r, geminiBody, identity, model, prompt, isStream)
	case "fast":
		s.handleAnthropicFast(w, r, geminiBody, identity, model, prompt, isStream)
	case "audit":
		s.handleAnthropicAudit(w, r, geminiBody, identity, model, prompt, isStream)
	}
}

func (s *Server) handleAnthropicStrict(w http.ResponseWriter, r *http.Request, geminiBody map[string]interface{}, identity *auth.Identity, model, prompt string, stream bool) {
	jsonBytes, err := s.vertex.CallJSON(r.Context(), geminiBody, identity.AccessToken, false)
	if err != nil {
		writeAnthropicError(w, err.Error())
		return
	}
	var vertexResp map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &vertexResp); err != nil {
		writeAnthropicError(w, "vertex response decode error")
		return
	}

	if pf, ok := vertexResp["promptFeedback"].(map[string]interface{}); ok && pf["blockReason"] != nil {
		s.logAction("MODEL_ARMOR_BLOCK", model, prompt, translate.StrVal(pf["blockReasonMessage"], ""), identity.Email)
		writeAnthropicError(w, "Model Armor: "+translate.StrVal(pf["blockReasonMessage"], ""))
		return
	}

	responseText := translate.ExtractGeminiText(vertexResp)
	if responseText != "" {
		if verdict := s.chain.ScreenResponse(r.Context(), responseText, identity.AccessToken); verdict != nil {
			s.logAction("BLOCK_RESPONSE", model, "", verdict.Reason, identity.Email)
			writeAnthropicError(w, verdict.Reason)
			return
		}
	}

	s.logAction("ALLOW", model, prompt, "", identity.Email)
	msg := translate.TranslateGeminiToAnthropic(vertexResp, model)

	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		streaming.WriteAnthropicSSE(w, msg)
	} else {
		writeJSON(w, msg)
	}
}

func (s *Server) handleAnthropicFast(w http.ResponseWriter, r *http.Request, geminiBody map[string]interface{}, identity *auth.Identity, model, prompt string, stream bool) {
	if stream {
		rc, err := s.vertex.CallStream(r.Context(), geminiBody, identity.AccessToken)
		if err != nil {
			writeAnthropicError(w, err.Error())
			return
		}
		defer rc.Close()
		s.logAction("ALLOW_FAST", model, prompt, "", identity.Email)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		streaming.StreamGeminiAsAnthropic(w, rc, model, nil)
	} else {
		jsonBytes, err := s.vertex.CallJSON(r.Context(), geminiBody, identity.AccessToken, false)
		if err != nil {
			writeAnthropicError(w, err.Error())
			return
		}
		var vertexResp map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &vertexResp); err != nil {
			writeAnthropicError(w, "vertex response decode error")
			return
		}
		s.logAction("ALLOW_FAST", model, prompt, "", identity.Email)
		writeJSON(w, translate.TranslateGeminiToAnthropic(vertexResp, model))
	}
}

func (s *Server) handleAnthropicAudit(w http.ResponseWriter, r *http.Request, geminiBody map[string]interface{}, identity *auth.Identity, model, prompt string, stream bool) {
	var accumulated string
	if stream {
		rc, err := s.vertex.CallStream(r.Context(), geminiBody, identity.AccessToken)
		if err != nil {
			writeAnthropicError(w, err.Error())
			return
		}
		defer rc.Close()
		s.logAction("ALLOW_AUDIT", model, prompt, "", identity.Email)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		accumulated = streaming.StreamGeminiAsAnthropic(w, rc, model, nil)
	} else {
		jsonBytes, err := s.vertex.CallJSON(r.Context(), geminiBody, identity.AccessToken, false)
		if err != nil {
			writeAnthropicError(w, err.Error())
			return
		}
		var vertexResp map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &vertexResp); err != nil {
			writeAnthropicError(w, "vertex response decode error")
			return
		}
		accumulated = translate.ExtractGeminiText(vertexResp)
		s.logAction("ALLOW_AUDIT", model, prompt, "", identity.Email)
		writeJSON(w, translate.TranslateGeminiToAnthropic(vertexResp, model))
	}
	s.auditResponse(r.Context(), accumulated, identity.AccessToken, model, identity.Email)
}

func writeAnthropicError(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"type":  "error",
		"error": map[string]string{"type": "api_error", "message": "Blocked by bulwarkai: " + reason},
	}); err != nil {
		_ = err
	}
}
