/*
Package handler implements Bulwarkai's HTTP layer.

The Server struct holds the configuration, inspector chain, Vertex AI client,
and HTTP client. It exposes route registration and handler methods for each
API format:

  - ServeAnthropic: handles POST /v1/messages (Anthropic Messages API)
  - ServeOpenAI: handles POST /v1/chat/completions (OpenAI Chat Completions)
  - ServeVertexCompat: handles /models/:model:action (Gemini native)
  - ServeVertexProject: handles /v1/projects/:project/... (Vertex AI full path)

Additional endpoints:

  - /health: returns JSON with status and current mode
  - /test-strings: returns EICAR-style test strings for verifying screening

Routes returns an http.Handler with all endpoints registered and request
middleware applied (request ID, trace ID, status capture, duration logging).

Three response modes control how responses are screened:

  - strict:     screens both prompt and response synchronously, non-streaming
  - fast:        screens prompt only, streams response without inspection (alias: input_only)
  - audit:       streams response, then audits the accumulated text after (alias: buffer)
*/
package handler
