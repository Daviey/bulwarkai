+++
title = "Client Configuration"
sort_by = "weight"
weight = 6
+++


## opencode

Two pieces: a plugin that mints tokens and patches fetch, and a config entry that points the provider at the Bulwarkai.

### opencode security advisory

[CVE-2026-22812](https://nvd.nist.gov/vuln/detail/CVE-2026-22812): Versions of opencode before 1.0.216 started an unauthenticated HTTP server on startup, allowing any local process or website to execute arbitrary shell commands.

[CVE-2026-22813](https://nvd.nist.gov/vuln/detail/CVE-2026-22813): The markdown renderer did not sanitize HTML in LLM responses, allowing JavaScript execution in the local web interface. Fixed in 1.1.10.

**Minimum version: 1.1.10.** Enforce this with the Bulwarkai User-Agent regex:

```
USER_AGENT_REGEX=^opencode/(1\.1\.1[0-9]|[2-9]\d+\.\d+).\*$
```

This rejects any opencode version below 1.1.10. Adjust the pattern as new versions are released. Set `USER_AGENT_REGEX` to empty to disable the check.

**opencode-plugin-model-armor.ts** (save anywhere on disk):

```ts
type Options = {
  provider: string
  floorServiceUrl: string
  location: string
  project: string
}

let cachedIdToken = { token: "", expires: 0 }
let cachedAccessToken = { token: "", expires: 0 }
let patchApplied = false
let adcCredentials: { client_id: string; client_secret: string; refresh_token: string } | null = null

async function loadAdc(): Promise<typeof adcCredentials> {
  if (adcCredentials) return adcCredentials
  const paths = [
    `${process.env.HOME}/.config/gcloud/application_default_credentials.json`,
  ]
  for (const p of paths) {
    try {
      const file = Bun.file(p)
      const json: any = await file.json()
      if (json.type === "authorized_user" && json.refresh_token) {
        adcCredentials = {
          client_id: json.client_id,
          client_secret: json.client_secret,
          refresh_token: json.refresh_token,
        }
        return adcCredentials
      }
    } catch {}
  }
  return null
}

async function getTokens(): Promise<{ idToken: string; accessToken: string }> {
  if (cachedIdToken.token && cachedAccessToken.token && Date.now() < cachedIdToken.expires) {
    return { idToken: cachedIdToken.token, accessToken: cachedAccessToken.token }
  }

  const adc = await loadAdc()
  if (!adc) return { idToken: "", accessToken: "" }

  try {
    const res = await fetch("https://oauth2.googleapis.com/token", {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({
        client_id: adc.client_id,
        client_secret: adc.client_secret,
        grant_type: "refresh_token",
        refresh_token: adc.refresh_token,
        scope: "https://www.googleapis.com/auth/cloud-platform openid email",
      }).toString(),
    })
    const json: any = await res.json()
    const idToken = json.id_token ?? ""
    const accessToken = json.access_token ?? ""
    if (idToken && accessToken) {
      const expires = Date.now() + 50 * 60 * 1000
      cachedIdToken = { token: idToken, expires }
      cachedAccessToken = { token: accessToken, expires }
    }
    return { idToken, accessToken }
  } catch (e) {
    console.error("[model-armor] ADC token error:", e)
  }

  return { idToken: "", accessToken: "" }
}

function patchGlobalFetch(opts: Options) {
  if (patchApplied) return
  patchApplied = true

  const floorUrl = opts.floorServiceUrl.replace(/\/$/, "")
  const realFetch = globalThis.fetch

  globalThis.fetch = async function (input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url

    if (!url.startsWith(floorUrl)) {
      return realFetch.call(globalThis, input, init)
    }

    const req = new Request(input, init)
    const { idToken, accessToken } = await getTokens()

    const headers: Record<string, string> = { "Content-Type": "application/json" }
    if (idToken) {
      headers["Authorization"] = `Bearer ${idToken}`
    }
    if (accessToken) {
      headers["X-Forwarded-Access-Token"] = accessToken
    }

    return realFetch.call(globalThis, req.url, {
      method: req.method,
      headers,
      body: req.body,
    })
  }
}

export default {
  id: "model-armor",

  server: async (_input: unknown, options?: Record<string, unknown>) => {
    const opts: Options = {
      provider: String(options?.provider ?? "google-vertex").trim(),
      floorServiceUrl: String(options?.floorServiceUrl ?? "").trim(),
      location: String(options?.location ?? "europe-west2").trim(),
      project: String(options?.project ?? "").trim(),
    }

    patchGlobalFetch(opts)

    return {
      auth: {
        provider: opts.provider,

        async loader(): Promise<Record<string, unknown>> {
          return {
            apiKey: "",
            baseURL: opts.floorServiceUrl.replace(/\/$/, ""),
            location: opts.location,
            project: opts.project,
          }
        },

        methods: [
          {
            type: "api",
            label: "Model Armor (Vertex ADC)",
            async authorize() {
              return {
                type: "success" as const,
                key: "model-armor-placeholder",
                provider: opts.provider,
              }
            },
          },
        ],
      },
    }
  },
}
```

The plugin reads your `gcloud` ADC credentials, requests a combined-scope token (`cloud-platform openid email`) from Google OAuth, then monkey-patches `globalThis.fetch` to inject `Authorization` (OIDC identity token) and `X-Forwarded-Access-Token` (OAuth access token) headers on any request headed for the Bulwarkai URL. Tokens are cached for 50 minutes.

**opencode.json** (`~/.config/opencode/opencode.json`):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "model": "google-vertex/gemini-2.5-flash",
  "small_model": "google-vertex/gemini-2.5-flash",
  "plugin": [
    ["file:///path/to/opencode-plugin-model-armor.ts", {
      "provider": "google-vertex",
      "floorServiceUrl": "https://bulwarkai-XXXXX-XXXXX.run.app",
      "gcloudAccount": "user@example.com"
    }]
  ]
}
```

Replace `floorServiceUrl` with your Cloud Run service URL. The `small_model` setting prevents opencode's title generation agent from requesting a model that doesn't exist.

## Claude Code

Claude Code uses the Anthropic Messages API, so it talks to the Bulwarkai's `/v1/messages` endpoint. Configuration is via environment variables:

```bash
export ANTHROPIC_BASE_URL="https://bulwarkai-XXXXX-XXXXX.run.app"
export ANTHROPIC_API_KEY="your-api-key-here"
```

Or set `ANTHROPIC_AUTH_TOKEN` if using bearer token auth. The API key must match one listed in the Bulwarkai's `API_KEYS` environment variable.

For bearer token authentication (forwarding user identity through to Vertex AI), you need a wrapper that mints OIDC + access tokens. A shell helper using `gcloud`:

```bash
#!/bin/bash
# claude-code-wrapper.sh
TOKENS=$(gcloud auth print-access-token --scopes="https://www.googleapis.com/auth/cloud-platform,openid,email" 2>/dev/null)
ID_TOKEN=$(gcloud auth print-identity-token 2>/dev/null)
ACCESS_TOKEN=$(echo "$TOKENS")

ANTHROPIC_BASE_URL="https://bulwarkai-XXXXX-XXXXX.run.app" \
ANTHROPIC_AUTH_TOKEN="$ID_TOKEN" \
  npx @anthropic-ai/claude-code "$@"
```

This requires `gcloud` to be authenticated with application-default credentials. The Bulwarkai receives the OIDC token as the bearer token and uses the forwarded access token for Vertex AI calls.

## curl (for testing)

Non-streaming OpenAI format:

```bash
curl -X POST https://bulwarkai-XXXXX.run.app/v1/chat/completions \
  -H "Authorization: Bearer $(gcloud auth print-identity-token)" \
  -H "X-Forwarded-Access-Token: $(gcloud auth print-access-token)" \
  -H "Content-Type: application/json" \
  -d '{"model":"gemini-2.5-flash","max_tokens":1024,"messages":[{"role":"user","content":"Hello"}]}'
```

Non-streaming Anthropic format:

```bash
curl -X POST https://bulwarkai-XXXXX.run.app/v1/messages \
  -H "Authorization: Bearer $(gcloud auth print-identity-token)" \
  -H "X-Forwarded-Access-Token: $(gcloud auth print-access-token)" \
  -H "Content-Type: application/json" \
  -d '{"model":"gemini-2.5-flash","max_tokens":1024,"messages":[{"role":"user","content":"Hello"}]}'
```

Vertex AI native format (passthrough):

```bash
curl -X POST https://bulwarkai-XXXXX.run.app/models/gemini-2.5-flash:generateContent \
  -H "Authorization: Bearer $(gcloud auth print-identity-token)" \
  -H "X-Forwarded-Access-Token: $(gcloud auth print-access-token)" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"role":"user","parts":[{"text":"Hello"}]}]}'
