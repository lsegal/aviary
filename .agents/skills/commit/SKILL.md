---
name: commit
description: "Stage and commit git changes with conventional prefixes. Use when the user asks to commit, save changes, write a commit message, or push staged work to the repository."
---

Group changed files into logical commits by what they changed. Each commit uses a conventional prefix:

- `bug:` — bug fixes
- `feature:` — new features
- `test:` — test-only changes
- `chore:` — maintenance, refactoring, dependencies

## Workflow

1. Run `git status` and `git diff --stat` to identify all changed files
2. Group related changes (e.g., all files for one feature together)
3. For each group:
   - Stage files: `git add <file1> <file2>`
   - Review staged diff: `git diff --staged --stat`
   - Commit: `git commit -m "feature: add calendar sync endpoint"`
4. Keep subject lines under 72 characters; add detail in the commit body if needed

## Examples

```
bug: fix nil pointer in envelope thread lookup
feature: add Drive file search to gogcli skill
test: add e2e tests for calendar event creation
chore: update Go dependencies
```
