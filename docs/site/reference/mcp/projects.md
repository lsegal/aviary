# Projects Tools

Projects settings live in the `projects:` section of `aviary.yaml` and are read and written with [`config_get` and `config_save`](./server-and-config). Only values that differ from the defaults are written, so a project watched with defaults adds nothing to the file.

---

## project_check

Check whether a repository, Project board or Discussions board can be watched as a project. The add-project dialog runs it as you type. It is read-only and writes nothing.

It reports, as separate checks:

- **github**: `gh` is installed, signed in, and can read the repository (archived repositories fail).
- **fork**: a warning when you can't push to the repository, so pull requests will come from your fork.
- **ready_label**: the ready label exists, with its count of open issues. Repositories only.
- **clis**: which coding CLIs (`claude`, `codex`, `gemini`, `opencode`, `cline`) are on `PATH`.
- **skills**: whether `gh-fix` is installed for each of those CLIs, with its version when the skill declares one.

**Arguments:**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `target` | string | ✓ | `owner/repo`, a GitHub URL, `users/<login>/projects/<n>`, `orgs/<org>/projects/<n>`, or `owner/repo/discussions`. |
| `ready_label` | string | | Ready label to look for. Defaults to `ready`. |

**Returns:** JSON object. `ok` is false when any check has `status: "fail"`.

```json
{
  "target": "lsegal/zvidlib",
  "repo": "lsegal/zvidlib",
  "kind": "repo",
  "ready_label": "ready",
  "fork": true,
  "ok": true,
  "checks": [
    { "name": "github", "status": "ok", "message": "gh is signed in and can read lsegal/zvidlib" },
    { "name": "fork", "status": "warn", "message": "You can't push to lsegal/zvidlib, so pull requests will come from your fork" },
    { "name": "ready_label", "status": "ok", "message": "\"ready\" exists, with 4 open issues" },
    { "name": "clis", "status": "ok", "message": "Found claude and codex" },
    { "name": "skills", "status": "ok", "message": "gh-fix 1.6.0 installed for all 2" }
  ],
  "clis": [
    { "name": "claude", "path": "/usr/local/bin/claude", "skills": { "gh-fix": "1.6.0" } },
    { "name": "codex", "path": "/usr/local/bin/codex", "skills": { "gh-fix": "1.6.0" } }
  ]
}
```
