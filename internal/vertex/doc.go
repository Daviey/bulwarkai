/*
Package vertex provides a client for Google Vertex AI's Gemini API.

The Client struct wraps HTTP calls to Vertex AI's generateContent and
streamGenerateContent endpoints. It handles:

  - Building the correct URL for the configured project, location, and model
  - Setting the Authorization header with the user's forwarded access token
  - Falling back to Application Default Credentials when LOCAL_MODE is set
    and no forwarded token is present

Call methods:

  - CallJSON: non-streaming request, returns the full response body
  - CallStream: streaming request, returns the raw response body for SSE parsing
  - CallJSONForModel: non-streaming request targeting a specific model
  - CallStreamRaw: streaming request with explicit model and action
*/
package vertex
