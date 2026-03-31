# Usage and Skills Tools

---

## usage_query

Return raw token-usage records within a date range, used to populate the Usage dashboard.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `start` | string | | Start of date range (RFC3339 or `YYYY-MM-DD`). Defaults to 30 days ago. |
| `end` | string | | End of date range. Defaults to now. |

**Returns:** JSON array of usage records.

```json
[
  {
    "timestamp": "2026-03-22T14:30:00Z",
    "agent": "assistant",
    "session_id": "01HZ...",
    "provider": "anthropic",
    "model": "claude-sonnet-4-6",
    "input_tokens": 1842,
    "output_tokens": 312
  }
]
```

Records are stored per LLM call. Aggregate them by `model`, `provider`, or `agent` to produce cost breakdowns and usage summaries.

---

## skills_list

List all installed skills and whether each is currently enabled in configuration.

**Arguments:** none

**Returns:** JSON array of skill objects.

```json
[
  {
    "name": "my-skill",
    "description": "Does something useful",
    "enabled": true
  }
]
```

Skills are either built-in (shipped with Aviary) or installed from disk. Enable a skill in `aviary.yaml` under the `skills` key:

Disk-installed skills are discovered from `AVIARY_CONFIG_BASE_DIR/skills` and `~/.agents/skill`. Search for published skills with `npx skills find` or on [skills.sh](https://skills.sh/). Example global install:

```bash
npx skills add --global -a universal 4ier/notion-cli
```

```yaml
skills:
  my-skill:
    enabled: true
    settings:
      api_url: https://example.com
```

Enabled skills register additional MCP tools at server startup. See the [Configuration Reference](/reference/config#skills) for the full `skills` schema.

---

## web_search

Search the web and return a list of results with titles, URLs, and descriptions.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `query` | string | yes | Search query |
| `count` | int | | Number of results to return (default: 10, max: 20) |

**Returns:** JSON array of result objects.

```json
[
  {
    "title": "Example Result",
    "url": "https://example.com/page",
    "description": "A brief description of the page content."
  }
]
```

**Backend:** Uses the Brave Search API when `search.web.brave_api_key` is configured with a stored credential. Falls back to a DuckDuckGo browser search otherwise (opens a browser tab, so `browser_*` tools must be available).
