/*
Package streaming handles Server-Sent Events formatting and Gemini chunk
parsing for Bulwarkai's streaming responses.

Vertex AI returns streaming data as a JSON array or newline-delimited JSON
objects. This package parses those into text chunks and finish reasons, then
formats them as SSE for Anthropic or OpenAI clients.

Exported functions:

  - StreamGeminiAsAnthropic: reads a Vertex AI stream and writes Anthropic-
    format SSE events (message_start, content_block_delta, message_stop)
  - StreamGeminiAsOpenAI: reads a Vertex AI stream and writes OpenAI-format
    SSE events (chat.completion.chunk, [DONE])
  - WriteAnthropicSSE: writes a single Anthropic SSE event
  - WriteOpenAISSE: writes a single OpenAI SSE event
  - ParseGeminiChunk: parses a single line from Vertex AI's streaming
    response into text, finish reason, and raw JSON
*/
package streaming
