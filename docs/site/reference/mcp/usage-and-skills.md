# Usage and Skills Tools

---

## usage_query

Return raw token-usage records within a date range, used for the Usage dashboard's totals and charts.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `start` | string | | Start of date range (RFC3339 or `YYYY-MM-DD`). Defaults to 30 days ago. |
| `end` | string | | End of date range. A bare date includes that whole day. Defaults to now. |
| `agent` | string | | Only records of this agent ID. |
| `model` | string | | Only records of this model. |
| `status` | string | | `ok` keeps only sessions with no error, `error` only sessions that had one. |
| `deleted` | string | | `include` (default), `exclude`, or `only` sessions that were deleted. |

The filters match what `usage_sessions` lists, so totals summed from these records agree with its sessions. Code Loop work and delivery records (below) are always returned, whatever the filters.

**Returns:** JSON array of usage records.

```json
[
  {
    "timestamp": "2026-03-22T14:30:00Z",
    "agent_id": "assistant",
    "session_id": "01HZ...",
    "provider": "anthropic",
    "model": "claude-sonnet-4-6",
    "input_tokens": 1842,
    "output_tokens": 312,
    "session_name": "Plan the trip",
    "session_kind": "chat"
  }
]
```

Each token record snapshots the session that spent it: `session_name` is its display name at the time, `session_kind` is `chat`, `job` (a Code Loop job), `task` or `workflow`, and `session_owner` is what it belongs to, such as the issue a job worked on (`owner/repo#1433`), a task's `agent/task` key, or the channel that fired a workflow. The snapshot outlives the session, so Usage still names it after `session_remove`, a delete with session data, or clearing a Code Loop job.

Records are stored per LLM call. Aggregate them by `model`, `provider`, or `agent` to produce cost breakdowns and usage summaries.

Records with a `project` object report Code Loop work instead of token use: `project.issue` is the lowercase `owner/repo#number` of the issue, `project.duration_ms` is the wall time of one job run on it, and `project.outcome` is `merged` when one of its pull requests merged or `failed` when its job failed. A `merged` record also carries `project.additions` and `project.deletions`, the lines the merged pull request's diff added and removed as GitHub reports them. Leave these records out of token aggregates.

```json
{
  "timestamp": "2026-03-22T15:02:11Z",
  "session_id": "owner/repo#42",
  "agent_id": "projects",
  "model": "",
  "provider": "",
  "input_tokens": 0,
  "output_tokens": 0,
  "project": { "issue": "owner/repo#42", "duration_ms": 1843000 }
}
```

## usage_sessions

Return one sorted page of the sessions that spent tokens within a date range, each with its name, kind, owner and token totals. Sessions are aggregated, filtered, sorted and paged on the server.

**Arguments:** every `usage_query` argument, plus:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `sort` | string | | `name`, `agent`, `model`, `tokens`, `tools`, or `last` (default, last activity). |
| `order` | string | | `asc` or `desc`. Text columns default to `asc`, the rest to `desc`. |
| `limit` | number | | Sessions per page. Defaults to 50, at most 500. |
| `offset` | number | | Sessions to skip before the page. |

**Returns:** the page, the count and totals over every matching session, and the agents and models in range to filter by.

```json
{
  "sessions": [
    {
      "session_id": "sess_01HZ...",
      "agent_id": "assistant",
      "model": "claude-sonnet-4-6",
      "input": 18420, "output": 3120, "tool_calls": 4, "messages": 6,
      "has_error": false, "has_throttle": false,
      "first_ts": "2026-03-22T14:30:00Z", "last_ts": "2026-03-22T15:10:00Z",
      "name": "Plan the trip", "kind": "chat",
      "deleted": true, "deleted_at": "2026-03-23T09:00:00Z"
    }
  ],
  "total": 696,
  "offset": 0,
  "limit": 50,
  "totals": { "input": 9120000, "output": 412000, "cache_read": 0, "cache_write": 0, "tool_calls": 2210, "messages": 3410, "errors": 12 },
  "agents": ["assistant", "projects"],
  "models": ["claude-sonnet-4-6", "opus"]
}
```

`name` is the session's live title while it exists, else the latest name its records snapshotted, else one recovered from its task, its Code Loop issue or its deletion record, else `Unknown session (deleted)`. `deleted` marks a session that no longer exists, and `deleted_at` is when it was deleted, when that was recorded. Code Loop job runs (agent `projects`) have no session of their own and are never marked deleted.

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

List the [skills Aviary ships for providers](/guide/skills/) and the copy installed in each CLI provider on the `PATH`.

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

Install or update the skills Aviary ships with `npx skills add`. A release installs from its own tag; a development build installs the copies bundled with it. Copies that are already current are skipped.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `skill` | string | | `gh-fix`, `gh-discuss`, `gh-create` or `gh-safe-merge`. Omit for every skill. |
| `cli` | string | | CLI provider id from `coding_skills_list`. Omit for every CLI provider on the `PATH`. |
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