```

API key auth (simpler, no identity forwarding):

```bash
curl -X POST https://bulwarkai-XXXXX.run.app/v1/chat/completions \
  -H "X-Api-Key: your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"gemini-2.5-flash","max_tokens":1024,"messages":[{"role":"user","content":"Hello"}]}'
```

## Geographic restrictions

Anthropic restricts use of its products to [supported countries](https://www.anthropic.com/supported-countries). China and Hong Kong are not on this list. The relevant clauses for reference:

- **[Consumer Terms of Service](https://www.anthropic.com/terms)**, Section 3: _"You may access and use our Services only in compliance with our Terms, our Acceptable Use Policy."_ Section 11 (Export Controls): _"You may not export or provide access to the Services into any U.S. embargoed countries."_

- **[Anthropic on Vertex Commercial Terms](https://www-cdn.anthropic.com/471bd07290603ee509a5ea0d5ccf131ea5897232/anthropic-vertex-commercial-terms-march-2024.pdf)**, Section C.4: _"Customer and its Users may only use the Services in the countries and regions Anthropic currently supports."_

- **[Supported Countries page](https://www.anthropic.com/supported-countries)**: _"To the extent permitted by law, Anthropic reserves the right to not provide its products or services to entities whose majority direct or indirect ownership is attributable to nations other than those listed in our Supported Regions Policy."_

It is unclear whether these restrictions apply to Claude Code when it is configured to route to a non-Anthropic model (e.g. Gemini via Bulwarkai). The Vertex terms define "Services" as Anthropic's AI technology, which might not cover requests sent to a third-party model. However, Claude Code is an Anthropic product, and the Consumer ToS covers "other products and services" beyond what is named. Anthropic has not publicly clarified this scenario. Consult legal counsel before deploying Claude Code in unsupported regions, even when routing to non-Anthropic models.

For users in unsupported regions, opencode and curl/SDK clients are not Anthropic products and are not subject to Anthropic's geographic restrictions.
