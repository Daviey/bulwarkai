/*
Package config loads Bulwarkai configuration from environment variables.

All runtime settings come from the process environment. The Load function
reads and validates them once at startup. There are no config files, no
flags, and no dynamic reloading.

Environment variables:

	GOOGLE_CLOUD_PROJECT      GCP project ID (required)
	GOOGLE_CLOUD_LOCATION     GCP region (default: europe-west2)
	ALLOWED_DOMAINS           Comma-separated email domain allowlist
	FALLBACK_GEMINI_MODEL     Model when request omits one (default: gemini-2.5-flash)
	RESPONSE_MODE             strict, fast (alias: input_only), or audit (alias: buffer) (default: strict)
	MODEL_ARMOR_TEMPLATE      Model Armor template name
	MODEL_ARMOR_LOCATION      Model Armor region
	API_KEYS                  Comma-separated valid API keys
	USER_AGENT_REGEX          Regex for User-Agent enforcement
	DLP_API                   Set to "true" to enable DLP inspector
	PORT                      HTTP listen port (default: 8080)
	LOG_LEVEL                 info or debug (default: info)
	LOG_PROMPT_MODE           truncate, hash, full, or none (default: truncate)
	LOCAL_MODE                Set to "true" for local dev
*/
package config
