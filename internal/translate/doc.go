/*
Package translate converts between Anthropic, OpenAI, and Gemini API formats.

Bulwarkai accepts requests in three formats and normalises them to Gemini
before calling Vertex AI. On the way back, Gemini responses are converted
to the caller's expected format.

Prompt extraction:

  - ExtractAnthropicPrompt: reads the text from an Anthropic messages array
  - ExtractOpenAIPrompt: reads the text from an OpenAI messages array

Request translation:

  - TranslateToGemini: converts an incoming request body to Gemini's
    contents array, handling system instructions and generation config

Response translation:

  - TranslateGeminiToAnthropic: builds an Anthropic Message from a Gemini
    response
  - TranslateGeminiToOpenAI: builds an OpenAI ChatCompletion from a Gemini
    response
  - ExtractGeminiText: pulls the text content out of a Gemini response

Helper functions:

  - StrVal: safe string extraction from map[string]interface{}
  - IntVal: safe int extraction from map[string]interface{}
*/
package translate
