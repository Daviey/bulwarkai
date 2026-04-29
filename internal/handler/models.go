package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// @Summary List available models
// @Description Returns models available through the proxy (OpenAI-compatible)
// @Tags OpenAI
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /v1/models [get]
func (s *Server) ServeOpenAIModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]interface{}{
		"object": "list",
		"data":   s.modelList(),
	})
}

// @Summary Get model details
// @Description Returns details for a specific model
// @Tags OpenAI
// @Accept json
// @Produce json
// @Param model path string true "Model ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {string} string "model not found"
// @Router /v1/models/{model} [get]
func (s *Server) ServeOpenAIModelDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	modelID := strings.TrimPrefix(r.URL.Path, "/v1/models/")
	if modelID == "" {
		http.Error(w, "model id required", http.StatusBadRequest)
		return
	}
	for _, m := range s.modelList() {
		if m["id"] == modelID {
			writeJSON(w, m)
			return
		}
	}
	http.Error(w, fmt.Sprintf("model %q not found", modelID), http.StatusNotFound)
}

func (s *Server) modelList() []map[string]interface{} {
	owned := "bulwarkai"
	now := time.Now().Unix()
	return []map[string]interface{}{
		{
			"id":       s.cfg.FallbackGeminiModel,
			"object":   "model",
			"created":  now,
			"owned_by": owned,
			"permission": []map[string]interface{}{
				{"id": fmt.Sprintf("modelperm-%s", s.cfg.FallbackGeminiModel), "object": "model_permission", "created": now, "allow_create_engine": false, "allow_sampling": true, "allow_logprobs": false, "allow_search_indices": false, "allow_view": true, "allow_fine_tuning": false, "organization": "*", "group": nil, "is_blocking": false},
			},
		},
		{
			"id":       "gemini-2.5-pro",
			"object":   "model",
			"created":  now,
			"owned_by": owned,
			"permission": []map[string]interface{}{
				{"id": "modelperm-gemini-2.5-pro", "object": "model_permission", "created": now, "allow_create_engine": false, "allow_sampling": true, "allow_logprobs": false, "allow_search_indices": false, "allow_view": true, "allow_fine_tuning": false, "organization": "*", "group": nil, "is_blocking": false},
			},
		},
		{
			"id":       "gemini-2.5-flash",
			"object":   "model",
			"created":  now,
			"owned_by": owned,
			"permission": []map[string]interface{}{
				{"id": "modelperm-gemini-2.5-flash", "object": "model_permission", "created": now, "allow_create_engine": false, "allow_sampling": true, "allow_logprobs": false, "allow_search_indices": false, "allow_view": true, "allow_fine_tuning": false, "organization": "*", "group": nil, "is_blocking": false},
			},
		},
	}
}
