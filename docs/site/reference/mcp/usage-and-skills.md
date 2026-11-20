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

Disk-installed skills are discovered from `~/.config/aviary/skills` and `~/.agents/skill`. Search for published skills with `npx skills find` or on [skills.sh](https://skills.sh/). Example global install:

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

## coding_skills_list

List the [skills Aviary ships for coding CLIs](/guide/skills/) and the copy installed in each coding CLI on the `PATH`.

**Arguments:** none

**Returns:** JSON status object. `installs` follows the order of `clis`. `current` is true when the installed copy matches the one bundled with this release, and `version` is the Aviary release a copy came from, when known.

```json
{
  "version": "2.0.0",
  "source": "lsegal/aviary",
  "release": true,
  "clis": [{ "id": "claude", "name": "Claude", "target": "claude-code", "binary": "claude" }],
  "skills": [
    {
      "name": "gh-fix",
      "summary": "Fixes an issue end to end: ...",
      "run_by_projects": true,
      "installs": [{ "cli": "claude", "installed": true, "current": false, "version": "1.5.16", "source": "lsegal/aviary" }]
    }
  ],
  "outdated": 1
}
```

---

## coding_skills_install

Install or update the coding-CLI skills with `npx skills add`. A release installs from its own tag; a development build installs the copies bundled with it. Copies that are already current are skipped.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `skill` | string | | `gh-fix`, `gh-discuss`, `gh-create` or `gh-safe-merge`. Omit for every skill. |
| `cli` | string | | Coding CLI id from `coding_skills_list`. Omit for every coding CLI on the `PATH`. |
| `update_only` | bool | | Only update outdated copies; skip CLIs that don't have the skill yet. |

**Returns:** the copies installed, as `skill/cli`.

```json
{ "installed": ["gh-fix/claude"] }
```

Both tools are in the `server` permission group.

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
