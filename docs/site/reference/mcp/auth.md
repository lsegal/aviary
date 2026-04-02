# Auth Tools

Auth tools manage API credentials and OAuth logins for model providers. Credentials are stored in an encrypted store (system keychain when available, otherwise a local file store).

The `auth_*` tools are blocked by the `standard` and `minimal` presets. They require the `full` preset or explicit allowlist inclusion.

---

## Credential Names

Credentials are stored by name. The `auth` field in `models.providers.<name>` must reference the stored key using the form `auth:<key>`.

```yaml
models:
  providers:
    anthropic:
      auth: auth:anthropic:default   # references the key stored as "anthropic:default"
```

Store the credential with: `auth_set { name: "anthropic:default", value: "sk-ant-..." }`

---

## auth_set

Store a credential by name.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes | Credential name (e.g. `"ANTHROPIC_API_KEY"`) |
| `value` | string | yes | Credential value (API key or token) |

**Returns:** Text confirmation.

**Side effects:** Writes to the encrypted credential store. The value is never written to `aviary.yaml`.

---

## auth_get

Check whether a credential is stored and return a masked preview.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes | Credential name |

**Returns:** JSON object.

```json
{ "name": "ANTHROPIC_API_KEY", "set": true, "preview": "sk-ant-...****" }
```

---

## auth_list

List the names of all stored credentials.

**Arguments:** none

**Returns:** JSON array of credential name strings.

---

## auth_delete

Remove a stored credential.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes | Credential name to delete |

**Returns:** Text confirmation.

---

## OAuth Login Flows

OAuth tools let you authenticate with providers using your existing account, without managing raw API keys. The typical pattern is a two-step flow: call the start tool, follow the instructions, then call the complete tool.

---

## auth_login_anthropic

Start an Anthropic Claude Pro/Max OAuth login. Returns an authorization URL to open in a browser.

**Arguments:** none

**Returns:** JSON with the URL and instructions.

```json
{
  "url": "https://claude.ai/oauth/authorize?...",
  "instructions": "Open this URL in your browser and complete sign-in, then copy the code shown and call auth_login_anthropic_complete."
}
```

**Side effects:** Generates a PKCE challenge and stores pending state. May open the browser automatically.

---

## auth_login_anthropic_complete

Complete the Anthropic OAuth login by exchanging the authorization code.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `code` | string | yes | The code shown after completing sign-in on the Anthropic page |

**Returns:** Text confirmation including the token expiry.

**Side effects:** Exchanges the code for tokens, stores the credential, and reconciles running agents.

---

## auth_login_gemini

Start a Google Gemini OAuth login. Returns the full authorization URL and callback URL, attempts to open the browser automatically, and starts a temporary callback listener on `localhost:45289`.

**Arguments:** none

**Returns:** JSON with the authorization URL, callback URL, browser-open status, and timeout metadata.

```json
{
  "url": "https://accounts.google.com/o/oauth2/v2/auth?...",
  "callback_url": "http://localhost:45289",
  "browser_opened": true,
  "browser_open_error": "",
  "expires_at": "2026-04-02T20:00:00Z",
  "timeout_seconds": 120
}
```

**Side effects:** Attempts to open the browser and starts a 2-minute local callback listener. Call `auth_login_gemini_complete` after you finish authorization in the browser.

---

## auth_login_gemini_complete

Wait for the Google Gemini OAuth callback, exchange the authorization code for tokens, and store them.

**Arguments:** none

**Returns:** Text confirmation including the token expiry.

**Side effects:** Waits for the pending Gemini callback, stores the token on success, and fails if the pending callback has timed out.

---

## auth_login_openai

Start an OpenAI/Codex OAuth login. Returns the full authorization URL and callback URL, attempts to open the browser automatically, and starts a temporary callback listener on `localhost:1455`.

**Arguments:** none

**Returns:** JSON with the authorization URL, callback URL, browser-open status, and timeout metadata.

```json
{
  "url": "https://auth.openai.com/oauth/authorize?...",
  "callback_url": "http://localhost:1455/auth/callback",
  "browser_opened": true,
  "browser_open_error": "",
  "expires_at": "2026-04-02T20:00:00Z",
  "timeout_seconds": 120
}
```

**Side effects:** Attempts to open the browser and starts a 2-minute local callback listener. Call `auth_login_openai_complete` after you finish authorization in the browser.

---

## auth_login_openai_complete

Wait for the OpenAI Codex OAuth callback, exchange the authorization code for tokens, and store them.

**Arguments:** none

**Returns:** Text confirmation with the token expiry.

**Side effects:** Waits for the pending OpenAI callback, stores the token on success, and fails if the pending callback has timed out.

---

## auth_login_github_copilot

Start a GitHub Copilot device-flow login. Returns a user code to enter on GitHub's device authorization page.

**Arguments:** none

**Returns:** JSON with the user code and verification URL.

```json
{
  "user_code": "ABCD-1234",
  "verification_uri": "https://github.com/login/device"
}
```

**Side effects:** Requests a device code and stores pending state.

---

## auth_login_github_copilot_complete

Complete the GitHub Copilot login after the user has authorized the device code on GitHub. Polls until authorization succeeds or times out (10 minutes).

**Arguments:** none

**Returns:** Text confirmation.

**Side effects:** Polls the GitHub device auth endpoint and stores the token on success.
